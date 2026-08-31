// 실제 카메라 RTSP 스트림의 NALU 구조를 분석하는 진단 도구
// 사용법: rtspanalyze -url rtsp://... 또는 -onvif host:port -user u -pass p
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/pion/rtp"

	"webnvr/internal/camera"
	"webnvr/internal/onvif"
)

func main() {
	url := flag.String("url", "", "RTSP URL")
	onvifAddr := flag.String("onvif", "", "ONVIF 주소 (host:port)")
	user := flag.String("user", "", "사용자명")
	pass := flag.String("pass", "", "비밀번호")
	cameraID := flag.String("camera", "", "config/cameras.json의 카메라 ID (자격증명 자동 복호화)")
	seconds := flag.Int("seconds", 8, "분석 시간(초)")
	flag.Parse()

	rawURL := *url
	if *cameraID != "" {
		store, err := camera.NewJSONCameraStore("config/cameras.json")
		if err != nil {
			log.Fatal(err)
		}
		mgr := camera.NewManager(store)
		cam, err := mgr.Get(*cameraID)
		if err != nil {
			log.Fatal(err)
		}
		pw, err := mgr.PasswordOf(cam)
		if err != nil {
			log.Fatal(err)
		}
		cli, err := onvif.New(cam.XAddr, cam.Username, pw)
		if err != nil {
			log.Fatal(err)
		}
		token := cam.ProfileToken
		if token == "" {
			profiles, err := cli.Profiles(context.Background())
			if err != nil {
				log.Fatal(err)
			}
			token = profiles[0].Token
		}
		uri, err := cli.StreamURI(context.Background(), token, "RTSP")
		if err != nil {
			log.Fatal(err)
		}
		rawURL = uri
		fmt.Println("스트림 URI:", uri)
	} else if *onvifAddr != "" {
		cli, err := onvif.New(*onvifAddr, *user, *pass)
		if err != nil {
			log.Fatal(err)
		}
		profiles, err := cli.Profiles(context.Background())
		if err != nil {
			log.Fatal(err)
		}
		if len(profiles) == 0 {
			log.Fatal("프로필 없음")
		}
		uri, err := cli.StreamURI(context.Background(), profiles[0].Token, "RTSP")
		if err != nil {
			log.Fatal(err)
		}
		rawURL = uri
	}
	if rawURL == "" {
		flag.Usage()
		return
	}

	u, err := base.ParseURL(rawURL)
	if err != nil {
		log.Fatal(err)
	}
	var tcp gortsplib.Transport = gortsplib.TransportTCP
	conf := gortsplib.Client{Transport: &tcp, ReadTimeout: 10 * time.Second}
	if err := conf.Start(u.Scheme, u.Host); err != nil {
		log.Fatal(err)
	}
	defer conf.Close()

	sd, _, err := conf.Describe(u)
	if err != nil {
		log.Fatal(err)
	}

	var (
		medi  *description.Media
		h264f *format.H264
	)
	for _, m := range sd.Medias {
		if m.Type == description.MediaTypeVideo && m.FindFormat(&h264f) {
			medi = m
			break
		}
	}
	if medi == nil {
		log.Fatal("비디오 미디어 없음")
	}
	mediaURL, err := medi.URL(sd.BaseURL)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := conf.Setup(mediaURL, medi, 0, 0); err != nil {
		log.Fatal(err)
	}

	dec, err := h264f.CreateDecoder()
	if err != nil {
		log.Fatal(err)
	}

	typeCounter := map[uint8]int{}
	packetCount := 0
	keyframeTs := []uint32{} // IDR이 포함된 프레임의 RTP 타임스탬프
	firstPayloadDump := 0
	frameTs := uint32(0)
	frameNals := map[uint8]bool{}
	naluSizes := []int{}

	conf.OnPacketRTP(medi, h264f, func(pkt *rtp.Packet) {
		packetCount++
		if firstPayloadDump < 3 {
			fmt.Printf("패킷#%d seq=%d ts=%d marker=%v payload_len=%d payload[:8]=%x type=%d\n",
				packetCount, pkt.SequenceNumber, pkt.Timestamp, pkt.Marker,
				len(pkt.Payload), pkt.Payload[:min(8, len(pkt.Payload))],
				pkt.Payload[0]&0x1f)
			firstPayloadDump++
		}

		nalus, err := dec.Decode(pkt)
		if err != nil {
			return
		}
		if len(nalus) > 0 && pkt.Timestamp != frameTs {
			// 새 프레임 시작 → 이전 프레임 기록
			if frameNals[5] {
				keyframeTs = append(keyframeTs, frameTs)
			}
			frameNals = map[uint8]bool{}
			frameTs = pkt.Timestamp
		}
		for _, n := range nalus {
			t := n[0] & 0x1f
			typeCounter[t]++
			frameNals[t] = true
			naluSizes = append(naluSizes, len(n))
		}
	})

	if _, err := conf.Play(nil); err != nil {
		log.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		time.Sleep(time.Duration(*seconds) * time.Second)
		close(done)
	}()
	<-done

	fmt.Println("\n── 분석 결과 ──")
	fmt.Printf("패킷: %d개, NALU: %d개\n", packetCount, len(naluSizes))
	names := map[uint8]string{1: "슬라이스(P/B)", 5: "IDR", 6: "SEI", 7: "SPS", 8: "PPS", 9: "AUD", 24: "STAP-A", 28: "FU-A"}
	for t, c := range typeCounter {
		n := names[t]
		if n == "" {
			n = "기타"
		}
		fmt.Printf("  NALU 타입 %2d (%s): %d개\n", t, n, c)
	}
	if len(naluSizes) > 0 {
		max := 0
		for _, s := range naluSizes {
			if s > max {
				max = s
			}
		}
		fmt.Printf("  최대 NALU 크기: %d bytes\n", max)
	}
	fmt.Printf("  프레임별 IDR: %d회", len(keyframeTs))
	if len(keyframeTs) >= 2 {
		gap := (keyframeTs[len(keyframeTs)-1] - keyframeTs[0]) / uint32(len(keyframeTs)-1)
		fmt.Printf(" (IDR 간격 ≈ %.1f초 @90kHz)", float64(gap)/90000)
	}
	fmt.Println()
	if len(keyframeTs) == 0 {
		fmt.Println("  ⚠️ IDR(type 5)이 한 번도 관측되지 않았습니다!")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

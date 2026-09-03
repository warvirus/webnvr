// 실제 카메라 RTP 패킷을 JSON으로 덤프하는 도구 (워커 디페이저 로직 검증용)
// 사용법: rtpdump -camera <id> -out <file> -count 400
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/pion/rtp"

	"webnvr/internal/camera"
	"webnvr/internal/db"
	"webnvr/internal/onvif"
)

type dumpPacket struct {
	Seq     uint16 `json:"seq"`
	TS      uint32 `json:"ts"`
	Marker  bool   `json:"marker"`
	Payload string `json:"payload"` // base64
}

func main() {
	cameraID := flag.String("camera", "", "config/cameras.json의 카메라 ID")
	out := flag.String("out", "rtpdump.json", "출력 파일")
	count := flag.Int("count", 400, "덤프할 패킷 수")
	flag.Parse()

	if *cameraID == "" {
		log.Fatal("-camera 필요")
	}
	database, err := db.Open("config")
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	mgr := camera.NewManager(camera.NewSQLCameraStore(database.SQL()))
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
		profiles, _ := cli.Profiles(context.Background())
		token = profiles[0].Token
	}
	uri, err := cli.StreamURI(context.Background(), token, "RTSP")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("스트림 URI:", uri)

	u, err := base.ParseURL(uri)
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
	var medi *description.Media
	var h264f *format.H264
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

	packets := make([]dumpPacket, 0, *count)
	conf.OnPacketRTP(medi, h264f, func(pkt *rtp.Packet) {
		if len(packets) >= *count {
			return
		}
		packets = append(packets, dumpPacket{
			Seq:     pkt.SequenceNumber,
			TS:      pkt.Timestamp,
			Marker:  pkt.Marker,
			Payload: base64.StdEncoding.EncodeToString(pkt.Payload),
		})
	})
	if _, err := conf.Play(nil); err != nil {
		log.Fatal(err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for len(packets) < *count && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	b, _ := json.MarshalIndent(packets, "", " ")
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("패킷 %d개를 %s에 저장\n", len(packets), *out)
}

// 실제 카메라 RTSP 스트림 수신을 확인하는 커맨드라인 진단 도구 (Phase 2.6 수동 테스트용)
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"webnvr/internal/onvif"
	"webnvr/internal/stream"
)

func main() {
	url := flag.String("url", "", "직접 테스트할 RTSP URL (rtsp://user:pass@host:554/stream)")
	onvifAddr := flag.String("onvif", "", "ONVIF 카메라 주소 (host:port)")
	onvifUser := flag.String("user", "", "ONVIF 사용자명")
	onvifPass := flag.String("pass", "", "ONVIF 비밀번호")
	profile := flag.String("profile", "", "ONVIF 프로필 토큰 (비우면 첫 프로필 사용)")
	transport := flag.String("transport", "tcp", "전송 방식 (tcp|udp)")
	seconds := flag.Int("seconds", 10, "수신 대기 시간(초)")
	flag.Parse()

	rawURL := *url
	if *onvifAddr != "" {
		uri, err := onvifURI(*onvifAddr, *onvifUser, *onvifPass, *profile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ONVIF 오류: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("스트림 URI:", uri)
		rawURL = uri
	}
	if rawURL == "" {
		flag.Usage()
		os.Exit(2)
	}

	fmt.Printf("연결 시도: %s (%s)\n", rawURL, *transport)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*seconds)*time.Second)
	defer cancel()

	var (
		packets int
		bytes   int64
		start   time.Time
	)
	done := make(chan error, 1)
	go func() {
		done <- stream.DialRTSP(ctx, rawURL, *transport,
			func(info stream.Info) {
				fmt.Printf("스트림 시작: 코덱=%s 해상도=%dx%d clock=%d ssrc=%d\n",
					info.Codec, info.Width, info.Height, info.ClockRate, info.SSRC)
				fmt.Printf("  SPS(%dB): %s\n", len(info.SPS), base64.StdEncoding.EncodeToString(info.SPS))
				fmt.Printf("  PPS(%dB): %s\n", len(info.PPS), base64.StdEncoding.EncodeToString(info.PPS))
				start = time.Now()
			},
			func(pkt stream.Packet) {
				packets++
				bytes += int64(len(pkt.Payload))
			},
			nil, // onNALU (녹화 탭 — 진단 도구는 미사용)
		)
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-done:
		if packets == 0 {
			fmt.Fprintln(os.Stderr, "수신된 패킷 없음. err=", err)
			os.Exit(1)
		}
	case <-sig:
		cancel()
	}

	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	fmt.Printf("\n수신 결과: 패킷 %d개, %.1f kbps, %.1f초\n",
		packets, float64(bytes)*8/1000/elapsed, elapsed)
}

// onvifURI는 ONVIF 카메라에서 RTSP 스트림 URI를 조회한다.
func onvifURI(addr, user, pass, profile string) (string, error) {
	cli, err := onvif.New(addr, user, pass)
	if err != nil {
		return "", err
	}
	info, err := cli.DeviceInformation(context.Background())
	if err != nil {
		return "", err
	}
	fmt.Printf("장치 연결 성공: %s %s (펌웨어 %s)\n", info.Manufacturer, info.Model, info.Firmware)

	if profile == "" {
		profiles, err := cli.Profiles(context.Background())
		if err != nil {
			return "", err
		}
		if len(profiles) == 0 {
			return "", fmt.Errorf("사용 가능한 프로필 없음")
		}
		profile = profiles[0].Token
		fmt.Printf("프로필 %d개 발견, 사용: %s\n", len(profiles), profile)
	}
	return cli.StreamURI(context.Background(), profile, "RTSP")
}

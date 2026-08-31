// WS 파이프라인 검증용 로컬 모의 RTSP 서버 — 정적 SPS/PPS와 합성 H.264 패킷을 스트리밍한다
package main

import (
	"flag"
	"log"
	"math/rand"
	"net"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
)

var sampleSPS = []byte{
	0x67, 0x64, 0x00, 0x0c, 0xac, 0x3b, 0x50, 0xb0,
	0x4b, 0x42, 0x00, 0x00, 0x03, 0x00, 0x02, 0x00,
	0x00, 0x03, 0x00, 0x3d, 0x08,
}
var samplePPS = []byte{0x68, 0xeb, 0xec, 0xb2, 0x2c}

type handler struct {
	stream *gortsplib.ServerStream
	medi   *description.Media
	h264f  *format.H264
}

func (h *handler) OnConnOpen(*gortsplib.ServerHandlerOnConnOpenCtx)         {}
func (h *handler) OnConnClose(*gortsplib.ServerHandlerOnConnCloseCtx)       {}
func (h *handler) OnSessionOpen(*gortsplib.ServerHandlerOnSessionOpenCtx)   {}
func (h *handler) OnSessionClose(*gortsplib.ServerHandlerOnSessionCloseCtx) {}

func (h *handler) OnDescribe(*gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, h.stream, nil
}

func (h *handler) OnSetup(*gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, h.stream, nil
}

func (h *handler) OnPlay(*gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	// 30fps로 IDR/슬라이스 NALU를 계속 전송한다
	go func() {
		enc, err := h.h264f.CreateEncoder()
		if err != nil {
			return
		}
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		idrBody := make([]byte, 40_000) // 1080p급 대형 IDR → FU-A 다중 단편
		for i := range idrBody {
			idrBody[i] = byte(r.Intn(255))
		}
		pBody := make([]byte, 3000)
		for i := range pBody {
			pBody[i] = byte(r.Intn(255))
		}
		tick := time.NewTicker(33 * time.Millisecond)
		defer tick.Stop()
		var ts int64
		frameCount := 0
		for range tick.C {
			var nalus [][]byte
			isIDR := frameCount%60 == 0 // 2초 간격 IDR (실제 카메라와 동일)
			if isIDR {
				nalus = [][]byte{append([]byte{5}, idrBody...)}
			} else {
				nalus = [][]byte{append([]byte{1}, pBody...)}
			}
			if isIDR {
				// 실제 카메라 패턴: IDR 프레임 앞에 STAP-A(SPS+PPS)를 먼저 인코딩/전송한다.
				// (순서를 바꾸면 시퀀스가 역행해 수신 측에서 대규모 유실로 오판된다)
				if stap, err := enc.Encode([][]byte{sampleSPS, samplePPS}); err == nil && len(stap) > 0 {
					_ = h.stream.WritePacketRTP(h.medi, stap[0])
				}
			}
			pkts, err := enc.Encode(nalus)
			if err != nil || len(pkts) == 0 {
				continue
			}
			for _, p := range pkts {
				if err := h.stream.WritePacketRTP(h.medi, p); err != nil {
					return
				}
			}
			ts += 3000
			frameCount++
		}
	}()
	return &base.Response{StatusCode: base.StatusOK}, nil
}

func main() {
	addr := flag.String("addr", "", "리슨 주소 (비우면 자유 포트 사용)")
	flag.Parse()

	if *addr == "" {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			log.Fatal(err)
		}
		*addr = l.Addr().String()
		_ = l.Close()
	}

	h := &handler{
		h264f: &format.H264{
			PayloadTyp:        96,
			SPS:               sampleSPS,
			PPS:               samplePPS,
			PacketizationMode: 1,
		},
	}
	h.medi = &description.Media{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{h.h264f},
	}

	srv := &gortsplib.Server{Handler: h, RTSPAddress: *addr}
	if err := srv.Start(); err != nil {
		log.Fatal(err)
	}
	h.stream = gortsplib.NewServerStream(srv, &description.Session{
		Medias: []*description.Media{h.medi},
	})

	log.Printf("모의 RTSP 서버 대기 중: rtsp://%s/stream", srv.RTSPAddress)
	if err := srv.Wait(); err != nil {
		log.Fatal(err)
	}
}

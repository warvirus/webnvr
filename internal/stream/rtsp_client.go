// gortsplib 기반 RTSP 클라이언트로 카메라 스트림에 연결해 RTP 패킷을 수신한다.
package stream

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/pion/rtp"
)

// Dialer는 스트림 연결 함수의 시그니처다. 테스트 시 가짜 구현으로 교체한다.
// 블로킹 함수이며 ctx가 취소되거나 스트림이 끊기면 반환한다.
type Dialer func(ctx context.Context, rawURL, transport string, onInfo func(Info), onPacket func(Packet)) error

// parameterSets는 스트림 수신 중 수집하는 코덱 파라미터 셋이다.
type parameterSets struct {
	mu   sync.Mutex
	ssrc uint32
	vps  []byte
	sps  []byte
	pps  []byte
}

// DialRTSP는 RTSP URL에 연결해 코덱 정보와 RTP 패킷을 콜백으로 전달한다.
// transport는 "tcp" 또는 "udp"이며 비어 있으면 tcp를 사용한다.
func DialRTSP(ctx context.Context, rawURL, transport string, onInfo func(Info), onPacket func(Packet)) error {
	u, err := base.ParseURL(rawURL)
	if err != nil {
		return fmt.Errorf("RTSP URL 파싱 실패: %w", err)
	}

	var transportTCP gortsplib.Transport = gortsplib.TransportTCP
	var transportUDP gortsplib.Transport = gortsplib.TransportUDP
	conf := gortsplib.Client{
		Transport:   &transportTCP,
		ReadTimeout: 10 * time.Second,
		// 카메라 서버(PythonCam 등)는 새 클라이언트 접속 시 인코더를 재시작해
		// SSRC가 바뀐다. 기본 동작(세션 종료) 대신 새 SSRC를 수용한다.
		AllowSSRCChange: true,
	}
	if transport == "udp" {
		conf.Transport = &transportUDP
	}
	if err := conf.Start(u.Scheme, u.Host); err != nil {
		return fmt.Errorf("RTSP 연결 실패: %w", err)
	}
	defer conf.Close()

	sd, _, err := conf.Describe(u)
	if err != nil {
		return fmt.Errorf("DESCRIBE 실패: %w", err)
	}

	// 비디오 미디어와 지원 코덱을 찾는다.
	var (
		medi      *description.Media
		h264f     *format.H264
		h265f     *format.H265
		forma     format.Format
		codec     = CodecH264
		clockRate = uint32(90000)
	)
	for _, m := range sd.Medias {
		if m.Type != description.MediaTypeVideo {
			continue
		}
		if m.FindFormat(&h264f) {
			medi, forma = m, h264f
			break
		}
		if m.FindFormat(&h265f) {
			medi, forma = m, h265f
			codec = CodecH265
			break
		}
	}
	if medi == nil {
		return fmt.Errorf("SDP에 비디오 미디어가 없음")
	}
	clockRate = uint32(forma.ClockRate())

	ps := &parameterSets{}
	if h264f != nil {
		ps.sps, ps.pps = append([]byte(nil), h264f.SPS...), append([]byte(nil), h264f.PPS...)
	} else {
		ps.vps = append([]byte(nil), h265f.VPS...)
		ps.sps = append([]byte(nil), h265f.SPS...)
		ps.pps = append([]byte(nil), h265f.PPS...)
	}

	mediaURL, err := medi.URL(sd.BaseURL)
	if err != nil {
		return fmt.Errorf("미디어 URL 추출 실패: %w", err)
	}
	if _, err := conf.Setup(mediaURL, medi, 0, 0); err != nil {
		return fmt.Errorf("SETUP 실패: %w", err)
	}

	var infoOnce sync.Once
	decoder, err := createDecoder(forma)
	if err != nil {
		return fmt.Errorf("디코더 생성 실패: %w", err)
	}

	// 코덱 정보가 확정되면 onInfo를 호출한다. SDP에 SPS/PPS가 없으면
	// 스트림에서 파라미터 셋 NALU가 도착할 때 호출된다.
	emitInfo := func() {
		infoOnce.Do(func() {
			ps.mu.Lock()
			info := Info{
				Codec:     codec,
				VPS:       append([]byte(nil), ps.vps...),
				SPS:       append([]byte(nil), ps.sps...),
				PPS:       append([]byte(nil), ps.pps...),
				SSRC:      ps.ssrc,
				ClockRate: clockRate,
			}
			ps.mu.Unlock()
			info.Width, info.Height = dimensionsOf(codec, info.SPS)
			onInfo(info)
		})
	}
	emitInfoIfReady := func() {
		ps.mu.Lock()
		ready := ps.sps != nil && ps.pps != nil
		ps.mu.Unlock()
		if ready {
			emitInfo()
		}
	}
	emitInfoIfReady()

	conf.OnPacketRTP(medi, forma, func(pkt *rtp.Packet) {
		ps.mu.Lock()
		if ps.ssrc == 0 {
			ps.ssrc = pkt.SSRC
		}
		ps.mu.Unlock()

		// SDP에 파라미터 셋이 없으면 스트림에서 수집한다.
		if ps.sps == nil || ps.pps == nil {
			if nalus, derr := decoder.Decode(pkt); derr == nil {
				collectParameterSets(codec, nalus, ps)
				emitInfoIfReady()
			}
		}

		onPacket(Packet{
			Codec:       codec,
			SSRC:        pkt.SSRC,
			ClockRate:   clockRate,
			PayloadType: pkt.PayloadType,
			Sequence:    pkt.SequenceNumber,
			Timestamp:   pkt.Timestamp,
			Marker:      pkt.Marker,
			Payload:     pkt.Payload,
		})
	})

	if _, err := conf.Play(nil); err != nil {
		return fmt.Errorf("PLAY 실패: %w", err)
	}

	// ctx 취소 또는 연결 종료까지 대기한다.
	done := make(chan error, 1)
	go func() {
		done <- conf.Wait()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("스트림 종료: %w", err)
		}
		return nil
	}
}

// createDecoder는 포맷에 맞는 RTP 디페이로더를 만든다.
func createDecoder(forma format.Format) (rtpDecoder, error) {
	switch f := forma.(type) {
	case *format.H264:
		return f.CreateDecoder()
	case *format.H265:
		return f.CreateDecoder()
	default:
		return nil, fmt.Errorf("지원하지 않는 포맷: %T", forma)
	}
}

// rtpDecoder는 H.264/H.265 RTP 디페이로더의 공통 인터페이스다.
type rtpDecoder interface {
	Decode(pkt *rtp.Packet) ([][]byte, error)
}

// collectParameterSets는 NALU 목록에서 SPS/PPS/VPS를 수집한다.
func collectParameterSets(codec Codec, nalus [][]byte, ps *parameterSets) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for _, n := range nalus {
		if len(n) == 0 {
			continue
		}
		switch codec {
		case CodecH264:
			switch n[0] & 0x1f {
			case 7:
				ps.sps = append([]byte(nil), n...)
			case 8:
				ps.pps = append([]byte(nil), n...)
			}
		case CodecH265:
			switch (n[0] >> 1) & 0x3f {
			case 32:
				ps.vps = append([]byte(nil), n...)
			case 33:
				ps.sps = append([]byte(nil), n...)
			case 34:
				ps.pps = append([]byte(nil), n...)
			}
		}
	}
}

// dimensionsOf는 SPS에서 해상도를 파싱한다. 실패 시 0을 반환한다.
func dimensionsOf(codec Codec, sps []byte) (int, int) {
	if sps == nil {
		return 0, 0
	}
	switch codec {
	case CodecH264:
		var s h264.SPS
		if err := s.Unmarshal(sps); err != nil {
			return 0, 0
		}
		return s.Width(), s.Height()
	default:
		return 0, 0 // H.265 SPS 파싱은 필요 시 확장
	}
}

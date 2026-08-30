// WebSocket 연결별 메시지 처리와 스트림 이벤트 펌핑을 담당한다.
package ws

import (
	"encoding/base64"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"webnvr/internal/stream"
)

// Controller는 WS 계층이 필요로 하는 스트림 제어 연산이다. (api.StreamService가 구현)
type Controller interface {
	Start(cameraID string) error
	Stop(cameraID string) error
	StartAll() error
	StopAll() error
	Subscribe(cameraID string) (<-chan stream.Event, func(), error)
	PTZ(cameraID string, cmd PTZCommand) error
}

// connState는 연결 하나의 상태다.
type connState struct {
	conn *websocket.Conn
	wmu  sync.Mutex // 쓰기 직렬화

	mu      sync.Mutex
	cancels map[string]func() // cameraID → 구독 해제
}

// sendMsg는 메시지를 직렬화해 전송한다. (쓰기 락 보호)
func (s *connState) sendMsg(m ServerMsg) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_ = s.conn.WriteJSON(m)
}

// handleError는 스트림 오류를 클라이언트에 알린다.
func (s *connState) handleError(cameraID, msg string) {
	s.sendMsg(ServerMsg{Type: MsgStreamError, CameraID: cameraID, Error: msg})
}

// startStream은 스트림을 시작하고 구독 이벤트를 연결로 펌핑한다.
func (s *connState) startStream(ctrl Controller, cameraID string) {
	s.mu.Lock()
	if _, ok := s.cancels[cameraID]; ok {
		s.mu.Unlock()
		return // 이미 구독 중
	}
	s.mu.Unlock()

	if err := ctrl.Start(cameraID); err != nil {
		s.handleError(cameraID, err.Error())
		return
	}
	ch, cancel, err := ctrl.Subscribe(cameraID)
	if err != nil {
		s.handleError(cameraID, err.Error())
		return
	}
	s.mu.Lock()
	if _, exists := s.cancels[cameraID]; exists {
		s.mu.Unlock()
		cancel()
		return
	}
	s.cancels[cameraID] = cancel
	s.mu.Unlock()

	go s.pump(cameraID, ch, cancel)
}

// pump는 스트림 이벤트를 WebSocket 메시지로 변환해 전송한다.
func (s *connState) pump(cameraID string, ch <-chan stream.Event, cancel func()) {
	defer func() {
		s.mu.Lock()
		delete(s.cancels, cameraID)
		s.mu.Unlock()
		cancel()
	}()
	for ev := range ch {
		switch e := ev.(type) {
		case stream.StartedEvent:
			s.sendMsg(streamStartedMsg(e.Info))
		case stream.PacketEvent:
			s.sendMsg(rtpPacketMsg(e.Packet))
		case stream.StoppedEvent:
			s.sendMsg(ServerMsg{Type: MsgStreamStopped, CameraID: cameraID, Reason: e.Reason})
			return
		}
	}
}

// streamStartedMsg는 Info를 stream_started 메시지로 변환한다.
func streamStartedMsg(info stream.Info) ServerMsg {
	return ServerMsg{
		Type:        MsgStreamStarted,
		CameraID:    info.CameraID,
		Codec:       string(info.Codec),
		SSRC:        info.SSRC,
		ClockRate:   info.ClockRate,
		PayloadType: info.PayloadType,
		SPS:         base64.StdEncoding.EncodeToString(info.SPS),
		PPS:         base64.StdEncoding.EncodeToString(info.PPS),
		VPS:         base64.StdEncoding.EncodeToString(info.VPS),
		Width:       info.Width,
		Height:      info.Height,
	}
}

// rtpPacketMsg는 Packet을 rtp_packet 메시지로 변환한다.
func rtpPacketMsg(pkt stream.Packet) ServerMsg {
	return ServerMsg{
		Type:      MsgRTPPacket,
		CameraID:  pkt.CameraID,
		Codec:     string(pkt.Codec),
		Payload:   base64.StdEncoding.EncodeToString(pkt.Payload),
		Timestamp: pkt.Timestamp,
		Marker:    pkt.Marker,
		Sequence:  pkt.Sequence,
	}
}

// handleClientMsg는 클라이언트 메시지를 해석해 동작을 수행한다.
func (s *connState) handleClientMsg(ctrl Controller, m ClientMsg) {
	switch m.Type {
	case MsgPing:
		s.sendMsg(ServerMsg{Type: MsgPong})
	case MsgStartStream:
		s.startStream(ctrl, m.CameraID)
	case MsgSubscribe:
		s.startStream(ctrl, m.CameraID)
	case MsgStopStream:
		s.stopStream(ctrl, m.CameraID, "사용자 정지")
	case MsgUnsubscribe:
		s.unsubscribe(m.CameraID)
	case MsgStartAllStreams:
		if err := ctrl.StartAll(); err != nil {
			s.handleError("", err.Error())
		}
	case MsgStopAllStreams:
		if err := ctrl.StopAll(); err != nil {
			s.handleError("", err.Error())
		}
	case MsgPTZ:
		if m.Command == nil {
			s.handleError(m.CameraID, "PTZ 명령이 비어 있음")
			return
		}
		if err := ctrl.PTZ(m.CameraID, *m.Command); err != nil {
			s.handleError(m.CameraID, err.Error())
		}
	case MsgRequestKeyframe:
		// RTSP 컨트롤 채널로 키프레임 요청은 카메라별 지원이 달라 추후 구현한다.
		slog.Debug("request_keyframe 미지원", "camera", m.CameraID)
		s.handleError(m.CameraID, "request_keyframe은 아직 지원되지 않음")
	default:
		s.handleError("", "알 수 없는 메시지 타입: "+m.Type)
	}
}

// stopStream은 구독을 해제하고 스트림을 정지한다.
func (s *connState) stopStream(ctrl Controller, cameraID, reason string) {
	s.unsubscribe(cameraID)
	if err := ctrl.Stop(cameraID); err != nil {
		s.handleError(cameraID, err.Error())
		return
	}
	s.sendMsg(ServerMsg{Type: MsgStreamStopped, CameraID: cameraID, Reason: reason})
}

// unsubscribe는 구독만 해제한다.
func (s *connState) unsubscribe(cameraID string) {
	s.mu.Lock()
	cancel, ok := s.cancels[cameraID]
	if ok {
		delete(s.cancels, cameraID)
	}
	s.mu.Unlock()
	if ok {
		cancel()
	}
}

// cleanupAll은 연결 종료 시 모든 구독을 해제한다.
func (s *connState) cleanupAll() {
	s.mu.Lock()
	cancels := s.cancels
	s.cancels = map[string]func(){}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// heartbeat는 서버 주도 ping 프레임으로 연결을 감시한다.
func (s *connState) heartbeat(stop <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.wmu.Lock()
			_ = s.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
			s.wmu.Unlock()
		}
	}
}

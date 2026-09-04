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
	StartAll() error
	Subscribe(cameraID string) (<-chan stream.Event, func(), error)
	PTZ(cameraID string, cmd PTZCommand) error
	// ReloadStream은 실행 중인 RTSP 세션을 강제 종료한다. 구독자는 desired 상태에 따라
	// 자동 재시작되며, 재다이얼 시 변경된 카메라 설정이 반영된다.
	ReloadStream(cameraID string) error
}

// connState는 연결 하나의 상태다.
type connState struct {
	conn *websocket.Conn
	wmu  sync.Mutex // 쓰기 직렬화

	mu      sync.Mutex
	cancels map[string]func() // cameraID → 구독 해제
}

// sendRaw는 임의 메시지를 직렬화해 전송한다. (쓰기 락 보호)
// 쓰기 데드라인을 걸어 반쯤 끊긴 커넥션에서 WriteJSON이 wmu를 영구 점유하고
// pump/브로드캐스트를 정지시키는 것을 막는다.
func (s *connState) sendRaw(v any) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(writeWait))
	_ = s.conn.WriteJSON(v)
}

// sendMsg는 메시지를 직렬화해 전송한다. (쓰기 락 보호)
func (s *connState) sendMsg(m ServerMsg) {
	s.sendRaw(m)
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
		slog.Error("❌ 스트림 시작 실패", "camera", cameraID, "err", err)
		s.handleError(cameraID, err.Error())
		return
	}
	ch, cancel, err := ctrl.Subscribe(cameraID)
	if err != nil {
		slog.Error("❌ 구독 실패", "camera", cameraID, "err", err)
		s.handleError(cameraID, err.Error())
		return
	}
	slog.Info("▶️ 스트림 구독 시작", "camera", cameraID)

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
// RTP 패킷은 즉시 도착한 만큼 배치로 묶어 전송한다(고비트레이트 스트림 대응).
func (s *connState) pump(cameraID string, ch <-chan stream.Event, cancel func()) {
	defer func() {
		s.mu.Lock()
		delete(s.cancels, cameraID)
		s.mu.Unlock()
		cancel()
		slog.Info("⏹️ 스트림 종료", "camera", cameraID)
	}()

	codec := ""
	flushBatch := func(batch []RTPPacketItem) {
		if len(batch) == 0 {
			return
		}
		s.sendRaw(&RTPBatchMsg{
			Type:     MsgRTPBatch,
			CameraID: cameraID,
			Codec:    codec,
			Packets:  batch,
		})
	}

	batch := make([]RTPPacketItem, 0, 64)
	for ev := range ch {
		switch e := ev.(type) {
		case stream.StartedEvent:
			flushBatch(batch)
			batch = batch[:0]
			codec = string(e.Info.Codec)
			slog.Info("📤 스트림 시작 메시지 전송", "camera", cameraID, "codec", codec)
			s.sendMsg(streamStartedMsg(e.Info))
		case stream.PacketEvent:
			batch = append(batch, RTPPacketItem{
				Payload:   base64.StdEncoding.EncodeToString(e.Packet.Payload),
				Timestamp: e.Packet.Timestamp,
				Marker:    e.Packet.Marker,
				Sequence:  e.Packet.Sequence,
			})
			// 채널에 대기 중인 패킷을 즉시 흡수해 배치 크기를 키운다
		drain:
			for len(batch) < 128 {
				select {
				case ev2, ok := <-ch:
					if !ok {
						break drain
					}
					switch e2 := ev2.(type) {
					case stream.PacketEvent:
						batch = append(batch, RTPPacketItem{
							Payload:   base64.StdEncoding.EncodeToString(e2.Packet.Payload),
							Timestamp: e2.Packet.Timestamp,
							Marker:    e2.Packet.Marker,
							Sequence:  e2.Packet.Sequence,
						})
					case stream.StartedEvent:
						flushBatch(batch)
						batch = batch[:0]
						codec = string(e2.Info.Codec)
						s.sendMsg(streamStartedMsg(e2.Info))
					case stream.StoppedEvent:
						flushBatch(batch)
						s.sendMsg(ServerMsg{Type: MsgStreamStopped, CameraID: cameraID, Reason: e2.Reason})
						return
					}
				default:
					break drain
				}
			}
			flushBatch(batch)
			batch = batch[:0]
		case stream.StoppedEvent:
			flushBatch(batch)
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
		s.stopStream(m.CameraID, "사용자 정지")
	case MsgUnsubscribe:
		s.unsubscribe(m.CameraID)
	case MsgReloadStream:
		if err := ctrl.ReloadStream(m.CameraID); err != nil {
			s.handleError(m.CameraID, err.Error())
		}
	case MsgStartAllStreams:
		if err := ctrl.StartAll(); err != nil {
			s.handleError("", err.Error())
		}
	case MsgStopAllStreams:
		// v1.1 의미 변경: 전역 정지가 아니라 "이 클라이언트의 모든 구독 해제"
		for _, cameraID := range s.ownCameraIDs() {
			s.stopStream(cameraID, "사용자 정지")
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

// ownCameraIDs는 이 연결이 구독 중인 카메라 ID 목록을 반환한다.
func (s *connState) ownCameraIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.cancels))
	for id := range s.cancels {
		out = append(out, id)
	}
	return out
}

// stopStream은 이 연결의 구독만 해제한다.
// RTSP 세션은 허브의 참조 카운팅이 관리 — 다른 클라이언트 구독에는 영향 없다.
func (s *connState) stopStream(cameraID, reason string) {
	s.unsubscribe(cameraID)
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

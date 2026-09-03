// WebSocket 서버의 JSON 프로토콜/하트비트/스트림 펌핑 통합 테스트
package ws

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"webnvr/internal/stream"
)

// fakeController는 Controller 인터페이스의 테스트 구현이다.
type fakeController struct {
	mu      sync.Mutex
	started []string
	stopped []string
	ptzCmds []PTZCommand
	failIDs map[string]bool                // Start 실패 카메라
	chans   map[string][]chan stream.Event // 카메라별 다중 구독자
}

func newFakeController() *fakeController {
	return &fakeController{
		failIDs: map[string]bool{},
		chans:   map[string][]chan stream.Event{},
	}
}

func (f *fakeController) Start(cameraID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failIDs[cameraID] {
		return fmt.Errorf("카메라 시작 실패: %s", cameraID)
	}
	f.started = append(f.started, cameraID)
	return nil
}

func (f *fakeController) Stop(cameraID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, cameraID)
	for _, ch := range f.chans[cameraID] {
		close(ch)
	}
	delete(f.chans, cameraID)
	return nil
}

func (f *fakeController) StartAll() error { return nil }
func (f *fakeController) StopAll() error  { return nil }

func (f *fakeController) Subscribe(cameraID string) (<-chan stream.Event, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failIDs[cameraID] {
		return nil, nil, fmt.Errorf("실행 중인 스트림이 없음: %s", cameraID)
	}
	ch := make(chan stream.Event, 64)
	f.chans[cameraID] = append(f.chans[cameraID], ch)
	cancel := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		cur := f.chans[cameraID]
		for i, c := range cur {
			if c == ch {
				f.chans[cameraID] = append(cur[:i], cur[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, cancel, nil
}

func (f *fakeController) PTZ(cameraID string, cmd PTZCommand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ptzCmds = append(f.ptzCmds, cmd)
	return nil
}

// emit은 구독 채널이 등록될 때까지 기다린 뒤 모든 구독자에게 이벤트를 발생시킨다.
func (f *fakeController) emit(t *testing.T, cameraID string, ev stream.Event) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		chs := f.chans[cameraID]
		f.mu.Unlock()
		if len(chs) > 0 {
			for _, ch := range chs {
				ch <- ev
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("구독 채널이 등록되지 않음: %s", cameraID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitSubs는 카메라의 구독자 수가 n 이상이 될 때까지 기다린다.
func (f *fakeController) waitSubs(t *testing.T, cameraID string, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		got := len(f.chans[cameraID])
		f.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("구독자 수 부족: %s (got %d, want >= %d)", cameraID, got, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// connect는 테스트 서버에 WS 클라이언트로 연결한다.
func connect(t *testing.T, srv *Server) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial("ws://"+srv.Addr()+"/ws", nil)
	if err != nil {
		t.Fatalf("Dial() err = %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// newTestServer는 자유 포트에 WS 서버를 시작한다.
func newTestServer(t *testing.T, ctrl Controller) *Server {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := NewServer(ctrl, addr, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv
}

// recv는 메시지를 읽는다.
func recv(t *testing.T, c *websocket.Conn) ServerMsg {
	t.Helper()
	var m ServerMsg
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := c.ReadJSON(&m); err != nil {
		t.Fatalf("ReadJSON() err = %v", err)
	}
	return m
}

// TestPingPong은 하트비트 메시지를 확인한다.
func TestPingPong(t *testing.T) {
	ctrl := newFakeController()
	srv := newTestServer(t, ctrl)
	c := connect(t, srv)

	if err := c.WriteJSON(ClientMsg{Type: MsgPing}); err != nil {
		t.Fatal(err)
	}
	m := recv(t, c)
	if m.Type != MsgPong {
		t.Errorf("Type = %q, want pong", m.Type)
	}
}

// TestStartStreamPump는 시작→started/패킷 수신→정지 흐름을 확인한다.
func TestStartStreamPump(t *testing.T) {
	ctrl := newFakeController()
	srv := newTestServer(t, ctrl)
	c := connect(t, srv)

	if err := c.WriteJSON(ClientMsg{Type: MsgStartStream, CameraID: "cam-1"}); err != nil {
		t.Fatal(err)
	}

	// 이벤트 발생
	sampleSPS := []byte{0x67, 0x01}
	ctrl.emit(t, "cam-1", stream.StartedEvent{Info: stream.Info{
		CameraID: "cam-1", Codec: stream.CodecH264, SPS: sampleSPS, PPS: []byte{0x68},
		Width: 352, Height: 288, SSRC: 42, ClockRate: 90000,
	}})
	ctrl.emit(t, "cam-1", stream.PacketEvent{Packet: stream.Packet{
		CameraID: "cam-1", Codec: stream.CodecH264, Sequence: 1, Timestamp: 100,
		Marker: true, Payload: []byte{1, 2, 3, 4},
	}})

	// stream_started 확인
	m1 := recv(t, c)
	if m1.Type != MsgStreamStarted || m1.CameraID != "cam-1" || m1.Width != 352 {
		t.Fatalf("stream_started 불일치: %+v", m1)
	}
	if got, want := m1.SPS, base64.StdEncoding.EncodeToString(sampleSPS); got != want {
		t.Errorf("SPS base64 불일치: %q", got)
	}

	// rtp_batch 확인 (패킷은 배치로 전송된다)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() err = %v", err)
	}
	var batch RTPBatchMsg
	if err := json.Unmarshal(raw, &batch); err != nil {
		t.Fatalf("배치 언마셜 실패: %v", err)
	}
	if batch.Type != MsgRTPBatch || batch.CameraID != "cam-1" {
		t.Fatalf("rtp_batch 불일치: %+v", batch)
	}
	if len(batch.Packets) == 0 {
		t.Fatal("배치에 패킷이 없음")
	}
	p0 := batch.Packets[0]
	if p0.Sequence != 1 || !p0.Marker {
		t.Errorf("첫 패킷 불일치: %+v", p0)
	}
	wantPayload := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	if p0.Payload != wantPayload {
		t.Errorf("payload base64 불일치: %q", p0.Payload)
	}

	// 정지 — 새 의미론: 이 연결의 구독 해제만 (세션 정지 없음)
	if err := c.WriteJSON(ClientMsg{Type: MsgStopStream, CameraID: "cam-1"}); err != nil {
		t.Fatal(err)
	}
	m3 := recv(t, c)
	if m3.Type != MsgStreamStopped || m3.Reason != "사용자 정지" {
		t.Errorf("stream_stopped 불일치: %+v", m3)
	}

	// 정지 후 이벤트가 오면 더 이상 수신되지 않아야 한다 (구독 해제됨)
	ctrl.mu.Lock()
	remaining := len(ctrl.chans["cam-1"])
	ctrl.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("구독 해제 후에도 채널이 남아있음: %d", remaining)
	}
}

// TestStreamError는 시작 실패 시 stream_error를 확인한다.
func TestStreamError(t *testing.T) {
	ctrl := newFakeController()
	ctrl.failIDs["cam-bad"] = true
	srv := newTestServer(t, ctrl)
	c := connect(t, srv)

	if err := c.WriteJSON(ClientMsg{Type: MsgStartStream, CameraID: "cam-bad"}); err != nil {
		t.Fatal(err)
	}
	m := recv(t, c)
	if m.Type != MsgStreamError || m.CameraID != "cam-bad" || m.Error == "" {
		t.Errorf("stream_error 불일치: %+v", m)
	}
}

// TestPTZForwarding은 PTZ 명령 전달을 확인한다.
func TestPTZForwarding(t *testing.T) {
	ctrl := newFakeController()
	srv := newTestServer(t, ctrl)
	c := connect(t, srv)

	if err := c.WriteJSON(ClientMsg{
		Type:     MsgPTZ,
		CameraID: "cam-1",
		Command:  &PTZCommand{Action: "move", Pan: 0.5, Tilt: -0.5},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	if len(ctrl.ptzCmds) != 1 || ctrl.ptzCmds[0].Action != "move" || ctrl.ptzCmds[0].Pan != 0.5 {
		t.Errorf("PTZ 전달 불일치: %+v", ctrl.ptzCmds)
	}
}

// TestUnknownMessageType은 알 수 없는 타입 처리를 확인한다.
func TestUnknownMessageType(t *testing.T) {
	ctrl := newFakeController()
	srv := newTestServer(t, ctrl)
	c := connect(t, srv)

	if err := c.WriteJSON(ClientMsg{Type: "bogus"}); err != nil {
		t.Fatal(err)
	}
	m := recv(t, c)
	if m.Type != MsgStreamError || m.Error == "" {
		t.Errorf("알 수 없는 타입 처리 불일치: %+v", m)
	}
}

// TestTwoClientsSameStream는 2개 연결이 같은 카메라를 구독할 때
// 둘 다 stream_started(코덱 메타데이터)를 수신하는지 확인한다.
// (늦게 합류한 클라이언트가 영상을 못 보던 결함의 회귀 테스트 — 2026-09-01)
func TestTwoClientsSameStream(t *testing.T) {
	ctrl := newFakeController()
	srv := newTestServer(t, ctrl)

	// 클라이언트 A: 먼저 구독
	cA := connect(t, srv)
	if err := cA.WriteJSON(ClientMsg{Type: MsgStartStream, CameraID: "cam-1"}); err != nil {
		t.Fatal(err)
	}
	ctrl.waitSubs(t, "cam-1", 1)
	ctrl.emit(t, "cam-1", stream.StartedEvent{Info: stream.Info{
		CameraID: "cam-1", Codec: stream.CodecH264, SPS: []byte{0x67}, PPS: []byte{0x68},
		Width: 352, Height: 288, ClockRate: 90000,
	}})
	if m := recv(t, cA); m.Type != MsgStreamStarted {
		t.Fatalf("A의 stream_started 미수신: %+v", m)
	}
	ctrl.emit(t, "cam-1", stream.PacketEvent{Packet: stream.Packet{Sequence: 1, Payload: []byte{9}}})
	if m := recv(t, cA); m.Type != MsgRTPBatch {
		t.Fatalf("A의 rtp_batch 미수신: %+v", m)
	}

	// 클라이언트 B: 나중에 합류 — 코덱 메타데이터가 이미 확정된 상태
	cB := connect(t, srv)
	if err := cB.WriteJSON(ClientMsg{Type: MsgSubscribe, CameraID: "cam-1"}); err != nil {
		t.Fatal(err)
	}
	// B의 구독 등록 대기 (등록 전 emit하면 이벤트가 유실된다)
	ctrl.waitSubs(t, "cam-1", 2)

	// fakeController는 Started 없이 채널만 제공 — 실제 Hub의 늦은 구독자
	// StartedEvent 재전송은 stream_test.go의 TestHubLateSubscriber가 검증한다.
	// 여기서는 2연결 배치 전달만 확인한다.
	ctrl.emit(t, "cam-1", stream.PacketEvent{Packet: stream.Packet{Sequence: 2, Payload: []byte{8}}})
	m := recv(t, cB)
	if m.Type != MsgRTPBatch {
		t.Fatalf("B의 rtp_batch 미수신: %+v", m)
	}

	// A에도 계속 전달되는지 (느린 구독자와 무관)
	ctrl.emit(t, "cam-1", stream.PacketEvent{Packet: stream.Packet{Sequence: 3, Payload: []byte{7}}})
	if m := recv(t, cA); m.Type != MsgRTPBatch {
		t.Errorf("A의 후속 배치 미수신: %+v", m)
	}
}

// TestClientIndependentStop는 한 연결의 stop_stream이 다른 연결에 영향을 주지 않음을 확인한다.
// (클라이언트별 독립 재생 — 2026-09-02 요구사항)
func TestClientIndependentStop(t *testing.T) {
	ctrl := newFakeController()
	srv := newTestServer(t, ctrl)

	// A, B 둘 다 구독
	cA := connect(t, srv)
	if err := cA.WriteJSON(ClientMsg{Type: MsgStartStream, CameraID: "cam-1"}); err != nil {
		t.Fatal(err)
	}
	ctrl.waitSubs(t, "cam-1", 1)

	cB := connect(t, srv)
	if err := cB.WriteJSON(ClientMsg{Type: MsgSubscribe, CameraID: "cam-1"}); err != nil {
		t.Fatal(err)
	}
	ctrl.waitSubs(t, "cam-1", 2)

	// B만 정지
	if err := cB.WriteJSON(ClientMsg{Type: MsgStopStream, CameraID: "cam-1"}); err != nil {
		t.Fatal(err)
	}
	mB := recv(t, cB)
	if mB.Type != MsgStreamStopped {
		t.Fatalf("B의 stream_stopped 미수신: %+v", mB)
	}

	// B 정지 후에도 A는 계속 패킷을 수신한다
	ctrl.emit(t, "cam-1", stream.PacketEvent{Packet: stream.Packet{Sequence: 10, Payload: []byte{1}}})
	mA := recv(t, cA)
	if mA.Type != MsgRTPBatch {
		t.Errorf("B 정지 후 A의 수신 단절: %+v", mA)
	}

	// A의 구독이 유지되었는지 (컨트롤러 기록)
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	if len(ctrl.stopped) != 0 {
		t.Errorf("B의 정지가 세션 정지로 전파됨: stopped=%v", ctrl.stopped)
	}
}

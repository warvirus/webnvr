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
	failIDs map[string]bool // Start 실패 카메라
	chans   map[string]chan stream.Event
}

func newFakeController() *fakeController {
	return &fakeController{
		failIDs: map[string]bool{},
		chans:   map[string]chan stream.Event{},
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
	if ch, ok := f.chans[cameraID]; ok {
		close(ch)
		delete(f.chans, cameraID)
	}
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
	f.chans[cameraID] = ch
	cancel := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if cur, ok := f.chans[cameraID]; ok && cur == ch {
			close(cur)
			delete(f.chans, cameraID)
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

// emit은 구독 채널이 등록될 때까지 기다린 뒤 이벤트를 발생시킨다.
func (f *fakeController) emit(t *testing.T, cameraID string, ev stream.Event) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		ch := f.chans[cameraID]
		f.mu.Unlock()
		if ch != nil {
			ch <- ev
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("구독 채널이 등록되지 않음: %s", cameraID)
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

	srv := NewServer(ctrl, addr)
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

	// 정지
	if err := c.WriteJSON(ClientMsg{Type: MsgStopStream, CameraID: "cam-1"}); err != nil {
		t.Fatal(err)
	}
	m3 := recv(t, c)
	if m3.Type != MsgStreamStopped || m3.Reason != "사용자 정지" {
		t.Errorf("stream_stopped 불일치: %+v", m3)
	}

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	if len(ctrl.stopped) != 1 || ctrl.stopped[0] != "cam-1" {
		t.Errorf("Controller.Stop 호출 기록: %v", ctrl.stopped)
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

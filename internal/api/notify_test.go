// 카메라/설정 변경 브로드캐스트 배선 테스트
package api

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"webnvr/internal/ws"
)

// fakeBroadcaster는 changeBroadcaster의 호출을 기록한다.
type fakeBroadcaster struct {
	mu      sync.Mutex
	cameras [][2]string // {reason, cameraID}
	configs int
}

func (f *fakeBroadcaster) BroadcastCamerasChanged(reason, cameraID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cameras = append(f.cameras, [2]string{reason, cameraID})
}

func (f *fakeBroadcaster) BroadcastConfigChanged() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configs++
}

func (f *fakeBroadcaster) last() [2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cameras) == 0 {
		return [2]string{}
	}
	return f.cameras[len(f.cameras)-1]
}

// TestCameraMutationBroadcasts는 CRUD/재정렬/설정 저장이 올바른 reason으로 브로드캐스트되는지 확인한다.
func TestCameraMutationBroadcasts(t *testing.T) {
	app, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}
	fake := &fakeBroadcaster{}
	app.Camera.notifier = fake

	saved, err := app.Camera.CreateCamera(CreateCameraRequest{
		Name: "정문", Type: "rtsp", StreamURL: "rtsp://127.0.0.1:554/s",
	})
	if err != nil {
		t.Fatalf("CreateCamera() err = %v", err)
	}
	if got := fake.last(); got != [2]string{"added", saved.ID} {
		t.Errorf("생성 브로드캐스트 = %v, want [added %s]", got, saved.ID)
	}

	name := "정문 카메라"
	if _, err := app.Camera.UpdateCamera(saved.ID, UpdateCameraRequest{Name: &name}); err != nil {
		t.Fatalf("UpdateCamera() err = %v", err)
	}
	if got := fake.last(); got != [2]string{"updated", saved.ID} {
		t.Errorf("수정 브로드캐스트 = %v", got)
	}

	if err := app.Camera.ReorderCameras([]string{saved.ID}); err != nil {
		t.Fatalf("ReorderCameras() err = %v", err)
	}
	if got := fake.last(); got != [2]string{"reordered", ""} {
		t.Errorf("재정렬 브로드캐스트 = %v", got)
	}

	if _, err := app.Camera.UpdateAppConfig(map[string]any{}); err != nil {
		t.Fatalf("UpdateAppConfig() err = %v", err)
	}
	if fake.configs != 1 {
		t.Errorf("config 브로드캐스트 횟수 = %d, want 1", fake.configs)
	}

	if err := app.Camera.DeleteCamera(saved.ID); err != nil {
		t.Fatalf("DeleteCamera() err = %v", err)
	}
	if got := fake.last(); got != [2]string{"deleted", saved.ID} {
		t.Errorf("삭제 브로드캐스트 = %v", got)
	}

	// 실패한 mutation은 브로드캐스트하지 않는다
	before := len(fake.cameras)
	if err := app.Camera.DeleteCamera("cam-nope"); err == nil {
		t.Fatal("없는 카메라 삭제가 성공함")
	}
	if len(fake.cameras) != before {
		t.Errorf("실패한 삭제가 브로드캐스트됨")
	}
}

// TestBroadcastReachesWSClients는 App → ws.Server 배선으로 실제 WS 클라이언트가
// cameras_changed를 수신하는지 확인한다.
func TestBroadcastReachesWSClients(t *testing.T) {
	app, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := ws.NewServer(app.Stream, addr, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("ws Start() err = %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	app.Camera.notifier = srv

	dial := func() *websocket.Conn {
		c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
		if err != nil {
			t.Fatalf("Dial() err = %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		// ping/pong으로 서버 등록 보장
		if err := c.WriteJSON(ws.ClientMsg{Type: ws.MsgPing}); err != nil {
			t.Fatal(err)
		}
		var m ws.ServerMsg
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := c.ReadJSON(&m); err != nil || m.Type != ws.MsgPong {
			t.Fatalf("동기화 실패: %+v err=%v", m, err)
		}
		return c
	}
	cA, cB := dial(), dial()

	if _, err := app.Camera.CreateCamera(CreateCameraRequest{
		Name: "정문", Type: "rtsp", StreamURL: "rtsp://127.0.0.1:554/s",
	}); err != nil {
		t.Fatalf("CreateCamera() err = %v", err)
	}

	for name, c := range map[string]*websocket.Conn{"A": cA, "B": cB} {
		var m ws.ServerMsg
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := c.ReadJSON(&m); err != nil {
			t.Fatalf("%s ReadJSON() err = %v", name, err)
		}
		if m.Type != ws.MsgCamerasChanged || m.Reason != "added" {
			t.Errorf("%s 수신 = %+v, want cameras_changed/added", name, m)
		}
	}
}

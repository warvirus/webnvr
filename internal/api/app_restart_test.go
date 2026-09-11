// Server 설정 변경(ws_port/bind) 시 리스너 재바인딩을 검증한다.
package api

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func getHealth(t *testing.T, port int) (int, error) {
	t.Helper()
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// TestApplyServerRestartRebindsPort — ws_port 변경 시 새 포트로 무중단 재바인딩되고
// 구포트는 닫힌다.
func TestApplyServerRestartRebindsPort(t *testing.T) {
	portA, portB := freePort(t), freePort(t)
	app, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}
	t.Cleanup(func() { app.Close() })

	app.Camera.appCfg.Server.WSPort = portA
	app.Camera.appCfg.Server.Bind = "127.0.0.1"
	if err := app.StartWSServer(nil); err != nil {
		t.Fatalf("StartWSServer() err = %v", err)
	}
	if code, _ := getHealth(t, portA); code != 200 {
		t.Fatalf("구포트 health = %d", code)
	}

	old := app.Camera.appCfg.Server
	app.Camera.appCfg.Server.WSPort = portB
	app.ApplyServerRestart(old)

	if code, err := getHealth(t, portB); err != nil || code != 200 {
		t.Fatalf("새 포트 health = %d err = %v, want 200", code, err)
	}
	if _, err := getHealth(t, portA); err == nil {
		t.Error("구포트가 아직 서빙 중 — 구서버가 닫히지 않음")
	}
	if app.BackendPort() != portB {
		t.Errorf("BackendPort = %d, want %d", app.BackendPort(), portB)
	}
}

// TestApplyServerRestartKeepsOldOnBindFailure — 새 포트 바인딩 실패 시
// 기존 포트로 계속 서빙한다 (서버가 죽지 않음).
func TestApplyServerRestartKeepsOldOnBindFailure(t *testing.T) {
	portA, portB := freePort(t), freePort(t)
	app, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}
	t.Cleanup(func() { app.Close() })

	app.Camera.appCfg.Server.WSPort = portA
	app.Camera.appCfg.Server.Bind = "127.0.0.1"
	if err := app.StartWSServer(nil); err != nil {
		t.Fatalf("StartWSServer() err = %v", err)
	}

	// portB를 미리 점유해 재바인딩 실패를 만든다
	blocker, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", portB))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { blocker.Close() })

	old := app.Camera.appCfg.Server
	app.Camera.appCfg.Server.WSPort = portB
	app.ApplyServerRestart(old)

	if code, err := getHealth(t, portA); err != nil || code != 200 {
		t.Fatalf("바인딩 실패 후 구포트 health = %d err = %v, want 200 (서버 유지)", code, err)
	}
	if app.BackendPort() != portB {
		t.Errorf("BackendPort = %d, want %d (DB 저장값 유지)", app.BackendPort(), portB)
	}
}

// /api/* REST 핸들러의 단위 테스트 — httptest로 CRUD/오류 케이스를 검증한다
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// newTestApp은 임시 설정 디렉토리 기반의 App과 테스트 서버를 만든다.
func newTestApp(t *testing.T) *httptest.Server {
	t.Helper()
	_, srv := newTestAppFull(t)
	_ = srv
	return srv
}

// newTestAppFull은 App 자체도 함께 반환한다. (녹화 매니저 접근 등 내부 검증용)
func newTestAppFull(t *testing.T) (*App, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	app, err := New(dir)
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/ws", app.wsServer.Mux())
	RegisterHTTP(mux, app)
	RegisterRecordingHTTP(mux, app)

	srv := httptest.NewServer(CORS(mux))
	t.Cleanup(srv.Close)
	return app, srv
}

// obj는 any를 map으로 단언한다.
func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("객체가 아닌 응답: %T (%v)", v, v)
	}
	return m
}

func doJSON(t *testing.T, method, url string, body any) (int, any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// TestHTTPHealth는 /api/health를 확인한다.
func TestHTTPHealth(t *testing.T) {
	srv := newTestApp(t)
	status, bodyAny := doJSON(t, http.MethodGet, srv.URL+"/api/health", nil)
	body := obj(t, bodyAny)
	if status != 200 || body["ok"] != true {
		t.Fatalf("health 불일치: %d %v", status, body)
	}
}

// TestHTTPCameraCRUD는 등록→조회→수정→삭제 전체 흐름을 확인한다.
func TestHTTPCameraCRUD(t *testing.T) {
	srv := newTestApp(t)

	// 생성
	status, saved := doJSON(t, http.MethodPost, srv.URL+"/api/cameras", map[string]any{
		"name": "정문", "type": "onvif", "xaddr": "192.168.0.217:8090",
		"username": "1", "password": "pw", "ptzSupported": true,
	})
	if status != 201 {
		t.Fatalf("생성 상태 = %d, body=%v", status, saved)
	}
	savedMap := obj(t, saved)
	id, _ := savedMap["id"].(string)
	if id == "" {
		t.Fatal("생성 응답에 id 없음")
	}
	if savedMap["hasPassword"] != true {
		t.Error("hasPassword 미설정")
	}
	if savedMap["password"] != nil {
		t.Error("응답에 비밀번호 노출됨")
	}

	// 목록
	status, list := doJSON(t, http.MethodGet, srv.URL+"/api/cameras", nil)
	if status != 200 {
		t.Fatalf("목록 상태 = %d", status)
	}
	cams, _ := list.([]any)
	if len(cams) != 1 {
		t.Fatalf("목록 수 = %d, want 1", len(cams))
	}

	// 단일 조회
	status, gotAny := doJSON(t, http.MethodGet, srv.URL+"/api/cameras/"+id, nil)
	got := obj(t, gotAny)
	if status != 200 || got["name"] != "정문" {
		t.Fatalf("조회 실패: %d %v", status, got)
	}

	// 수정
	status, updatedAny := doJSON(t, http.MethodPut, srv.URL+"/api/cameras/"+id, map[string]any{
		"name": "정문 카메라",
	})
	updated := obj(t, updatedAny)
	if status != 200 || updated["name"] != "정문 카메라" {
		t.Fatalf("수정 실패: %d %v", status, updated)
	}

	// 존재하지 않는 카메라 수정 → 404
	status, _ = doJSON(t, http.MethodPut, srv.URL+"/api/cameras/cam-nope", map[string]any{"name": "x"})
	if status != 404 {
		t.Errorf("미존재 수정 상태 = %d, want 404", status)
	}

	// 삭제
	status, _ = doJSON(t, http.MethodDelete, srv.URL+"/api/cameras/"+id, nil)
	if status != 200 {
		t.Fatalf("삭제 상태 = %d", status)
	}
	status, _ = doJSON(t, http.MethodGet, srv.URL+"/api/cameras/"+id, nil)
	if status != 404 {
		t.Errorf("삭제 후 조회 상태 = %d, want 404", status)
	}
}

// TestHTTPReorder는 순서 변경 엔드포인트를 확인한다.
func TestHTTPReorder(t *testing.T) {
	srv := newTestApp(t)
	var ids []string
	for _, name := range []string{"a", "b", "c"} {
		_, savedAny := doJSON(t, http.MethodPost, srv.URL+"/api/cameras", map[string]any{
			"name": name, "type": "rtsp", "streamUrl": "rtsp://127.0.0.1:9/x",
		})
		saved := obj(t, savedAny)
		id, _ := saved["id"].(string)
		ids = append(ids, id)
	}

	status, _ := doJSON(t, http.MethodPost, srv.URL+"/api/cameras/reorder", map[string]any{
		"ids": []string{ids[2], ids[0], ids[1]},
	})
	if status != 200 {
		t.Fatalf("reorder 상태 = %d", status)
	}

	status, list := doJSON(t, http.MethodGet, srv.URL+"/api/cameras", nil)
	cams, _ := list.([]any)
	if status != 200 || len(cams) != 3 {
		t.Fatalf("목록 조회 실패: %d", status)
	}
	first, _ := cams[0].(map[string]any)
	if first["name"] != "c" {
		t.Errorf("첫 번째 카메라 = %v, want c", first["name"])
	}
}

// TestHTTPValidation은 잘못된 요청의 오류 처리를 확인한다.
func TestHTTPValidation(t *testing.T) {
	srv := newTestApp(t)

	// 생성 검증 실패 → 400
	status, bodyAny := doJSON(t, http.MethodPost, srv.URL+"/api/cameras", map[string]any{
		"name": "", "type": "rtsp",
	})
	body := obj(t, bodyAny)
	if status != 400 || body["error"] == nil {
		t.Errorf("검증 오류 상태 = %d, body=%v", status, body)
	}

	// 잘못된 JSON → 400
	res, err := http.Post(srv.URL+"/api/cameras", "application/json", bytes.NewReader([]byte("{bad")))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Errorf("잘못된 JSON 상태 = %d, want 400", res.StatusCode)
	}

	// 지원하지 않는 메서드 → 405
	status, _ = doJSON(t, http.MethodDelete, srv.URL+"/api/health", nil)
	if status != 405 {
		t.Errorf("405 상태 = %d", status)
	}

	// 알 수 없는 경로 → 404
	status, _ = doJSON(t, http.MethodGet, srv.URL+"/api/unknown", nil)
	if status != 404 {
		t.Errorf("404 상태 = %d", status)
	}
}

// TestHTTPRegisteredOnlyEndpoints는 등록 카메라 대상 조회 엔드포인트의 404를 확인한다.
// 실제 ONVIF 조회는 카메라 서버가 필요하므로 존재 여부까지만 검증한다.
func TestHTTPRegisteredOnlyEndpoints(t *testing.T) {
	srv := newTestApp(t)
	for _, path := range []string{
		"/api/cameras/cam-nope/profiles",
		"/api/cameras/cam-nope/presets",
		"/api/cameras/cam-nope/stream-uri",
	} {
		status, bodyAny := doJSON(t, http.MethodGet, srv.URL+path, nil)
		if status != 404 {
			t.Errorf("%s 상태 = %d, want 404 (body=%v)", path, status, bodyAny)
		}
	}
}

// TestCORSPreflight는 OPTIONS 프리플라이트를 확인한다.
func TestCORSPreflight(t *testing.T) {
	srv := newTestApp(t)
	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/api/cameras", nil)
	req.Header.Set("Origin", "http://localhost:34115")
	req.Header.Set("Access-Control-Request-Method", "POST")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Errorf("프리플라이트 상태 = %d, want 204", res.StatusCode)
	}
	if res.Header.Get("Access-Control-Allow-Origin") == "" {
		t.Error("CORS 헤더 누락")
	}
}

// 빈 설정 디렉토리로 App이 정상 기동하는지 확인한다 (기본 파일 자동 생성).
func TestNewAppEmptyDir(t *testing.T) {
	dir := t.TempDir()
	app, err := New(dir)
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}
	_ = app
	_ = filepath.Join(dir, "app.json")
}

// TestHTTPConfig는 앱 설정 조회/수정을 확인한다.
func TestHTTPConfig(t *testing.T) {
	srv := newTestApp(t)

	status, cfgAny := doJSON(t, http.MethodGet, srv.URL+"/api/config", nil)
	cfg := obj(t, cfgAny)
	if status != 200 || cfg["version"] != float64(1) {
		t.Fatalf("설정 조회 실패: %d %v", status, cfg)
	}

	// 유효한 전체 교체
	status, savedAny := doJSON(t, http.MethodPut, srv.URL+"/api/config", map[string]any{
		"version": 1,
		"server":  map[string]any{"ws_port": 8080, "http_port": 8081},
		"stream":  map[string]any{"default_transport": "udp", "rtp_timeout_ms": 5000, "jitter_buffer_ms": 150, "max_concurrent_streams": 20},
		"decoder": map[string]any{"prefer_hardware": true, "max_threads": 4},
		"logging": map[string]any{"level": "info", "file": "logs/app.log", "max_size_mb": 100, "max_backups": 5},
	})
	if status != 200 {
		t.Fatalf("설정 수정 실패: %d", status)
	}
	saved := obj(t, savedAny)
	if saved["stream"].(map[string]any)["default_transport"] != "udp" {
		t.Errorf("transport 반영 안 됨: %v", saved["stream"])
	}

	// 유효하지 않은 설정 → 400
	status, _ = doJSON(t, http.MethodPut, srv.URL+"/api/config", map[string]any{
		"version": 1, "server": map[string]any{"ws_port": 0, "http_port": 8081},
	})
	if status != 400 {
		t.Errorf("잘못된 설정 상태 = %d, want 400", status)
	}
}

// TestHTTPBackupRestore는 백업 내보내기/복원을 확인한다.
func TestHTTPBackupRestore(t *testing.T) {
	srv := newTestApp(t)

	// 카메라 2대 등록
	for _, name := range []string{"a", "b"} {
		s, _ := doJSON(t, http.MethodPost, srv.URL+"/api/cameras", map[string]any{
			"name": name, "type": "rtsp", "streamUrl": "rtsp://127.0.0.1:9/x",
		})
		_ = s
	}

	// 백업 내보내기
	status, backupAny := doJSON(t, http.MethodGet, srv.URL+"/api/backup", nil)
	backup := obj(t, backupAny)
	if status != 200 {
		t.Fatalf("백업 상태 = %d", status)
	}
	cams, _ := backup["cameras"].([]any)
	if len(cams) != 2 {
		t.Fatalf("백업 카메라 수 = %d", len(cams))
	}
	// 비밀번호 필드가 백업에 없어야 한다
	c0 := cams[0].(map[string]any)
	if _, exists := c0["password"]; exists {
		t.Error("백업에 비밀번호 포함됨")
	}

	// 삭제 후 복원
	for _, c := range cams {
		doJSON(t, http.MethodDelete, srv.URL+"/api/cameras/"+c.(map[string]any)["id"].(string), nil)
	}
	status, resAny := doJSON(t, http.MethodPost, srv.URL+"/api/backup/restore", backup)
	res := obj(t, resAny)
	if status != 200 || res["restored"] != float64(2) {
		t.Fatalf("복원 실패: %d %v", status, res)
	}

	// 복원 결과 확인 (비밀번호 없음 → hasPassword false)
	status, listAny := doJSON(t, http.MethodGet, srv.URL+"/api/cameras", nil)
	list := listAny.([]any)
	if status != 200 || len(list) != 2 {
		t.Fatalf("복원 후 목록: %d (%d대)", status, len(list))
	}
	c1 := list[0].(map[string]any)
	if c1["hasPassword"] != false {
		t.Error("복원된 카메라에 hasPassword=true")
	}
}

// TestHTTPSecurity는 보안 상태 엔드포인트를 확인한다.
func TestHTTPSecurity(t *testing.T) {
	srv := newTestApp(t)
	status, secAny := doJSON(t, http.MethodGet, srv.URL+"/api/security", nil)
	sec := obj(t, secAny)
	if status != 200 || sec["masterKeySource"] == "" {
		t.Fatalf("보안 상태 실패: %d %v", status, sec)
	}
}

package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"webnvr/internal/camera"
)

func TestOpen_FreshSchema(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	if _, err := os.Stat(filepath.Join(dir, DBFile)); err != nil {
		t.Fatalf("webnvr.db 미생성: %v", err)
	}
	var ver int
	if err := d.SQL().QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != len(migrations) {
		t.Fatalf("user_version=%d, 기대 %d", ver, len(migrations))
	}
	// 마이그레이션 #2 — 녹화 테이블 존재 확인
	for _, tbl := range []string{"segments", "events"} {
		var name string
		err := d.SQL().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&name)
		if err != nil {
			t.Fatalf("%s 테이블 없음: %v", tbl, err)
		}
	}
	if _, err := d.SQL().Exec(
		`INSERT INTO segments(camera_id, start_ts, rel_path) VALUES ('cam-1', 1000, 'a/b.ts')`); err != nil {
		t.Fatalf("segments INSERT: %v", err)
	}

	// 재열기 = 무동작
	d.Close()
	d2, err := Open(dir)
	if err != nil {
		t.Fatalf("재Open: %v", err)
	}
	d2.Close()
}

func TestConfig_DefaultThenRoundtrip(t *testing.T) {
	dir := t.TempDir()
	d, _ := Open(dir)
	defer d.Close()

	cfg, err := d.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Version != 1 {
		t.Fatalf("기본 설정 Version=1 기대, got %d", cfg.Version)
	}
	var n int
	if err := d.SQL().QueryRow("SELECT COUNT(*) FROM app_config").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("app_config 행 1개 기대(기본값 저장됨), got %d", n)
	}

	cfg.Server.WSPort = 9123
	if err := d.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	d.Close()

	d2, _ := Open(dir)
	defer d2.Close()
	got, err := d2.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Server.WSPort != 9123 {
		t.Fatalf("저장한 ws_port 미반영: %d", got.Server.WSPort)
	}
}

func TestMigrateJSON(t *testing.T) {
	dir := t.TempDir()

	camFile := camera.CamerasFile{
		Version: 1,
		Cameras: []camera.Camera{
			{ID: "cam-a", Name: "정문", Type: camera.TypeONVIF, XAddr: "10.0.0.1:80",
				Username: "admin", Password: "encrypted:abc", LayoutOrder: 0, Enabled: true},
			{ID: "cam-b", Name: "후문", Type: camera.TypeRTSP, StreamURL: "rtsp://10.0.0.2/s",
				LayoutOrder: 1, Enabled: false},
		},
		Groups: []camera.Group{{ID: "g1", Name: "방범", LayoutType: "grid", LayoutCols: 2, LayoutRows: 2}},
	}
	b, _ := json.MarshalIndent(camFile, "", "  ")
	writeFile(t, filepath.Join(dir, "cameras.json"), b)
	writeFile(t, filepath.Join(dir, "app.json"), []byte(`{"version":1,"server":{"ws_port":8888}}`))

	d, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(이관): %v", err)
	}
	defer d.Close()

	cams, err := camera.NewSQLCameraStore(d.SQL()).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 2 || cams[0].ID != "cam-a" || cams[1].ID != "cam-b" {
		t.Fatalf("카메라 이관 실패: %+v", cams)
	}
	if cams[0].Password != "encrypted:abc" {
		t.Fatalf("비밀번호 이관 실패: %q", cams[0].Password)
	}
	if cams[1].Enabled {
		t.Fatal("enabled=false 이관 실패")
	}

	cfg, _ := d.LoadConfig()
	if cfg.Server.WSPort != 8888 {
		t.Fatalf("app.json 이관 실패: ws_port=%d", cfg.Server.WSPort)
	}

	// 원본은 .bak으로 보존, 원본 경로는 사라짐
	assertGone(t, filepath.Join(dir, "cameras.json"))
	assertExists(t, filepath.Join(dir, "cameras.json.bak"))
	assertGone(t, filepath.Join(dir, "app.json"))
	assertExists(t, filepath.Join(dir, "app.json.bak"))

	// 재열기 시 재이관 안 함
	d.Close()
	d2, err := Open(dir)
	if err != nil {
		t.Fatalf("재Open: %v", err)
	}
	defer d2.Close()
	cams2, _ := camera.NewSQLCameraStore(d2.SQL()).List()
	if len(cams2) != 2 {
		t.Fatalf("재이관으로 중복됨: %d대", len(cams2))
	}
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s 가 남아 있음 (err=%v)", path, err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s 없음: %v", path, err)
	}
}

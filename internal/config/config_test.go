// config 패키지(로드/저장/검증/암호화/핫리로드)의 단위 테스트
package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestDefault는 기본값이 문서 §8 스키마와 일치하는지 확인한다.
func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Version != 1 {
		t.Errorf("Version = %d, want 1", cfg.Version)
	}
	if cfg.Server.WSPort != 8080 || cfg.Server.HTTPPort != 8081 {
		t.Errorf("Server = %+v, want ws 8080 / http 8081", cfg.Server)
	}
	if cfg.Stream.DefaultTransport != "tcp" || cfg.Stream.JitterBufferMS != 150 {
		t.Errorf("Stream = %+v", cfg.Stream)
	}
	if cfg.Stream.MaxConcurrentStreams != 20 {
		t.Errorf("MaxConcurrentStreams = %d, want 20", cfg.Stream.MaxConcurrentStreams)
	}
	if cfg.Discovery.ScanTimeoutMS != 3000 || cfg.Discovery.AutoScanIntervalMin != 30 {
		t.Errorf("Discovery = %+v", cfg.Discovery)
	}
	if !cfg.Decoder.PreferHardware || cfg.Decoder.MaxThreads != 4 {
		t.Errorf("Decoder = %+v", cfg.Decoder)
	}
	if cfg.Logging.Level != "info" || cfg.Logging.MaxSizeMB != 100 {
		t.Errorf("Logging = %+v", cfg.Logging)
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("기본값 검증 실패: %v", err)
	}
}

// TestLoadCreatesDefaultWhenMissing은 파일이 없으면 기본 설정을 생성하는지 확인한다.
func TestLoadCreatesDefaultWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() err = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("기본 설정 파일이 생성되지 않음: %v", err)
	}
	if cfg.Version != CurrentVersion {
		t.Errorf("Version = %d, want %d", cfg.Version, CurrentVersion)
	}
}

// TestSaveLoadRoundtrip은 저장 후 로드 시 동일한 값이 유지되는지 확인한다.
func TestSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")
	cfg := Default()
	cfg.Server.WSPort = 9090
	cfg.Stream.JitterBufferMS = 200
	cfg.Discovery.ScanInterfaces = []string{"en5"}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() err = %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() err = %v", err)
	}
	if got.Server.WSPort != 9090 || got.Stream.JitterBufferMS != 200 {
		t.Errorf("roundtrip 불일치: got %+v", got.Server)
	}
	if len(got.Discovery.ScanInterfaces) != 1 || got.Discovery.ScanInterfaces[0] != "en5" {
		t.Errorf("ScanInterfaces = %v, want [en5]", got.Discovery.ScanInterfaces)
	}
}

// TestLoadFillsDefaultsForMissingFields는 부분 파일 로드 시 기본값이 채워지는지 확인한다.
func TestLoadFillsDefaultsForMissingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"server":{"ws_port":7000}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() err = %v", err)
	}
	if cfg.Server.WSPort != 7000 {
		t.Errorf("WSPort = %d, want 7000", cfg.Server.WSPort)
	}
	if cfg.Server.HTTPPort != 8081 {
		t.Errorf("HTTPPort = %d, want 기본값 8081", cfg.Server.HTTPPort)
	}
	if cfg.Stream.DefaultTransport != "tcp" {
		t.Errorf("DefaultTransport = %q, want 기본값 tcp", cfg.Stream.DefaultTransport)
	}
}

// TestLoadRejectsFutureVersion은 지원하지 않는 미래 버전을 거부하는지 확인한다.
func TestLoadRejectsFutureVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")
	if err := os.WriteFile(path, []byte(`{"version":999}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("미래 버전 로드가 에러 없이 성공함")
	}
}

// TestValidate는 유효성 검증의 주요 분기를 확인한다.
func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*AppConfig)
		wantErr bool
	}{
		{"기본값은 통과", func(*AppConfig) {}, false},
		{"포트 0은 실패", func(c *AppConfig) { c.Server.WSPort = 0 }, true},
		{"포트 중복은 실패", func(c *AppConfig) { c.Server.HTTPPort = c.Server.WSPort }, true},
		{"잘못된 transport", func(c *AppConfig) { c.Stream.DefaultTransport = "quic" }, true},
		{"max_streams 0", func(c *AppConfig) { c.Stream.MaxConcurrentStreams = 0 }, true},
		{"max_streams 101", func(c *AppConfig) { c.Stream.MaxConcurrentStreams = 101 }, true},
		{"빈 인터페이스", func(c *AppConfig) { c.Discovery.ScanInterfaces = []string{" "} }, true},
		{"잘못된 로그 레벨", func(c *AppConfig) { c.Logging.Level = "verbose" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mutate(cfg)
			err := Validate(cfg)
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestEncryptDecryptRoundtrip은 암호화/복호화 왕복을 확인한다.
func TestEncryptDecryptRoundtrip(t *testing.T) {
	t.Setenv(EnvMasterKey, "6f1c0f5a5f3d4a8f9a2b7c8d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f")
	ct, err := EncryptSecret("cam-001", "s3cret!pass")
	if err != nil {
		t.Fatalf("EncryptSecret() err = %v", err)
	}
	if len(ct) < len(EncryptedPrefix) || ct[:len(EncryptedPrefix)] != EncryptedPrefix {
		t.Fatalf("암호문 접두어 없음: %q", ct[:min(20, len(ct))])
	}
	pt, err := DecryptSecret("cam-001", ct)
	if err != nil {
		t.Fatalf("DecryptSecret() err = %v", err)
	}
	if pt != "s3cret!pass" {
		t.Errorf("평문 불일치: %q", pt)
	}
}

// TestEncryptKeyIsolation은 카메라별 키가 서로 다름을 확인한다.
func TestEncryptKeyIsolation(t *testing.T) {
	ct1, err := EncryptSecret("cam-001", "same-password")
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := EncryptSecret("cam-002", "same-password")
	if err != nil {
		t.Fatal(err)
	}
	if ct1 == ct2 {
		t.Error("다른 카메라 ID로 동일한 암호문 생성됨")
	}
	// cam-001 키로 암호화한 값을 cam-002 키로 복호화하면 실패해야 한다.
	if _, err := DecryptSecret("cam-002", ct1); err == nil {
		t.Error("다른 카메라 키로 복호화가 성공함")
	}
}

// TestDecryptPlaintextPassthrough는 접두어 없는 값(구버전 평문) 호환을 확인한다.
func TestDecryptPlaintextPassthrough(t *testing.T) {
	pt, err := DecryptSecret("cam-001", "plain-password")
	if err != nil {
		t.Fatalf("DecryptSecret() err = %v", err)
	}
	if pt != "plain-password" {
		t.Errorf("평문 통과 실패: %q", pt)
	}
}

// TestDecryptWithDifferentMasterKey는 마스터 키가 바뀌면 복호화가 실패함을 확인한다.
func TestDecryptWithDifferentMasterKey(t *testing.T) {
	t.Setenv(EnvMasterKey, "first-key")
	ct, err := EncryptSecret("cam-001", "data")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvMasterKey, "second-key")
	if _, err := DecryptSecret("cam-001", ct); err == nil {
		t.Error("마스터 키 변경 후 복호화가 성공함")
	}
}

// TestWatcherHotReload는 설정 파일 변경 시 콜백이 실행되는지 확인한다.
func TestWatcherHotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.json")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	called := make(chan *AppConfig, 1)
	w, err := StartWatching(path, func(c *AppConfig) {
		mu.Lock()
		defer mu.Unlock()
		select {
		case called <- c:
		default:
		}
	})
	if err != nil {
		t.Fatalf("StartWatching() err = %v", err)
	}
	defer w.Stop()

	// 감시자가 준비될 시간을 준다.
	time.Sleep(100 * time.Millisecond)

	cfg.Server.WSPort = 7777
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-called:
		if got.Server.WSPort != 7777 {
			t.Errorf("리로드된 WSPort = %d, want 7777", got.Server.WSPort)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("핫 리로드 콜백이 3초 내 호출되지 않음")
	}
}

// TestWatcherIgnoresOtherFiles는 다른 파일 변경이 콜백을 트리거하지 않는지 확인한다.
func TestWatcherIgnoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.json")
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}

	w, err := StartWatching(path, func(*AppConfig) {
		t.Error("무관한 파일 변경으로 콜백이 호출됨")
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "other.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1 * time.Second)
}

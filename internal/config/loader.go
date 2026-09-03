// app.json 파일의 로드/저장(원자적 쓰기)과 버전 검사를 담당한다.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Load는 지정 경로의 설정 파일을 읽는다. 파일이 없으면 기본값을 생성해 저장한다.
// 파일에 없는 필드는 기본값으로 채워진다.
func Load(path string) (*AppConfig, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := Default()
		if err := Save(path, cfg); err != nil {
			return nil, fmt.Errorf("기본 설정 파일 생성 실패: %w", err)
		}
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("설정 파일 읽기 실패: %w", err)
	}
	return Parse(b)
}

// Parse는 설정 JSON 바이트를 기본값 위에 언마샬하고 버전을 검사한다.
// (파일/DB 등 저장 매체에 무관하게 공유되는 파싱 경로다.)
func Parse(b []byte) (*AppConfig, error) {
	cfg := Default()
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("설정 파싱 실패: %w", err)
	}
	if err := checkVersion(cfg.Version); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Bytes는 설정을 버전 검사 후 들여쓴 JSON으로 직렬화한다.
func Bytes(cfg *AppConfig) ([]byte, error) {
	if err := checkVersion(cfg.Version); err != nil {
		return nil, err
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// Save는 설정을 원자적으로(attempt: 임시 파일 쓰기 후 rename) 파일에 기록한다.
func Save(path string, cfg *AppConfig) error {
	b, err := Bytes(cfg)
	if err != nil {
		return fmt.Errorf("설정 직렬화 실패: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("설정 디렉토리 생성 실패: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".app-*.json.tmp")
	if err != nil {
		return fmt.Errorf("임시 파일 생성 실패: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("임시 파일 쓰기 실패: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("임시 파일 닫기 실패: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("설정 파일 교체 실패: %w", err)
	}
	return nil
}

// checkVersion은 지원하지 않는 미래 버전을 거부한다. 구버전은 로드 후 현재 버전으로 갱신한다.
func checkVersion(v int) error {
	if v > CurrentVersion {
		return fmt.Errorf("지원하지 않는 설정 버전: %d (지원 버전: %d 이하)", v, CurrentVersion)
	}
	return nil
}

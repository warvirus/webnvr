// StoragePool의 대상 선택(fill-then-next)과 페일오버 동작을 검증한다.
package recording

import (
	"os"
	"path/filepath"
	"testing"

	"webnvr/internal/config"
)

func poolCfg(paths ...string) []config.StorageConfig {
	out := make([]config.StorageConfig, 0, len(paths))
	for _, p := range paths {
		out = append(out, config.StorageConfig{Path: p, MinFreePercent: 0})
	}
	return out
}

func TestPoolPickFirstAvailable(t *testing.T) {
	dir := t.TempDir()
	p, err := NewStoragePool(poolCfg(filepath.Join(dir, "a"), filepath.Join(dir, "b")))
	if err != nil {
		t.Fatal(err)
	}
	idx, abs, err := p.Pick(0)
	if err != nil {
		t.Fatal(err)
	}
	if idx != 0 || abs != filepath.Join(dir, "a") {
		t.Fatalf("첫 대상이 선택돼야 함: idx=%d abs=%s", idx, abs)
	}
}

// 대상 1이 실제로 쓸 수 없으면(경로가 파일로 막힘) 대상 2로 페일오버한다.
func TestPoolFailoverWhenOffline(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil { // 디렉토리 생성이 실패하는 파일
		t.Fatal(err)
	}
	p, err := NewStoragePool([]config.StorageConfig{
		{Path: blocked, MinFreePercent: 0},
		{Path: filepath.Join(dir, "b"), MinFreePercent: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	idx, abs, err := p.Pick(0)
	if err != nil {
		t.Fatal(err)
	}
	if idx != 1 || abs != filepath.Join(dir, "b") {
		t.Fatalf("두 번째 대상으로 페일오버해야 함: idx=%d abs=%s", idx, abs)
	}
}

// 여유 하한(min_free_percent) 미달 대상은 건너뛴다 — fill-then-next.
func TestPoolSkipsBelowFreeFloor(t *testing.T) {
	dir := t.TempDir()
	orig := freePercentFn
	freePercentFn = func(path string) (float64, bool) {
		if filepath.Base(path) == "full" {
			return 1.0, true // 하한 5 미달
		}
		return 50.0, true
	}
	t.Cleanup(func() { freePercentFn = orig })

	p, err := NewStoragePool([]config.StorageConfig{
		{Path: filepath.Join(dir, "full"), MinFreePercent: 5},
		{Path: filepath.Join(dir, "ok"), MinFreePercent: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	idx, abs, err := p.Pick(0)
	if err != nil {
		t.Fatal(err)
	}
	if idx != 1 || abs != filepath.Join(dir, "ok") {
		t.Fatalf("여유 하한 미달 대상을 건너뛰어야 함: idx=%d abs=%s", idx, abs)
	}
}

// 모든 대상이 여유 측정 불가여도 마지막 수단으로 첫 대상을 쓴다.
func TestPoolFreeUnknownFallback(t *testing.T) {
	dir := t.TempDir()
	orig := freePercentFn
	freePercentFn = func(string) (float64, bool) { return 0, false }
	t.Cleanup(func() { freePercentFn = orig })

	p, err := NewStoragePool(poolCfg(filepath.Join(dir, "a"), filepath.Join(dir, "b")))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Pick(0); err != nil {
		t.Fatalf("여유 불명 대상을 후순위로 써야 함: %v", err)
	}
}

func TestPoolAllUnavailable(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := NewStoragePool(poolCfg(blocked))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Pick(0); err == nil {
		t.Fatal("모든 대상 불가 시 오류여야 함")
	}
}

func TestPoolRootOutOfRange(t *testing.T) {
	dir := t.TempDir()
	p, err := NewStoragePool(poolCfg(filepath.Join(dir, "a")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Root(5); err == nil {
		t.Fatal("범위 밖 인덱스는 오류여야 함")
	}
}

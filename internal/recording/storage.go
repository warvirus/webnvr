// 다중 스토리지 풀 — fill-then-next 채우기와 대상 오프라인 페일오버를 담당한다.
package recording

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"webnvr/internal/config"
)

// StoragePool은 설정된 스토리지 대상 목록이다. 세그먼트를 열 때 Pick으로 대상을 고른다.
// 정책(fill-then-next): 현재 대상을 여유 하한까지 쓰고, 못 쓰면(여유 부족/오프라인) 다음 대상.
type StoragePool struct {
	mu    sync.Mutex
	roots []poolRoot
}

type poolRoot struct {
	abs     string
	minFree int // 여유 하한 퍼센트 (0 = 검사 안 함)
}

// NewStoragePool은 설정의 storages로 풀을 만든다. 대상 디렉토리 생성은 시도만 하고
// 실패해도 풀에는 남긴다(Pick에서 건너뛴다) — USB 분리 같은 오프라인 대상이 부팅 시점에 있을 수 있다.
func NewStoragePool(storages []config.StorageConfig) (*StoragePool, error) {
	if len(storages) == 0 {
		return nil, fmt.Errorf("recording.storages가 비어 있음")
	}
	p := &StoragePool{roots: make([]poolRoot, 0, len(storages))}
	for i, s := range storages {
		abs, err := filepath.Abs(s.Path)
		if err != nil {
			return nil, fmt.Errorf("스토리지 %d 경로 확정 실패: %w", i, err)
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			slog.Warn("스토리지 디렉토리 생성 실패 — Pick에서 건너뜀", "idx", i, "path", abs, "err", err)
		}
		p.roots = append(p.roots, poolRoot{abs: abs, minFree: s.MinFreePercent})
	}
	return p, nil
}

// Pick은 prefer 인덱스부터 시작해 첫 사용 가능 대상을 반환한다.
// 사용 가능 = 디렉토리 준비 성공 + (여유 퍼센트가 하한 이상 또는 측정 불가면 후순위).
// 모든 대상이 불가하면 오류를 반환한다(호출자는 녹화를 이번 AU에서 건너뛴다).
func (p *StoragePool) Pick(prefer int) (idx int, abs string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(p.roots)
	if n == 0 {
		return 0, "", fmt.Errorf("스토리지 풀이 비어 있음")
	}
	start := prefer % n
	var fallbackIdx = -1
	var fallbackAbs string
	for i := 0; i < n; i++ {
		k := (start + i) % n
		root := p.roots[k]
		if err := os.MkdirAll(root.abs, 0o755); err != nil {
			slog.Warn("스토리지 대상 접근 실패 — 다음 대상 검토", "idx", k, "path", root.abs, "err", err)
			continue
		}
		pct, ok := freePercent(root.abs)
		if !ok {
			// 여유 불명(네트워크 드라이브 등) — 다른 대상이 모두 불가할 때만 사용
			if fallbackIdx < 0 {
				fallbackIdx, fallbackAbs = k, root.abs
			}
			continue
		}
		if root.minFree > 0 && pct < float64(root.minFree) {
			continue // fill-then-next: 여유 하한 미달 → 다음 대상
		}
		return k, root.abs, nil
	}
	if fallbackIdx >= 0 {
		slog.Warn("모든 대상 여부 측정 불가 — 여유 불명 대상 사용", "idx", fallbackIdx, "path", fallbackAbs)
		return fallbackIdx, fallbackAbs, nil
	}
	return 0, "", fmt.Errorf("사용 가능한 스토리지 대상이 없음 (%d개 검토)", n)
}

// Root는 인덱스의 절대 경로를 반환한다. (janitor 삭제, 재생 서빙의 rel_path 해석용)
func (p *StoragePool) Root(idx int) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx < 0 || idx >= len(p.roots) {
		return "", fmt.Errorf("스토리지 인덱스 범위 밖: %d (0..%d)", idx, len(p.roots)-1)
	}
	return p.roots[idx].abs, nil
}

// Roots는 모든 대상의 (절대경로, 여유 퍼센트, 여유 하한)을 반환한다. (status API)
func (p *StoragePool) Roots() []RootStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]RootStatus, 0, len(p.roots))
	for _, r := range p.roots {
		pct, ok := freePercent(r.abs)
		out = append(out, RootStatus{Path: r.abs, FreePercent: pct, FreeKnown: ok, MinFreePercent: r.minFree})
	}
	return out
}

// RootStatus는 status API의 대상별 상태다.
type RootStatus struct {
	Path           string  `json:"path"`
	FreePercent    float64 `json:"freePercent"`
	FreeKnown      bool    `json:"freeKnown"`
	MinFreePercent int     `json:"minFreePercent"`
}

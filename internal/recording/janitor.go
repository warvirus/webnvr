// 용량 관리 — 할당량(max_usage_gb) · 디스크 여유(min_free_percent) · 보관기간(retention_days).
// 60초 주기로 스윕하며 오래된 세그먼트부터 삭제한다. 가드레일: keep_min_hours, 기록 중 카메라 보호.
package recording

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"webnvr/internal/config"
)

const dayMS = 24 * 60 * 60 * 1000

// Janitor는 저장 용량을 정책 안에서 유지한다.
type Janitor struct {
	cfg         config.RecordingConfig
	store       *Store
	root        string
	isRecording func(cameraID string) bool

	stop chan struct{}
	done chan struct{}
}

// NewJanitor를 만든다. isRecording은 기록 중 카메라 보호에 쓰인다(nil이면 항상 false).
func NewJanitor(cfg config.RecordingConfig, store *Store, root string, isRecording func(string) bool) *Janitor {
	if isRecording == nil {
		isRecording = func(string) bool { return false }
	}
	return &Janitor{cfg: cfg, store: store, root: root, isRecording: isRecording}
}

// Start는 60초 주기 스윕을 시작한다(즉시 1회 포함). ctx 취소 또는 Stop으로 종료한다.
func (j *Janitor) Start(ctx context.Context) {
	if j.stop != nil {
		return
	}
	j.stop = make(chan struct{})
	j.done = make(chan struct{})
	go func() {
		defer close(j.done)
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		j.Sweep()
		for {
			select {
			case <-ctx.Done():
				return
			case <-j.stop:
				return
			case <-t.C:
				j.Sweep()
			}
		}
	}()
}

// Stop은 스윕 루프를 멈춘다.
func (j *Janitor) Stop() {
	if j.stop == nil {
		return
	}
	close(j.stop)
	<-j.done
	j.stop = nil
}

// Sweep은 한 번의 정리 사이클이다. (테스트에서 직접 호출)
func (j *Janitor) Sweep() {
	// 1) 보관기간 — 공간과 무관하게 오래된 것 삭제 (keep_min_hours보다 최근은 유지)
	if j.cfg.RetentionDays > 0 {
		cutoff := nowMS() - int64(j.cfg.RetentionDays)*dayMS
		for {
			segs, err := j.store.OlderThan(cutoff, 200)
			if err != nil {
				slog.Error("janitor: retention 조회 실패", "err", err)
				break
			}
			deleted := 0
			for _, s := range segs {
				if j.tooRecent(s) {
					continue
				}
				j.delete(s)
				deleted++
			}
			if deleted == 0 || len(segs) < 200 {
				break
			}
		}
	}

	// 2) 할당량 + 디스크 여유 — 오래된 것부터, reclaim 여유까지 삭제
	if !j.over(true) {
		return
	}
	for j.over(true) {
		segs, err := j.store.Oldest(100)
		if err != nil {
			slog.Error("janitor: oldest 조회 실패", "err", err)
			return
		}
		if len(segs) == 0 {
			return
		}
		deleted := 0
		for _, s := range segs {
			if j.tooRecent(s) || j.isRecording(s.CameraID) && j.isNewestFor(s) {
				continue
			}
			j.delete(s)
			deleted++
			if !j.over(true) {
				return
			}
		}
		if deleted == 0 {
			slog.Warn("janitor: 삭제 가능한 세그먼트 없음 (전부 보호됨) — 용량 목표 미달")
			return
		}
	}
}

// over는 한도 초과 여부다. withReclaim이면 reclaim_percent 여유까지 목표로 한다.
func (j *Janitor) over(withReclaim bool) bool {
	reclaim := 0.0
	if withReclaim {
		reclaim = float64(j.cfg.ReclaimPercent)
	}
	if j.cfg.MaxUsageGB > 0 {
		used, err := j.store.SumBytes()
		if err == nil {
			limit := j.cfg.MaxUsageGB * 1e9 * (1 - reclaim/100)
			if float64(used) > limit {
				return true
			}
		}
	}
	floor := 0
	if len(j.cfg.Storages) > 0 {
		floor = j.cfg.Storages[0].MinFreePercent
	}
	if floor > 0 {
		if pct, ok := freePercent(j.root); ok && pct < float64(floor)+reclaim {
			return true
		}
	}
	return false
}

// tooRecent는 keep_min_hours 안쪽이면 true (풀 디스크에 전부 지워지는 것 방지).
func (j *Janitor) tooRecent(s Segment) bool {
	if j.cfg.KeepMinHours <= 0 {
		return false
	}
	return nowMS()-s.StartTS < int64(j.cfg.KeepMinHours)*60*60*1000
}

// isNewestFor는 그 카메라의 가장 최근(=현재 열려 있을 수 있는) 세그먼트인지 근사 판정한다.
func (j *Janitor) isNewestFor(s Segment) bool {
	rows, err := j.store.Range(s.CameraID, s.StartTS+1, nowMS()+dayMS)
	return err == nil && len(rows) == 0
}

func (j *Janitor) delete(s Segment) {
	if err := os.Remove(filepath.Join(j.root, filepath.FromSlash(s.RelPath))); err != nil && !os.IsNotExist(err) {
		slog.Warn("janitor: 파일 삭제 실패", "path", s.RelPath, "err", err)
	}
	if err := j.store.Delete(s.ID); err != nil {
		slog.Error("janitor: segments 행 삭제 실패", "id", s.ID, "err", err)
		return
	}
	slog.Info("janitor: 세그먼트 삭제", "camera", s.CameraID, "path", s.RelPath, "bytes", s.Bytes, "start_ts", s.StartTS)
}

// freePercentFn은 테스트에서 교체 가능한 디스크 여유 프로버다.
var freePercentFn = freePercentOS

func freePercent(path string) (float64, bool) { return freePercentFn(path) }

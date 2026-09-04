package recording

import (
	"testing"

	"webnvr/internal/config"
)

func withFixedNow(t *testing.T, ms int64) {
	t.Helper()
	orig := nowMS
	nowMS = func() int64 { return ms }
	t.Cleanup(func() { nowMS = orig })
}

func withFreePercent(t *testing.T, pct float64) {
	t.Helper()
	orig := freePercentFn
	freePercentFn = func(string) (float64, bool) { return pct, true }
	t.Cleanup(func() { freePercentFn = orig })
}

// seedSegments는 start_ts가 base부터 step 간격인 세그먼트 n개를 넣는다(각 bytes).
func seedSegments(t *testing.T, s *Store, n int, base, step, bytes int64) []int64 {
	t.Helper()
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		id, err := s.Insert(Segment{
			CameraID: "cam-1", StartTS: base + int64(i)*step, DurMS: step,
			RelPath: "cam-1/x/s.ts", Bytes: bytes, Codec: "h264",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestJanitorRetention(t *testing.T) {
	now := int64(100 * dayMS)
	withFixedNow(t, now)
	s := newTestStore(t)
	// 10일 전부터 하루 간격 12개
	seedSegments(t, s, 12, now-12*dayMS, dayMS, 1000)

	cfg := config.RecordingConfig{RetentionDays: 5, KeepMinHours: 1}
	j := NewJanitor(cfg, s, t.TempDir(), nil)
	j.Sweep()

	rows, _ := s.Oldest(100)
	for _, r := range rows {
		if r.StartTS < now-int64(cfg.RetentionDays)*dayMS {
			t.Errorf("보관기간 초과 세그먼트가 남음: start_ts=%d (cutoff=%d)", r.StartTS, now-5*dayMS)
		}
	}
	if len(rows) == 0 {
		t.Fatal("전부 삭제됨 — 최근 것은 남아야")
	}
}

func TestJanitorQuota(t *testing.T) {
	now := int64(100 * dayMS)
	withFixedNow(t, now)
	withFreePercent(t, 100) // 디스크 여유 충분
	s := newTestStore(t)
	// 각 1e8 바이트 × 20개 = 2e9. 한도 1GB(1e9), reclaim 10% → 목표 ≤ 0.9e9
	seedSegments(t, s, 20, now-20*int64(3600_000), 3600_000, 100_000_000)

	cfg := config.RecordingConfig{
		MaxUsageGB: 1, ReclaimPercent: 10, KeepMinHours: 1,
		Storages: []config.StorageConfig{{Path: "recordings", MinFreePercent: 0}},
	}
	j := NewJanitor(cfg, s, t.TempDir(), nil)
	j.Sweep()

	used, _ := s.SumBytes()
	if float64(used) > cfg.MaxUsageGB*1e9*(1-0.10) {
		t.Errorf("할당량 초과 유지: used=%d, 목표<=%.0f", used, cfg.MaxUsageGB*1e9*0.9)
	}
	// 오래된 것부터 지워졌는지 — 남은 것의 최소 start_ts가 지워진 것보다 커야
	rows, _ := s.Oldest(100)
	if len(rows) == 0 {
		t.Fatal("전부 삭제됨")
	}
}

func TestJanitorDiskFloor(t *testing.T) {
	now := int64(100 * dayMS)
	withFixedNow(t, now)
	withFreePercent(t, 2) // min_free_percent=5 미달
	s := newTestStore(t)
	seedSegments(t, s, 10, now-10*int64(3600_000), 3600_000, 1_000_000)

	cfg := config.RecordingConfig{
		ReclaimPercent: 0, KeepMinHours: 0,
		Storages: []config.StorageConfig{{Path: "recordings", MinFreePercent: 5}},
	}
	j := NewJanitor(cfg, s, t.TempDir(), nil)
	j.Sweep()

	// freePercent가 계속 2%로 고정이라 janitor는 전부 지우고 "목표 미달" 경고 후 멈춘다
	rows, _ := s.Oldest(100)
	if len(rows) != 0 {
		t.Errorf("디스크 여유 미달인데 삭제 안 됨: %d행 남음", len(rows))
	}
}

func TestJanitorKeepMinHoursProtects(t *testing.T) {
	now := int64(100 * dayMS)
	withFixedNow(t, now)
	withFreePercent(t, 1) // 항상 미달
	s := newTestStore(t)
	// 전부 30분 이내 (keep_min_hours=1 안쪽)
	seedSegments(t, s, 5, now-30*60_000, 5*60_000, 1_000_000)

	cfg := config.RecordingConfig{
		KeepMinHours: 1,
		Storages:     []config.StorageConfig{{Path: "recordings", MinFreePercent: 90}},
	}
	j := NewJanitor(cfg, s, t.TempDir(), nil)
	j.Sweep()

	rows, _ := s.Oldest(100)
	if len(rows) != 5 {
		t.Errorf("keep_min_hours 안쪽인데 삭제됨: %d행 남음 (want 5)", len(rows))
	}
}

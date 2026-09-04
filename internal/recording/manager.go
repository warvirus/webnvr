// config.Recording.Enabled일 때 record_mode 카메라의 녹화기 생명주기를 관리한다.
// R.1: 상시(continuous) 모드 + 단일 스토리지. record_mode 변경은 앱 재시작 시 반영된다.
package recording

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"webnvr/internal/camera"
	"webnvr/internal/config"
	"webnvr/internal/stream"
)

// HubRef는 Manager가 스트림 허브에 요구하는 연산이다. (*stream.Hub가 구현)
type HubRef interface {
	SetNALUTap(cameraID string, tap stream.NALUTap)
	Start(cameraID string) error
	Subscribe(cameraID string) (<-chan stream.Event, func(), error)
	Reload(cameraID string)
	Running() []string
}

// CameraLister는 녹화 대상 판정을 위해 카메라 목록을 제공한다. (*camera.Manager가 구현)
type CameraLister interface {
	List() ([]camera.Camera, error)
}

// Manager는 녹화기 집합을 관리한다.
type Manager struct {
	cfg         config.RecordingConfig
	storageRoot string
	store       *Store
	hub         HubRef
	cams        CameraLister
	janitor     *Janitor

	mu        sync.Mutex
	sessions  map[string]*recSession
	closed    bool
}

type recSession struct {
	rec    *Recorder
	cancel context.CancelFunc
}

// NewManager는 설정을 검증하고 스토리지 루트를 준비한다. cfg.Enabled 여부와 무관하게
// 호출 가능하나, Start가 실제 녹화기를 만든다.
func NewManager(cfg config.RecordingConfig, store *Store, hub HubRef, cams CameraLister) (*Manager, error) {
	if len(cfg.Storages) == 0 {
		return nil, fmt.Errorf("recording.storages가 비어 있음")
	}
	root, err := filepath.Abs(cfg.Storages[0].Path)
	if err != nil {
		return nil, fmt.Errorf("스토리지 경로 확정 실패: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("스토리지 디렉토리 생성 실패: %w", err)
	}
	m := &Manager{
		cfg: cfg, storageRoot: root, store: store, hub: hub, cams: cams,
		sessions: map[string]*recSession{},
	}
	m.janitor = NewJanitor(cfg, store, root, m.isRecording)
	return m, nil
}

// Start는 record_mode가 continuous/both인 카메라의 녹화를 시작하고 janitor를 돌린다.
func (m *Manager) Start(ctx context.Context) error {
	cams, err := m.cams.List()
	if err != nil {
		return fmt.Errorf("카메라 목록 조회 실패: %w", err)
	}
	n := 0
	for _, c := range cams {
		if c.RecordMode == camera.RecordContinuous || c.RecordMode == camera.RecordBoth {
			m.addRecorder(c.ID)
			n++
		}
	}
	slog.Info("녹화 시작", "cameras", n, "storage", m.storageRoot,
		"segment_seconds", m.cfg.SegmentSeconds, "max_usage_gb", m.cfg.MaxUsageGB)
	m.janitor.Start(ctx)
	return nil
}

// addRecorder는 카메라 하나의 녹화 세션을 만든다.
func (m *Manager) addRecorder(cameraID string) {
	m.mu.Lock()
	if _, ok := m.sessions[cameraID]; ok || m.closed {
		m.mu.Unlock()
		return
	}
	rec := NewRecorder(cameraID, m.storageRoot, 0, m.cfg.SegmentSeconds, m.cfg.SegmentMaxMB, m.store)
	ctx, cancel := context.WithCancel(context.Background())
	m.sessions[cameraID] = &recSession{rec: rec, cancel: cancel}
	m.mu.Unlock()

	wasRunning := false
	for _, id := range m.hub.Running() {
		if id == cameraID {
			wasRunning = true
		}
	}
	m.hub.SetNALUTap(cameraID, rec.OnNALU)
	if wasRunning {
		m.hub.Reload(cameraID) // 탭 없이 돌던 세션을 재다이얼해 NALU 탭 반영
	}
	go m.supervise(ctx, cameraID, rec)
}

// supervise는 녹화 세션을 24/7 유지한다 — 구독 채널이 닫히면 백오프 후 재구독한다.
func (m *Manager) supervise(ctx context.Context, cameraID string, rec *Recorder) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if err := m.hub.Start(cameraID); err != nil {
			slog.Warn("녹화 스트림 시작 실패", "camera", cameraID, "err", err)
		}
		ch, cancelSub, err := m.hub.Subscribe(cameraID)
		if err != nil {
			if sleepCtx(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = time.Second
		for ev := range ch {
			switch e := ev.(type) {
			case stream.StartedEvent:
				rec.OnInfo(e.Info.Codec, e.Info.SPS, e.Info.PPS, e.Info.VPS)
			case stream.StoppedEvent:
				rec.OnGap()
			}
		}
		cancelSub()
		if ctx.Err() != nil {
			return
		}
		rec.OnGap() // 채널 닫힘 = 세션 종료 → 다음 세그먼트는 불연속
		slog.Info("녹화 세션 재연결 대기", "camera", cameraID, "backoff", backoff)
		if sleepCtx(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff)
	}
}

// Close는 모든 녹화기를 정지하고 진행 중 세그먼트를 flush한다.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	sessions := m.sessions
	m.sessions = map[string]*recSession{}
	m.mu.Unlock()

	for id, s := range sessions {
		s.cancel()
		m.hub.SetNALUTap(id, nil)
		s.rec.Close()
	}
	if m.janitor != nil {
		m.janitor.Stop()
	}
	slog.Info("녹화 정지", "cameras", len(sessions))
}

// isRecording은 janitor가 "기록 중인 세그먼트 보호"에 쓴다.
func (m *Manager) isRecording(cameraID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[cameraID]
	return ok
}

func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

// sleepCtx는 d만큼 대기하되 ctx 취소 시 true를 반환한다.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-t.C:
		return false
	}
}

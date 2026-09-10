// 녹화기 생명주기를 관리한다 — 카메라 목록과 설정을 기준으로 세션을 조정(reconcile)한다.
// record_mode/enabled 변경은 UpdateRecordingConfig·카메라 mutation 콜백으로 즉시 반영된다.
package recording

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// StateNotifier는 녹화 세션 변화를 클라이언트에 알린다. (*ws.Server가 구현, nil 허용)
type StateNotifier interface {
	BroadcastRecordingState()
}

// Manager는 녹화기 집합을 관리한다.
type Manager struct {
	pool  *StoragePool
	store *Store
	hub   HubRef
	cams  CameraLister
	jan   *Janitor
	nf    StateNotifier // 세션 변화 시 브로드캐스트(nil 허용)

	cfgMu sync.RWMutex
	cfg   config.RecordingConfig

	mu       sync.Mutex
	sessions map[string]*recSession
	closed   bool
}

type recSession struct {
	rec     *Recorder
	cancel  context.CancelFunc
	tapStop func() // 비동기 탭 드레인+종료 (stopLocked에서 rec.Close 전 호출)

	subMu     sync.Mutex
	subCancel func() // supervise의 허브 구독 해제 (stop 시 RTSP 세션 해제를 위해 필요)
}

// setSubCancel은 supervise의 구독 해제 함수를 기록한다.
func (s *recSession) setSubCancel(fn func()) {
	s.subMu.Lock()
	s.subCancel = fn
	s.subMu.Unlock()
}

// releaseSub는 구독을 해제한다 (멱등).
func (s *recSession) releaseSub() {
	s.subMu.Lock()
	fn := s.subCancel
	s.subCancel = nil
	s.subMu.Unlock()
	if fn != nil {
		fn()
	}
}

// NewManager는 설정을 검증하고 스토리지 풀을 준비한다. enabled 여부와 무관하게
// 호출 가능하나(동적 토글 지원), Start가 실제 녹화기를 만든다.
func NewManager(cfg config.RecordingConfig, store *Store, hub HubRef, cams CameraLister) (*Manager, error) {
	pool, err := NewStoragePool(cfg.Storages)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		pool: pool, store: store, hub: hub, cams: cams,
		cfg:      cfg,
		sessions: map[string]*recSession{},
	}
	m.jan = NewJanitor(cfg, store, pool, m.isRecording)
	return m, nil
}

// Start는 녹화 대상 카메라의 녹화를 시작하고 janitor를 돌린다.
func (m *Manager) Start(ctx context.Context) error {
	m.sweepOrphans() // 크래시로 인덱스 행 없이 남은 파일 정리
	if err := m.reconcile(); err != nil {
		return err
	}
	cfg := m.Config()
	slog.Info("녹화 시작", "enabled", cfg.Enabled, "sessions", m.Count(),
		"storages", len(cfg.Storages), "segment_seconds", cfg.SegmentSeconds,
		"max_usage_gb", cfg.MaxUsageGB)
	m.jan.Start(ctx)
	return nil
}

// sweepOrphans는 스토리지를 스캔해 DB 행이 없는 .ts 파일(크래시 시 close 전
// INSERT 누락분)을 삭제한다. 세그먼트는 close 시점에만 INSERT되므로 강제 종료 시
// 파일만 남는데, 이대로 두면 재생 불가 + janitor가 지우지 못해 영구 누적된다.
func (m *Manager) sweepOrphans() {
	rows, err := m.store.All()
	if err != nil {
		slog.Warn("고아 스캔 생략 — segments 조회 실패", "err", err)
		return
	}
	known := map[int]map[string]bool{} // storage_idx → rel_path 집합
	for _, g := range rows {
		if known[g.StorageIdx] == nil {
			known[g.StorageIdx] = map[string]bool{}
		}
		known[g.StorageIdx][g.RelPath] = true
	}
	for i, r := range m.pool.Roots() {
		removed, failed := 0, 0
		err := filepath.WalkDir(r.Path, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // 접근 불가 디렉토리는 건너뛴다
			}
			if d.IsDir() || !strings.HasSuffix(path, ".ts") {
				return nil
			}
			rel, err := filepath.Rel(r.Path, path)
			if err != nil {
				return nil
			}
			if known[i][filepath.ToSlash(rel)] {
				return nil
			}
			if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
				failed++
				return nil
			}
			removed++
			return nil
		})
		if err != nil {
			slog.Warn("고아 스캔 실패", "root", r.Path, "err", err)
			continue
		}
		if removed > 0 || failed > 0 {
			slog.Info("고아 세그먼트 정리", "root", r.Path, "removed", removed, "failed", failed)
		}
	}
}

// SetNotifier는 세션 변화 통지자를 주입한다. (App 조립 시, nil 허용)
func (m *Manager) SetNotifier(n StateNotifier) { m.nf = n }

// notifyState는 세션 집합이 변했음을 클라이언트에 알린다.
func (m *Manager) notifyState() {
	if m.nf != nil {
		m.nf.BroadcastRecordingState()
	}
}

// reconcile은 카메라 목록+현재 설정과 세션을 대조해 녹화기를 만들거나 정지한다.
// 동적 반영의 핵심 — 카메라 mutation과 설정 변경 후에 호출된다.
func (m *Manager) reconcile() error {
	cfg := m.Config()
	if !cfg.Enabled {
		m.stopAll()
		return nil
	}
	cams, err := m.cams.List()
	if err != nil {
		return fmt.Errorf("카메라 목록 조회 실패: %w", err)
	}
	desired := map[string]camera.Camera{}
	for _, c := range cams {
		if !c.Enabled {
			continue
		}
		if c.RecordMode == camera.RecordContinuous || c.RecordMode == camera.RecordEvent || c.RecordMode == camera.RecordBoth {
			desired[c.ID] = c
		}
	}

	m.mu.Lock()
	// 제거 대상: desired에 없는 세션
	var toStop []string
	for id := range m.sessions {
		if _, ok := desired[id]; !ok {
			toStop = append(toStop, id)
		}
	}
	// 신규/재기동 대상: 세션이 없거나 모드가 바뀐 카메라
	var toStart []camera.Camera
	for id, c := range desired {
		s, exists := m.sessions[id]
		wantMode := modeOf(c)
		if !exists {
			toStart = append(toStart, c)
			continue
		}
		if s.rec.SetMode(wantMode) { // 세션 유지 — RTSP 재다이얼 없이 모드만 전환
			continue
		}
		// 같은 모드 — pre/post-roll만 변경 시 세션 교체 없이 필드만 갱신
		if s.rec.preRollB != int64(prerollOf(c))*90000 || s.rec.postRoll != int64(postrollOf(c))*1000 {
			s.rec.mu.Lock()
			s.rec.preRollB = int64(prerollOf(c)) * 90000
			s.rec.postRoll = int64(postrollOf(c)) * 1000
			s.rec.mu.Unlock()
		}
	}
	for _, id := range toStop {
		m.stopLocked(id)
	}
	m.mu.Unlock()

	for _, c := range toStart {
		m.addRecorder(c)
	}
	if len(toStop) > 0 || len(toStart) > 0 {
		m.notifyState() // 녹화 세션 집합 변화 → 클라이언트 즉시 갱신
	}
	return nil
}

func modeOf(c camera.Camera) string {
	switch c.RecordMode {
	case camera.RecordEvent, camera.RecordBoth:
		return c.RecordMode
	default:
		return camera.RecordContinuous
	}
}

func prerollOf(c camera.Camera) int {
	if c.PreRoll <= 0 {
		return 10
	}
	return c.PreRoll
}

func postrollOf(c camera.Camera) int {
	if c.PostRoll <= 0 {
		return 15
	}
	return c.PostRoll
}

// UpdateRecordingConfig는 녹화 설정 변경을 즉시 반영한다.
// storages/segment_seconds/segment_max_mb는 세그먼트 열기 정책이라 세션 교체가 필요하다
// (현재 v1: 재시작 안내 — 다음 세션부터는 세션 재기동으로 처리 가능). janitor 정책은 즉시 반영.
func (m *Manager) UpdateRecordingConfig(cfg config.RecordingConfig) {
	m.cfgMu.Lock()
	m.cfg = cfg
	m.cfgMu.Unlock()
	m.jan.SetCfg(cfg)
	if err := m.reconcile(); err != nil {
		slog.Warn("녹화 설정 반영 실패", "err", err)
	}
}

// NotifyCameras는 카메라 목록 변경(추가/수정/삭제)을 매니저에 알린다. (API 계층에서 호출)
func (m *Manager) NotifyCameras() {
	if err := m.reconcile(); err != nil {
		slog.Warn("카메라 변경 반영 실패", "err", err)
	}
}

// TriggerEvent는 카메라에 이벤트 녹화를 트리거한다(수동 트리거 API).
func (m *Manager) TriggerEvent(cameraID, typ string) error {
	m.mu.Lock()
	s, ok := m.sessions[cameraID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("녹화 중이 아닌 카메라입니다: %s", cameraID)
	}
	_, err := s.rec.TriggerEvent(typ)
	return err
}

// Status는 현재 녹화 상태를 반환한다. (status API)
func (m *Manager) Status() StatusInfo {
	cfg := m.Config()
	used, _ := m.store.SumBytes()
	m.mu.Lock()
	recording := make([]RecordingInfo, 0, len(m.sessions))
	for id, s := range m.sessions {
		recording = append(recording, RecordingInfo{CameraID: id, Mode: s.rec.Mode()})
	}
	m.mu.Unlock()
	return StatusInfo{
		Enabled:   cfg.Enabled,
		Recording: recording,
		UsedBytes: used,
		Storages:  m.pool.Roots(),
	}
}

// RecordingInfo는 녹화 중 카메라 한 대의 표시 정보다.
type RecordingInfo struct {
	CameraID string `json:"cameraId"`
	Mode     string `json:"mode"` // continuous | event | both
}

// StorageRoot는 세그먼트 행의 storage_idx를 절대 경로로 해석한다. (재생 서빙용)
func (m *Manager) StorageRoot(idx int) (string, error) {
	return m.pool.Root(idx)
}

// Store는 녹화 인덱스 DAO를 노출한다. (재생 API 조회용)
func (m *Manager) Store() *Store { return m.store }

// StatusInfo는 status API 응답이다.
type StatusInfo struct {
	Enabled   bool            `json:"enabled"`
	Recording []RecordingInfo `json:"recording"`
	UsedBytes int64           `json:"usedBytes"`
	Storages  []RootStatus    `json:"storages"`
}

// Config는 현재 녹화 설정을 반환한다.
func (m *Manager) Config() config.RecordingConfig {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return m.cfg
}

// Count는 활성 녹화 세션 수다.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// addRecorder는 카메라 하나의 녹화 세션을 만든다.
func (m *Manager) addRecorder(c camera.Camera) {
	m.mu.Lock()
	if _, ok := m.sessions[c.ID]; ok || m.closed {
		m.mu.Unlock()
		return
	}
	cfg := m.Config()
	rec := NewRecorder(RecorderConfig{
		CameraID:        c.ID,
		Pool:            m.pool,
		SegmentSeconds:  cfg.SegmentSeconds,
		SegmentMaxMB:    cfg.SegmentMaxMB,
		Mode:            modeOf(c),
		PreRollSeconds:  prerollOf(c),
		PostRollSeconds: postrollOf(c),
		Store:           m.store,
	})
	ctx, cancel := context.WithCancel(context.Background())
	sess := &recSession{rec: rec, cancel: cancel}
	m.sessions[c.ID] = sess
	m.mu.Unlock()

	wasRunning := false
	for _, id := range m.hub.Running() {
		if id == c.ID {
			wasRunning = true
		}
	}
	// 탭을 비동기로 감싼다 — 세그먼트 회전 시 Statfs/MkdirAll/DB INSERT 같은 느린 I/O가
	// RTP 수신 스레드를 블로킹해 라이브 배포가 버스트로 끊기는 것을 막는다.
	// 큐는 유계이며 포화 시 AU를 드롭한다(녹화 프레임 유실 < 라이브 지연).
	tap, tapStop := asyncTap(rec.OnNALU)
	sess.tapStop = tapStop
	m.hub.SetNALUTap(c.ID, tap)
	if wasRunning {
		m.hub.Reload(c.ID) // 탭 없이 돌던 세션을 재다이얼해 NALU 탭 반영
	}
	go m.supervise(ctx, c.ID, rec, sess)
}

// asyncTap은 NALU 탭을 유계 큐 뒤의 전용 goroutine으로 실행한다.
// stop은 큐를 모두 소비한 뒤 반환한다 — rec.Close() 전에 호출해야 세그먼트 꼬리가
// 유실되지 않는다. stop 전에 탭을 먼저 해제해야 한다(해제 후 유입 없음을 보장).
func asyncTap(onNALU stream.NALUTap) (stream.NALUTap, func()) {
	type auCall struct {
		codec stream.Codec
		nalus [][]byte
		pts   int64
		key   bool
	}
	ch := make(chan auCall, 512)
	stopCh := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case ev := <-ch:
				onNALU(ev.codec, ev.nalus, ev.pts, ev.key)
			case <-stopCh:
				for { // 드레인 — 남은 AU를 모두 기록하고 종료
					select {
					case ev := <-ch:
						onNALU(ev.codec, ev.nalus, ev.pts, ev.key)
					default:
						return
					}
				}
			}
		}
	}()
	tap := func(codec stream.Codec, nalus [][]byte, pts int64, key bool) {
		// 비동기 소비이므로 enqueue 시점에 복사해야 한다 — 발신 측(gortsplib)은
		// 콜백 반환 후 패킷 버퍼를 재활용할 수 있다.
		cp := make([][]byte, len(nalus))
		for i, n := range nalus {
			cp[i] = append([]byte(nil), n...)
		}
		select {
		case ch <- auCall{codec: codec, nalus: cp, pts: pts, key: key}:
		default:
			// 녹화 큐 포화 — 디스크/DB 병목. AU 1개 드롭은 세그먼트 1프레임 유실이다.
		}
	}
	return tap, func() {
		close(stopCh)
		<-done
	}
}

// supervise는 녹화 세션을 24/7 유지한다 — 구독 채널이 닫히면 백오프 후 재구독한다.
func (m *Manager) supervise(ctx context.Context, cameraID string, rec *Recorder, sess *recSession) {
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
		sess.setSubCancel(cancelSub)
		if ctx.Err() != nil { // 정지가 Subscribe와 경합 — 즉시 해제
			sess.releaseSub()
			return
		}
		backoff = time.Second
	drain:
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					break drain
				}
				switch e := ev.(type) {
				case stream.StartedEvent:
					rec.OnInfo(e.Info.Codec, e.Info.SPS, e.Info.PPS, e.Info.VPS)
				case stream.StoppedEvent:
					rec.OnGap()
				}
			case <-ctx.Done():
				sess.releaseSub() // 구독 해제 → 마지막 구독자면 RTSP 세션도 해제
				return
			}
		}
		sess.releaseSub()
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

// stopAll은 모든 세션을 정지한다.
func (m *Manager) stopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	for _, id := range ids {
		m.stopLocked(id)
	}
	m.mu.Unlock()
	if len(ids) > 0 {
		slog.Info("녹화 전체 정지", "cameras", len(ids))
	}
}

// stopLocked는 세션 하나를 정지한다. m.mu 보유 필요.
func (m *Manager) stopLocked(id string) {
	s, ok := m.sessions[id]
	if !ok {
		return
	}
	delete(m.sessions, id)
	s.cancel()     // supervise가 구독을 해제하고 종료한다 (백오프 대기 중이어도)
	s.releaseSub() // 채널 drain 전이라도 즉시 해제 (멱등)
	m.hub.SetNALUTap(id, nil)
	if s.tapStop != nil {
		s.tapStop() // 큐에 남은 AU를 모두 기록한 뒤 반환
	}
	s.rec.Close()
}

// Close는 모든 녹화기를 정지하고 진행 중 세그먼트를 flush한다.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	for _, id := range ids {
		m.stopLocked(id)
	}
	m.mu.Unlock()
	if m.jan != nil {
		m.jan.Stop()
	}
	slog.Info("녹화 정지", "cameras", len(ids))
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

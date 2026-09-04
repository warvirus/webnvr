package recording

import (
	"context"
	"sync"
	"testing"
	"time"

	"webnvr/internal/camera"
	"webnvr/internal/config"
	"webnvr/internal/stream"
)

// fakeHub는 HubRef의 테스트 구현이다.
type fakeHub struct {
	mu      sync.Mutex
	taps    map[string]stream.NALUTap
	chans   map[string]chan stream.Event
	running map[string]bool
	starts  int
}

func newFakeHub() *fakeHub {
	return &fakeHub{taps: map[string]stream.NALUTap{}, chans: map[string]chan stream.Event{}, running: map[string]bool{}}
}
func (h *fakeHub) SetNALUTap(id string, tap stream.NALUTap) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if tap == nil {
		delete(h.taps, id)
	} else {
		h.taps[id] = tap
	}
}
func (h *fakeHub) Start(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.starts++
	h.running[id] = true
	if _, ok := h.chans[id]; !ok {
		h.chans[id] = make(chan stream.Event, 64)
	}
	return nil
}
func (h *fakeHub) Subscribe(id string) (<-chan stream.Event, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := h.chans[id]
	return ch, func() {}, nil
}
func (h *fakeHub) Reload(id string) {}
func (h *fakeHub) Running() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for id := range h.running {
		out = append(out, id)
	}
	return out
}
func (h *fakeHub) emit(id string, ev stream.Event) {
	h.mu.Lock()
	ch := h.chans[id]
	h.mu.Unlock()
	ch <- ev
}
func (h *fakeHub) tapOf(id string) stream.NALUTap {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.taps[id]
}

type fakeCams struct{ list []camera.Camera }

func (c fakeCams) List() ([]camera.Camera, error) { return c.list, nil }

func recCfg() config.RecordingConfig {
	c := config.Default().Recording
	c.Enabled = true
	c.Storages = []config.StorageConfig{{Path: "recordings", MinFreePercent: 0}}
	c.SegmentSeconds = 300
	c.SegmentMaxMB = 512
	c.MaxUsageGB = 0
	c.RetentionDays = 0
	return c
}

func TestManagerRecordsContinuousCamera(t *testing.T) {
	_, _ = withFakeSink(t) // Recorder는 fakeSink 사용
	store := newTestStore(t)
	hub := newFakeHub()
	cams := fakeCams{list: []camera.Camera{
		{ID: "cam-1", RecordMode: camera.RecordContinuous},
		{ID: "cam-2", RecordMode: camera.RecordOff},
	}}

	cfg := recCfg()
	cfg.Storages[0].Path = t.TempDir()
	m, err := NewManager(cfg, store, hub, cams)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// cam-1만 탭이 걸려야 한다
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && hub.tapOf("cam-1") == nil {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.tapOf("cam-1") == nil {
		t.Fatal("cam-1에 NALU 탭이 안 걸림")
	}
	if hub.tapOf("cam-2") != nil {
		t.Error("off 카메라에 탭이 걸림")
	}

	// StartedEvent → OnInfo, 이어서 탭으로 IDR + 슬라이스 공급
	hub.emit("cam-1", stream.StartedEvent{Info: stream.Info{Codec: stream.CodecH264, SPS: []byte{0x67, 1}, PPS: []byte{0x68, 1}}})
	time.Sleep(50 * time.Millisecond)
	tap := hub.tapOf("cam-1")
	tap(stream.CodecH264, [][]byte{{0x65, 0}}, 90000, true)
	tap(stream.CodecH264, [][]byte{{0x61, 0}}, 93000, false)

	m.Close()

	rows, _ := store.Oldest(10)
	if len(rows) != 1 {
		t.Fatalf("segments 행 = %d, want 1", len(rows))
	}
	if rows[0].CameraID != "cam-1" || rows[0].Codec != "h264" {
		t.Errorf("행 불일치: %+v", rows[0])
	}
	if hub.tapOf("cam-1") != nil {
		t.Error("Close 후에도 탭이 남음")
	}
}

func TestManagerNoRecorderWhenDisabledList(t *testing.T) {
	_, _ = withFakeSink(t)
	store := newTestStore(t)
	hub := newFakeHub()
	cfg := recCfg()
	cfg.Storages[0].Path = t.TempDir()
	m, _ := NewManager(cfg, store, hub, fakeCams{list: []camera.Camera{{ID: "c", RecordMode: camera.RecordOff}}})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	m.Close()
	if len(hub.Running()) != 0 {
		t.Errorf("off인데 스트림 시작됨: %v", hub.Running())
	}
}

// 이벤트 모드(링버퍼 pre-roll, post-roll 종료, both 동시 기록)를 검증한다.
package recording

import (
	"strings"
	"testing"
	"time"

	"webnvr/internal/camera"
	"webnvr/internal/stream"
)

// TestRecorderEventClipPrePostRoll — 트리거 시 pre-roll이 클립에 들어가고,
// post_roll 경과 후 다음 AU에서 클립이 닫힌다. segments(kind=event) + events 행 생성.
func TestRecorderEventClipPrePostRoll(t *testing.T) {
	sinks, setNow := withFakeSink(t)
	s := newTestStore(t)
	r := NewRecorder(RecorderConfig{
		CameraID: "cam-1", Pool: testPool(t), SegmentSeconds: 300, SegmentMaxMB: 512,
		Mode: camera.RecordEvent, PreRollSeconds: 10, PostRollSeconds: 15, Store: s,
	})
	r.OnInfo(stream.CodecH264, []byte{0x67, 1}, []byte{0x68, 1}, nil)

	// pre-roll 적재 — 1초 간격 AU (키 프레임은 첫 AU만)
	n, k := au(true)
	r.OnNALU(stream.CodecH264, n, 90000, k)
	for i := 1; i <= 20; i++ {
		nn, kk := au(false)
		r.OnNALU(stream.CodecH264, nn, int64(90000+i*90000), kk)
	}

	// 트리거 — 클립 열림 (fakeSink 1개 추가)
	setNow(1_000_000 + 20_000) // 벽시계 20초 경과
	if _, err := r.TriggerEvent("manual"); err != nil {
		t.Fatalf("TriggerEvent: %v", err)
	}
	if len(*sinks) != 1 {
		t.Fatalf("트리거 후 sink 수 = %d, want 1", len(*sinks))
	}

	// post_roll(15s) 안의 AU → 클립 유지
	nn, _ := au(false)
	r.OnNALU(stream.CodecH264, nn, 90000+21*90000, false)
	if !(*sinks)[0].closed && len(*sinks) != 1 {
		t.Fatal("post-roll 안에서 클립이 닫힘")
	}

	// post_roll(15s) 경과 후 AU → 클립 닫힘
	setNow(1_000_000 + 20_000 + 16_000)
	r.OnNALU(stream.CodecH264, nn, 90000+22*90000, false)
	if !(*sinks)[0].closed {
		t.Fatal("post-roll 경과 후에도 클립이 안 닫힘")
	}
	r.Close()

	rows, _ := s.Oldest(10)
	if len(rows) != 1 {
		t.Fatalf("segments 행 = %d, want 1", len(rows))
	}
	if rows[0].Kind != "event" {
		t.Errorf("kind = %s, want event", rows[0].Kind)
	}
	// dur = lastPTS(22s) - evOpenPTS(ring 첫 AU ≈ 11~12s) — pre-roll 10s + 클립 마지막 AU
	if rows[0].DurMS < 10_000 || rows[0].DurMS > 12_000 {
		t.Errorf("dur_ms = %d, want 10000~12000 (pre-roll 포함)", rows[0].DurMS)
	}
	if rows[0].Bytes <= 0 {
		t.Error("bytes 미기록")
	}

	evs, err := s.EventsRange("cam-1", 0, 1<<62)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "manual" || evs[0].SegmentID != rows[0].ID {
		t.Errorf("events 행 불일치: %+v (segment=%d)", evs, rows[0].ID)
	}
}

// TestRecorderTriggerWithoutEventMode — continuous 모드에서 트리거는 오류.
func TestRecorderTriggerWithoutEventMode(t *testing.T) {
	_, _ = withFakeSink(t)
	s := newTestStore(t)
	r := NewRecorder(RecorderConfig{
		CameraID: "cam-1", Pool: testPool(t), SegmentSeconds: 300, SegmentMaxMB: 512,
		Mode: camera.RecordContinuous, Store: s,
	})
	r.OnInfo(stream.CodecH264, nil, nil, nil)
	if _, err := r.TriggerEvent(""); err == nil {
		t.Fatal("continuous 모드에서 트리거가 성공함")
	}
}

// TestRecorderEventTypeSanitized — 이벤트 유형은 화이트리스트만 허용한다 (경로 조작 차단).
func TestRecorderEventTypeSanitized(t *testing.T) {
	_, setNow := withFakeSink(t)
	s := newTestStore(t)
	r := NewRecorder(RecorderConfig{
		CameraID: "cam-1", Pool: testPool(t), SegmentSeconds: 300, SegmentMaxMB: 512,
		Mode: camera.RecordEvent, Store: s,
	})
	r.OnInfo(stream.CodecH264, nil, nil, nil)
	n, _ := au(true)
	r.OnNALU(stream.CodecH264, n, 90000, true)
	for _, typ := range []string{"../../etc/pwned", "a/b", "..", "이벤트!", ""} {
		if _, err := r.TriggerEvent(typ); err != nil && typ == "" {
			t.Fatalf("빈 유형은 manual 기본값이어야 함: %v", err)
		} else if (err == nil) != (typ == "") {
			t.Errorf("typ = %q: err = %v, 빈 값만 허용되어야 함", typ, err)
		}
	}
	setNow(2_000)
	// 안전한 유형과 빈 유형은 성공
	if _, err := r.TriggerEvent("motion_ok-1"); err != nil {
		t.Fatalf("화이트리스트 유형 거부됨: %v", err)
	}
	r.Close()
	rows, _ := s.Oldest(10)
	if len(rows) == 0 || rows[0].Kind != "event" {
		t.Fatalf("event 행 없음: %+v", rows)
	}
	if !strings.HasSuffix(rows[0].RelPath, "manual.ts") {
		t.Errorf("빈 유형 클립 = %s, want ...manual.ts", rows[0].RelPath)
	}
}

// TestRecorderSetMode — 세션 유지 상태에서 모드가 동적으로 전환된다.
func TestRecorderSetMode(t *testing.T) {
	sinks, setNow := withFakeSink(t)
	s := newTestStore(t)
	r := NewRecorder(RecorderConfig{
		CameraID: "cam-1", Pool: testPool(t), SegmentSeconds: 300, SegmentMaxMB: 512,
		Mode: camera.RecordContinuous, PreRollSeconds: 5, PostRollSeconds: 5, Store: s,
	})
	r.OnInfo(stream.CodecH264, []byte{0x67, 1}, []byte{0x68, 1}, nil)
	n, _ := au(true)
	r.OnNALU(stream.CodecH264, n, 90000, true)
	if len(*sinks) != 1 {
		t.Fatalf("상시 세그먼트 미시작: %d", len(*sinks))
	}

	// continuous → event: 상시 세그먼트 닫힘, 트리거 가능해짐
	if !r.SetMode(camera.RecordEvent) {
		t.Fatal("SetMode가 변경을 보고하지 않음")
	}
	if r.Mode() != camera.RecordEvent {
		t.Fatalf("mode = %s, want event", r.Mode())
	}
	if !(*sinks)[0].closed {
		t.Error("event 전환 후 상시 세그먼트가 안 닫힘")
	}
	if _, err := r.TriggerEvent("manual"); err != nil {
		t.Fatalf("event 전환 후 트리거 실패: %v", err)
	}
	if len(*sinks) != 2 {
		t.Fatalf("클립 미개시: %d", len(*sinks))
	}

	// event → continuous: 상시 녹화 재개 (다음 IDR에서), 클립 닫힘
	setNow(3_000)
	if !r.SetMode(camera.RecordContinuous) {
		t.Fatal("SetMode가 변경을 보고하지 않음")
	}
	if !(*sinks)[1].closed {
		t.Error("continuous 전환 후 클립이 안 닫힘")
	}
	nn, kk := au(true)
	r.OnNALU(stream.CodecH264, nn, 180000, kk)
	if len(*sinks) != 3 {
		t.Fatalf("continuous 재개 세그먼트 미시작: %d", len(*sinks))
	}
	r.Close()
}

// TestRecorderBothModeRecordsBoth — both 모드는 상시 세그먼트와 이벤트 클립을 동시에 만든다.
func TestRecorderBothModeRecordsBoth(t *testing.T) {
	sinks, _ := withFakeSink(t)
	s := newTestStore(t)
	r := NewRecorder(RecorderConfig{
		CameraID: "cam-1", Pool: testPool(t), SegmentSeconds: 300, SegmentMaxMB: 512,
		Mode: camera.RecordBoth, PreRollSeconds: 5, PostRollSeconds: 5, Store: s,
	})
	r.OnInfo(stream.CodecH264, []byte{0x67, 1}, []byte{0x68, 1}, nil)

	n, _ := au(true)
	r.OnNALU(stream.CodecH264, n, 90000, true) // 상시 세그먼트 시작 + 링버퍼 적재
	if len(*sinks) != 1 {
		t.Fatalf("상시 세그먼트 미시작: %d", len(*sinks))
	}
	if _, err := r.TriggerEvent(""); err != nil { // 이벤트 클립 추가
		t.Fatalf("TriggerEvent: %v", err)
	}
	if len(*sinks) != 2 {
		t.Fatalf("트리거 후 sink 수 = %d, want 2", len(*sinks))
	}
	r.Close()

	rows, _ := s.Oldest(10)
	kinds := map[string]int{}
	for _, g := range rows {
		kinds[g.Kind]++
	}
	if kinds["continuous"] != 1 || kinds["event"] != 1 {
		t.Errorf("kinds = %v, want continuous=1 event=1", kinds)
	}
}

// TestRecorderEventSurvivesGap — 스트림 끊김 시 진행 중 클립은 닫힌다(불연속 데이터 방지).
func TestRecorderEventSurvivesGap(t *testing.T) {
	sinks, _ := withFakeSink(t)
	s := newTestStore(t)
	r := NewRecorder(RecorderConfig{
		CameraID: "cam-1", Pool: testPool(t), SegmentSeconds: 300, SegmentMaxMB: 512,
		Mode: camera.RecordEvent, Store: s,
	})
	r.OnInfo(stream.CodecH264, nil, nil, nil)
	n, _ := au(true)
	r.OnNALU(stream.CodecH264, n, 90000, true)
	if _, err := r.TriggerEvent(""); err != nil {
		t.Fatal(err)
	}
	if len(*sinks) != 1 {
		t.Fatal("클립 미개시")
	}
	r.OnGap()
	if !(*sinks)[0].closed {
		t.Fatal("Gap 후 클립이 안 닫힘")
	}
	r.Close()
}

// 트리거 시각이 벽시계 기준으로 events 행에 기록되는지 (nowMS mock 기반).
func TestRecorderEventTimestampsFromWall(t *testing.T) {
	_, setNow := withFakeSink(t)
	s := newTestStore(t)
	r := NewRecorder(RecorderConfig{
		CameraID: "cam-1", Pool: testPool(t), SegmentSeconds: 300, SegmentMaxMB: 512,
		Mode: camera.RecordEvent, Store: s,
	})
	r.OnInfo(stream.CodecH264, nil, nil, nil)
	n, _ := au(true)
	setNow(5_000) // 벽시계 5초
	r.OnNALU(stream.CodecH264, n, 90000, true)
	if _, err := r.TriggerEvent(""); err != nil {
		t.Fatal(err)
	}
	r.Close()
	evs, _ := s.EventsRange("cam-1", 0, 1<<62)
	if len(evs) != 1 || evs[0].TS < 4000 || evs[0].TS > 6000 {
		t.Errorf("events.TS = %v, want ≈5000", evs)
	}
	_ = time.Now
}

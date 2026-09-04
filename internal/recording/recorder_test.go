package recording

import (
	"testing"

	"webnvr/internal/stream"
)

// fakeSink는 tsSink 대신 주입해 회전 로직만 검증한다.
type fakeSink struct {
	perWrite int64
	bytes    int64
	writes   int
	closed   bool
}

func (f *fakeSink) write(_ [][]byte, _ int64, _ bool) error { f.writes++; f.bytes += f.perWrite; return nil }
func (f *fakeSink) bytesWritten() int64                     { return f.bytes }
func (f *fakeSink) close() (int64, error)                   { f.closed = true; return f.bytes, nil }

// withFakeSink는 newSink/nowMS를 테스트용으로 바꾸고 복원 함수를 반환한다.
func withFakeSink(t *testing.T) (sinks *[]*fakeSink, setNow func(int64)) {
	t.Helper()
	origSink, origNow := newSink, nowMS
	created := &[]*fakeSink{}
	newSink = func(_ string, _ stream.Codec) (segmentSink, error) {
		fs := &fakeSink{perWrite: 10}
		*created = append(*created, fs)
		return fs, nil
	}
	var cur int64 = 1_000_000
	nowMS = func() int64 { return cur }
	t.Cleanup(func() { newSink, nowMS = origSink, origNow })
	return created, func(ms int64) { cur = ms }
}

func au(key bool) ([][]byte, bool) {
	if key {
		// SPS, PPS, IDR slice (타입 5)
		return [][]byte{{0x67, 0x00}, {0x68, 0x00}, {0x65, 0x00}}, true
	}
	return [][]byte{{0x61, 0x00}}, false // non-IDR slice (타입 1)
}

func TestRecorderRotatesOnTimeAndSize(t *testing.T) {
	sinks, setNow := withFakeSink(t)
	s := newTestStore(t)
	// segment_seconds=10, segment_max_mb=1 (=1MiB)
	r := NewRecorder("cam-1", t.TempDir(), 0, 10, 1, s)
	r.OnInfo(stream.CodecH264, []byte{0x67, 1}, []byte{0x68, 1}, nil)

	// 첫 non-key → 무시 (세그먼트 없음)
	n, k := au(false)
	r.OnNALU(stream.CodecH264, n, 90000, k)
	if len(*sinks) != 0 {
		t.Fatal("IDR 전에 세그먼트가 열림")
	}

	// 첫 IDR → 세그먼트 1 시작
	n, k = au(true)
	r.OnNALU(stream.CodecH264, n, 90000, k)
	if len(*sinks) != 1 {
		t.Fatalf("IDR 후 세그먼트 수 = %d, want 1", len(*sinks))
	}

	// 시간 미경과 + 크기 미달인 IDR → 회전 안 함
	r.OnNALU(stream.CodecH264, n, 180000, true)
	if len(*sinks) != 1 {
		t.Fatalf("조기 회전 발생: %d", len(*sinks))
	}

	// 10초 경과 → 다음 IDR에서 회전
	setNow(1_000_000 + 10_001)
	r.OnNALU(stream.CodecH264, n, 270000, true)
	if len(*sinks) != 2 {
		t.Fatalf("시간 경과 회전 실패: %d", len(*sinks))
	}
	if !(*sinks)[0].closed {
		t.Error("이전 세그먼트가 안 닫힘")
	}

	// 크기 초과 유도 → 다음 IDR에서 회전
	(*sinks)[1].bytes = 2 * mib
	r.OnNALU(stream.CodecH264, n, 360000, true)
	if len(*sinks) != 3 {
		t.Fatalf("크기 초과 회전 실패: %d", len(*sinks))
	}

	r.Close()

	// segments 행 3개 (닫힌 것만) — 마지막은 Close에서
	rows, _ := s.Oldest(10)
	if len(rows) != 3 {
		t.Fatalf("segments 행 = %d, want 3", len(rows))
	}
	for _, g := range rows {
		if g.CameraID != "cam-1" || g.Codec != "h264" || g.Kind != "continuous" {
			t.Errorf("행 필드 불일치: %+v", g)
		}
		if g.Bytes <= 0 {
			t.Errorf("bytes 미기록: %+v", g)
		}
	}
	// seg1: openPTS=90000, lastPTS=180000 → dur_ms = 90000*1000/90000 = 1000
	if rows[0].DurMS != 1000 {
		t.Errorf("seg1 dur_ms = %d, want 1000", rows[0].DurMS)
	}
}

func TestRecorderGapMarksDiscontinuity(t *testing.T) {
	_, _ = withFakeSink(t)
	s := newTestStore(t)
	r := NewRecorder("cam-1", t.TempDir(), 0, 300, 512, s)
	r.OnInfo(stream.CodecH264, nil, nil, nil)

	n, _ := au(true)
	r.OnNALU(stream.CodecH264, n, 90000, true)
	r.OnGap() // 세그먼트 1 닫힘, 다음은 불연속
	r.OnNALU(stream.CodecH264, n, 900000, true)
	r.Close()

	rows, _ := s.Oldest(10)
	if len(rows) != 2 {
		t.Fatalf("행 = %d, want 2", len(rows))
	}
	if rows[0].Flags&FlagDiscontinuity != 0 {
		t.Error("첫 세그먼트에 불연속 플래그가 붙음")
	}
	if rows[1].Flags&FlagDiscontinuity == 0 {
		t.Error("gap 후 세그먼트에 불연속 플래그 없음")
	}
}

func TestRecorderRejectsUnsupportedCodec(t *testing.T) {
	sinks, _ := withFakeSink(t)
	s := newTestStore(t)
	r := NewRecorder("cam-1", t.TempDir(), 0, 300, 512, s)
	r.OnInfo(stream.Codec("mjpeg"), nil, nil, nil)
	n, _ := au(true)
	r.OnNALU(stream.Codec("mjpeg"), n, 90000, true)
	if len(*sinks) != 0 {
		t.Fatal("미지원 코덱인데 세그먼트가 생성됨")
	}
}

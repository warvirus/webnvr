package recording

import (
	"testing"

	"webnvr/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return NewStore(d.SQL())
}

func TestStoreInsertRangeSumDelete(t *testing.T) {
	s := newTestStore(t)

	ids := make([]int64, 0, 4)
	for i, seg := range []Segment{
		{CameraID: "cam-1", StartTS: 1000, DurMS: 500, RelPath: "cam-1/a.ts", Bytes: 100, Codec: "h264"},
		{CameraID: "cam-1", StartTS: 2000, DurMS: 500, RelPath: "cam-1/b.ts", Bytes: 200, Codec: "h264"},
		{CameraID: "cam-1", StartTS: 9000, DurMS: 500, RelPath: "cam-1/c.ts", Bytes: 400, Codec: "h264"},
		{CameraID: "cam-2", StartTS: 1500, DurMS: 500, RelPath: "cam-2/a.ts", Bytes: 800, Codec: "h265"},
	} {
		id, err := s.Insert(seg)
		if err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
		ids = append(ids, id)
	}

	// SumBytes = 전 카메라 합
	if n, _ := s.SumBytes(); n != 1500 {
		t.Errorf("SumBytes = %d, want 1500", n)
	}

	// Range: cam-1 [1200, 2600] → b만 (a는 1000+500=1500 < 1200? 아니 1500>=1200이라 겹침)
	got, err := s.Range("cam-1", 1200, 2600)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].RelPath != "cam-1/a.ts" || got[1].RelPath != "cam-1/b.ts" {
		t.Errorf("Range = %+v, want a.ts,b.ts", got)
	}

	// Oldest 2 (전 카메라): 1000(cam-1/a), 1500(cam-2/a)
	old, _ := s.Oldest(2)
	if len(old) != 2 || old[0].StartTS != 1000 || old[1].StartTS != 1500 {
		t.Errorf("Oldest = %+v", old)
	}

	// OlderThan 2000 → 1000, 1500
	ot, _ := s.OlderThan(2000, 10)
	if len(ot) != 2 {
		t.Errorf("OlderThan(2000) len = %d, want 2", len(ot))
	}

	// Delete
	if err := s.Delete(ids[0]); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.SumBytes(); n != 1400 {
		t.Errorf("SumBytes after delete = %d, want 1400", n)
	}
	if _, err := s.Get(ids[0]); err == nil {
		t.Error("삭제된 세그먼트 Get이 성공함")
	}
}

func TestStoreInsertDefaultsKind(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Insert(Segment{CameraID: "c", StartTS: 1, RelPath: "x.ts"})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := s.Get(id)
	if g.Kind != "continuous" {
		t.Errorf("Kind = %q, want continuous", g.Kind)
	}
}

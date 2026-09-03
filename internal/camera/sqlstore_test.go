// SQLCameraStore가 Store 계약을 JSONCameraStore와 동일하게 만족하는지 검증한다.
package camera

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newSQLTestStore(t *testing.T) *SQLCameraStore {
	t.Helper()
	dir := t.TempDir()
	d, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "t.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := d.Exec(SchemaSQL); err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return NewSQLCameraStore(d)
}

func sampleCam(name string) Camera {
	return Camera{
		Name:         name,
		Type:         TypeRTSP,
		StreamURL:    "rtsp://x/" + name,
		StreamConfig: StreamConfig{Transport: "tcp", Protocol: "rtsp", BufferSize: 1024},
		Enabled:      true,
	}
}

func TestSQLStore_AddGetList(t *testing.T) {
	s := newSQLTestStore(t)

	a, err := s.Add(sampleCam("a"))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || !strings.HasPrefix(a.ID, "cam-") {
		t.Fatalf("ID 미생성: %q", a.ID)
	}
	if a.AddedAt.IsZero() || a.UpdatedAt.IsZero() {
		t.Fatal("타임스탬프 미설정")
	}
	if a.LayoutOrder != 0 {
		t.Fatalf("첫 카메라 layout_order=0 기대, got %d", a.LayoutOrder)
	}
	b, _ := s.Add(sampleCam("b"))
	if b.LayoutOrder != 1 {
		t.Fatalf("둘째 카메라 layout_order=1 기대, got %d", b.LayoutOrder)
	}

	got, err := s.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "a" || got.StreamConfig.BufferSize != 1024 || !got.Enabled {
		t.Fatalf("Get 값 불일치: %+v", got)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Fatalf("List 순서 불일치: %+v", list)
	}
}

func TestSQLStore_GetNotFound(t *testing.T) {
	s := newSQLTestStore(t)
	_, err := s.Get("cam-nope")
	if err == nil || !strings.Contains(err.Error(), "카메라를 찾을 수 없음") {
		t.Fatalf("not-found 메시지 기대, got %v", err)
	}
}

func TestSQLStore_DupID(t *testing.T) {
	s := newSQLTestStore(t)
	c := sampleCam("a")
	c.ID = "cam-fixed"
	if _, err := s.Add(c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(c); err == nil || !strings.Contains(err.Error(), "중복") {
		t.Fatalf("중복 ID 거부 기대, got %v", err)
	}
}

func TestSQLStore_UpdatePreservesAddedAt(t *testing.T) {
	s := newSQLTestStore(t)
	a, _ := s.Add(sampleCam("a"))
	orig := a.AddedAt
	time.Sleep(2 * time.Millisecond)

	a.Name = "renamed"
	a.PTZSupported = true
	upd, err := s.Update(*a)
	if err != nil {
		t.Fatal(err)
	}
	if !upd.AddedAt.Equal(orig) {
		t.Fatalf("AddedAt 변경됨: %v → %v", orig, upd.AddedAt)
	}
	if !upd.UpdatedAt.After(orig) {
		t.Fatal("UpdatedAt 미갱신")
	}
	got, _ := s.Get(a.ID)
	if got.Name != "renamed" || !got.PTZSupported {
		t.Fatalf("Update 반영 안 됨: %+v", got)
	}

	if _, err := s.Update(sampleCam("ghost")); err == nil {
		t.Fatal("없는 카메라 Update는 오류여야 함")
	}
}

func TestSQLStore_DeleteRenumbers(t *testing.T) {
	s := newSQLTestStore(t)
	a, _ := s.Add(sampleCam("a"))
	b, _ := s.Add(sampleCam("b"))
	c, _ := s.Add(sampleCam("c"))

	if err := s.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List()
	if len(list) != 2 {
		t.Fatalf("삭제 후 2대 기대, got %d", len(list))
	}
	if list[0].ID != a.ID || list[0].LayoutOrder != 0 ||
		list[1].ID != c.ID || list[1].LayoutOrder != 1 {
		t.Fatalf("layout_order 재정렬 실패: %+v", list)
	}
	if err := s.Delete("cam-nope"); err == nil {
		t.Fatal("없는 카메라 Delete는 오류여야 함")
	}
}

func TestSQLStore_Reorder(t *testing.T) {
	s := newSQLTestStore(t)
	a, _ := s.Add(sampleCam("a"))
	b, _ := s.Add(sampleCam("b"))
	c, _ := s.Add(sampleCam("c"))

	if err := s.Reorder([]string{a.ID, b.ID}); err == nil {
		t.Fatal("개수 불일치 Reorder는 오류여야 함")
	}
	if err := s.Reorder([]string{a.ID, b.ID, "cam-x"}); err == nil {
		t.Fatal("없는 ID Reorder는 오류여야 함")
	}
	if err := s.Reorder([]string{c.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List()
	if list[0].ID != c.ID || list[1].ID != a.ID || list[2].ID != b.ID {
		t.Fatalf("Reorder 반영 안 됨: %+v", list)
	}
}

func TestSQLStore_ImportFrom(t *testing.T) {
	s := newSQLTestStore(t)
	added := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	f := CamerasFile{
		Version: 1,
		Cameras: []Camera{
			{ID: "cam-1", Name: "one", Type: TypeONVIF, XAddr: "1.2.3.4:80",
				Password: "encrypted:zzz", LayoutOrder: 0, Enabled: true,
				AddedAt: added, UpdatedAt: added},
			{ID: "cam-2", Name: "two", Type: TypeRTSP, StreamURL: "rtsp://y",
				LayoutOrder: 1, Enabled: false, AddedAt: added, UpdatedAt: added},
		},
		Groups: []Group{{ID: "g1", Name: "정문", LayoutType: "grid", LayoutCols: 2, LayoutRows: 2}},
	}
	if err := s.ImportFrom(f); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List()
	if len(list) != 2 || list[0].ID != "cam-1" || list[1].ID != "cam-2" {
		t.Fatalf("이관 목록 불일치: %+v", list)
	}
	if !list[0].AddedAt.Equal(added) {
		t.Fatalf("이관 시 AddedAt 보존 실패: %v", list[0].AddedAt)
	}
	if list[0].Password != "encrypted:zzz" {
		t.Fatalf("암호화 비밀번호 보존 실패: %q", list[0].Password)
	}
	if list[1].Enabled {
		t.Fatal("enabled=false 보존 실패")
	}
	if list[1].StreamConfig.Transport != "tcp" || list[1].StreamConfig.BufferSize != 1048576 {
		t.Fatalf("스트림 기본값 채우기 실패: %+v", list[1].StreamConfig)
	}
	g := s.ListGroups()
	if len(g) != 1 || g[0].Name != "정문" {
		t.Fatalf("그룹 이관 실패: %+v", g)
	}
}

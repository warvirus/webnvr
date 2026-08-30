// 카메라 저장소와 매니저의 단위 테스트
package camera

import (
	"path/filepath"
	"testing"

	"webnvr/internal/config"
)

// newTestStore는 임시 파일 기반 저장소를 만든다.
func newTestStore(t *testing.T) *JSONCameraStore {
	t.Helper()
	s, err := NewJSONCameraStore(filepath.Join(t.TempDir(), "cameras.json"))
	if err != nil {
		t.Fatalf("NewJSONCameraStore() err = %v", err)
	}
	return s
}

// TestStoreAddGetDelete는 저장소 CRUD 기본 흐름을 확인한다.
func TestStoreAddGetDelete(t *testing.T) {
	s := newTestStore(t)

	added, err := s.Add(Camera{Name: "정문", Type: TypeRTSP, StreamURL: "rtsp://x/stream1", Enabled: true})
	if err != nil {
		t.Fatalf("Add() err = %v", err)
	}
	if added.ID == "" {
		t.Fatal("ID가 자동 생성되지 않음")
	}

	got, err := s.Get(added.ID)
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if got.Name != "정문" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.LayoutOrder != 0 {
		t.Errorf("LayoutOrder = %d, want 0", got.LayoutOrder)
	}

	// 수정
	got.Name = "정문 카메라"
	if _, err := s.Update(*got); err != nil {
		t.Fatalf("Update() err = %v", err)
	}

	// 삭제
	if err := s.Delete(added.ID); err != nil {
		t.Fatalf("Delete() err = %v", err)
	}
	if _, err := s.Get(added.ID); err == nil {
		t.Error("삭제된 카메라 조회가 성공함")
	}
}

// TestStoreDuplicateID는 중복 ID 추가를 거부하는지 확인한다.
func TestStoreDuplicateID(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add(Camera{ID: "cam-dup", Name: "a", Type: TypeRTSP}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(Camera{ID: "cam-dup", Name: "b", Type: TypeRTSP}); err == nil {
		t.Error("중복 ID 추가가 성공함")
	}
}

// TestStorePersistRoundtrip은 재오픈 시 데이터가 유지되는지 확인한다.
func TestStorePersistRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cameras.json")
	s1, err := NewJSONCameraStore(path)
	if err != nil {
		t.Fatal(err)
	}
	added, err := s1.Add(Camera{Name: "주차장", Type: TypeRTMP, StreamURL: "rtmp://x/live"})
	if err != nil {
		t.Fatal(err)
	}

	s2, err := NewJSONCameraStore(path)
	if err != nil {
		t.Fatalf("재오픈 err = %v", err)
	}
	got, err := s2.Get(added.ID)
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if got.Name != "주차장" || got.Type != TypeRTMP {
		t.Errorf("불일치: %+v", got)
	}
}

// TestStoreReorder는 순서 변경이 반영되는지 확인한다.
func TestStoreReorder(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Add(Camera{Name: "a", Type: TypeRTSP})
	b, _ := s.Add(Camera{Name: "b", Type: TypeRTSP})
	c, _ := s.Add(Camera{Name: "c", Type: TypeRTSP})

	if err := s.Reorder([]string{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("Reorder() err = %v", err)
	}
	list, _ := s.List()
	if list[0].ID != c.ID || list[1].ID != a.ID || list[2].ID != b.ID {
		t.Errorf("순서 불일치: %s,%s,%s", list[0].ID, list[1].ID, list[2].ID)
	}
}

// TestCreateONVIF는 ONVIF 카메라 생성 시 비밀번호가 암호화됨을 확인한다.
func TestCreateONVIF(t *testing.T) {
	m := NewManager(newTestStore(t))
	cam, err := m.Create(CreateRequest{
		Name:         "정문",
		Type:         TypeONVIF,
		XAddr:        "192.168.0.217:8090",
		Username:     "admin",
		Password:     "p@ssword",
		PTZSupported: true,
	})
	if err != nil {
		t.Fatalf("Create() err = %v", err)
	}
	if cam.Password == "" || cam.Password == "p@ssword" {
		t.Fatalf("비밀번호가 암호화되지 않음: %q", cam.Password)
	}
	// 암호화된 비밀번호가 실제로 복호화 가능한지 확인한다.
	pt, err := config.DecryptSecret(cam.ID, cam.Password)
	if err != nil {
		t.Fatalf("DecryptSecret() err = %v", err)
	}
	if pt != "p@ssword" {
		t.Errorf("복호화된 평문 불일치: %q", pt)
	}
	// 스트림 기본값 확인
	if cam.StreamConfig.Protocol != "rtsp" || cam.StreamConfig.Transport != "tcp" {
		t.Errorf("StreamConfig 기본값 오류: %+v", cam.StreamConfig)
	}
}

// TestCreateValidation은 타입별 생성 검증을 확인한다.
func TestCreateValidation(t *testing.T) {
	m := NewManager(newTestStore(t))

	cases := []struct {
		name string
		req  CreateRequest
	}{
		{"빈 이름", CreateRequest{Type: TypeRTSP, StreamURL: "rtsp://x/s"}},
		{"onvif xaddr 누락", CreateRequest{Name: "a", Type: TypeONVIF, Username: "u", Password: "p"}},
		{"onvif 비밀번호 누락", CreateRequest{Name: "a", Type: TypeONVIF, XAddr: "1.2.3.4:80", Username: "u"}},
		{"rtsp 스킴 불일치", CreateRequest{Name: "a", Type: TypeRTSP, StreamURL: "http://x/s"}},
		{"rtp 스킴 불일치", CreateRequest{Name: "a", Type: TypeRTP, StreamURL: "rtsp://x/s"}},
		{"rtmp URL 누락", CreateRequest{Name: "a", Type: TypeRTMP}},
		{"알 수 없는 타입", CreateRequest{Name: "a", Type: "weird"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := m.Create(tc.req); err == nil {
				t.Errorf("검증 오류 없이 생성 성공: %+v", tc.req)
			}
		})
	}
}

// TestUpdatePassword는 수정 시 비밀번호가 재암호화됨을 확인한다.
func TestUpdatePassword(t *testing.T) {
	m := NewManager(newTestStore(t))
	cam, err := m.Create(CreateRequest{
		Name: "후문", Type: TypeONVIF, XAddr: "192.168.0.217:8100",
		Username: "admin", Password: "old-pass",
	})
	if err != nil {
		t.Fatal(err)
	}

	newName := "후문 주차장 I"
	newPass := "new-pass"
	updated, err := m.Update(cam.ID, UpdateRequest{Name: &newName, Password: &newPass})
	if err != nil {
		t.Fatalf("Update() err = %v", err)
	}
	if updated.Name != newName {
		t.Errorf("Name = %q, want %q", updated.Name, newName)
	}
	pt, err := config.DecryptSecret(cam.ID, updated.Password)
	if err != nil {
		t.Fatal(err)
	}
	if pt != newPass {
		t.Errorf("재암호화된 비밀번호 불일치: %q", pt)
	}
}

// TestUpdateInvalidValue는 수정 시에도 검증이 적용됨을 확인한다.
func TestUpdateInvalidValue(t *testing.T) {
	m := NewManager(newTestStore(t))
	cam, err := m.Create(CreateRequest{Name: "a", Type: TypeRTSP, StreamURL: "rtsp://x/s"})
	if err != nil {
		t.Fatal(err)
	}
	bad := "http://x/s"
	if _, err := m.Update(cam.ID, UpdateRequest{StreamURL: &bad}); err == nil {
		t.Error("잘못된 스킴 수정이 성공함")
	}
}

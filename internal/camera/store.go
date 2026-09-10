// cameras.json 파일 기반의 스레드 세이프 카메라 저장소다.
package camera

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// CurrentVersion은 cameras.json 스키마 버전이다.
const CurrentVersion = 1

// ErrNotFound는 카메라 ID 조회 실패의 센티널 오류다. 호출자는 errors.Is로 판별하고
// 표시 메시지는 이를 감싸 만든다(메시지 문자열 매칭은 하지 않는다).
var ErrNotFound = errors.New("카메라를 찾을 수 없음")

// NotFound는 ErrNotFound를 ID 정보와 함께 감싸 반환한다.
func NotFound(id string) error { return fmt.Errorf("%w: %s", ErrNotFound, id) }

// Store는 카메라 영속화를 위한 인터페이스다. (Phase 6에서 SQLCameraStore로 교체 예정)
type Store interface {
	List() ([]Camera, error)
	Get(id string) (*Camera, error)
	Add(cam Camera) (*Camera, error)
	Update(cam Camera) (*Camera, error)
	Delete(id string) error
	Reorder(ids []string) error
}

// JSONCameraStore는 Store를 JSON 파일로 구현한다.
type JSONCameraStore struct {
	mu   sync.RWMutex
	path string
	file CamerasFile
}

// NewJSONCameraStore는 지정 경로의 저장소를 생성하고 파일을 로드한다.
// 파일이 없으면 기본 구조로 새로 생성한다.
func NewJSONCameraStore(path string) (*JSONCameraStore, error) {
	s := &JSONCameraStore{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load는 파일을 읽어 메모리 상태를 채운다. 파일이 없으면 기본 구조를 저장한다.
func (s *JSONCameraStore) load() error {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.file = CamerasFile{Version: CurrentVersion, Cameras: []Camera{}, Groups: []Group{}}
		return s.persist()
	}
	if err != nil {
		return fmt.Errorf("카메라 파일 읽기 실패: %w", err)
	}
	var f CamerasFile
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("카메라 파일 파싱 실패: %w", err)
	}
	if f.Version > CurrentVersion {
		return fmt.Errorf("지원하지 않는 카메라 파일 버전: %d", f.Version)
	}
	if f.Cameras == nil {
		f.Cameras = []Camera{}
	}
	if f.Groups == nil {
		f.Groups = []Group{}
	}
	s.file = f
	return nil
}

// persist는 메모리 상태를 원자적으로 파일에 기록한다. 호출자가 락을 보유해야 한다.
func (s *JSONCameraStore) persist() error {
	b, err := json.MarshalIndent(s.file, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cameras-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// List는 모든 카메라를 layout_order 순으로 반환한다.
func (s *JSONCameraStore) List() ([]Camera, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Camera, len(s.file.Cameras))
	copy(out, s.file.Cameras)
	sort.SliceStable(out, func(i, j int) bool { return out[i].LayoutOrder < out[j].LayoutOrder })
	return out, nil
}

// Get은 ID로 카메라를 조회한다. 없으면 오류를 반환한다.
func (s *JSONCameraStore) Get(id string) (*Camera, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.file.Cameras {
		if s.file.Cameras[i].ID == id {
			c := s.file.Cameras[i]
			return &c, nil
		}
	}
	return nil, NotFound(id)
}

// Add는 새 카메라를 저장하고 ID와 타임스탬프가 채워진 결과를 반환한다.
func (s *JSONCameraStore) Add(cam Camera) (*Camera, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cam.ID == "" {
		id, err := NewID()
		if err != nil {
			return nil, err
		}
		cam.ID = id
	}
	for i := range s.file.Cameras {
		if s.file.Cameras[i].ID == cam.ID {
			return nil, fmt.Errorf("중복된 카메라 ID: %s", cam.ID)
		}
	}
	now := time.Now().UTC()
	cam.AddedAt = now
	cam.UpdatedAt = now
	if cam.LayoutOrder == 0 {
		cam.LayoutOrder = len(s.file.Cameras)
	}
	s.file.Cameras = append(s.file.Cameras, cam)
	if err := s.persist(); err != nil {
		return nil, err
	}
	c := cam
	return &c, nil
}

// Update는 기존 카메라를 교체하고 UpdatedAt을 갱신한다.
func (s *JSONCameraStore) Update(cam Camera) (*Camera, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.file.Cameras {
		if s.file.Cameras[i].ID == cam.ID {
			cam.AddedAt = s.file.Cameras[i].AddedAt
			cam.UpdatedAt = time.Now().UTC()
			s.file.Cameras[i] = cam
			if err := s.persist(); err != nil {
				return nil, err
			}
			c := cam
			return &c, nil
		}
	}
	return nil, NotFound(cam.ID)
}

// Delete는 카메라를 제거하고 남은 카메라의 layout_order를 재정렬한다.
func (s *JSONCameraStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.file.Cameras {
		if s.file.Cameras[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return NotFound(id)
	}
	s.file.Cameras = append(s.file.Cameras[:idx], s.file.Cameras[idx+1:]...)
	for i := range s.file.Cameras {
		s.file.Cameras[i].LayoutOrder = i
	}
	return s.persist()
}

// Reorder는 주어진 ID 순서대로 layout_order를 다시 배정한다.
func (s *JSONCameraStore) Reorder(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ids) != len(s.file.Cameras) {
		return fmt.Errorf("순서 목록(%d)이 카메라 수(%d)와 일치하지 않음", len(ids), len(s.file.Cameras))
	}
	byID := make(map[string]*Camera, len(s.file.Cameras))
	for i := range s.file.Cameras {
		byID[s.file.Cameras[i].ID] = &s.file.Cameras[i]
	}
	for order, id := range ids {
		c, ok := byID[id]
		if !ok {
			return fmt.Errorf("존재하지 않는 카메라 ID: %s", id)
		}
		c.LayoutOrder = order
		c.UpdatedAt = time.Now().UTC()
	}
	return s.persist()
}

// ListGroups는 모든 그룹을 반환한다.
func (s *JSONCameraStore) ListGroups() []Group {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Group, len(s.file.Groups))
	copy(out, s.file.Groups)
	return out
}

// newID는 "cam-" 접두어와 8자리 16진수 난수로 ID를 생성한다.
func NewID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("ID 생성 실패: %w", err)
	}
	return "cam-" + hex.EncodeToString(b), nil
}

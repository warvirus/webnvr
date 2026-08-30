// 카메라 생성/수정/삭제 비즈니스 로직과 타입별(onvif/rtsp/rtp/rtmp) 검증을 담당한다.
package camera

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"webnvr/internal/config"
)

// CreateRequest는 카메라 생성 요청이다. Password는 평문으로 전달되면 매니저가 암호화한다.
type CreateRequest struct {
	Name         string
	Type         CameraType
	XAddr        string
	Username     string
	Password     string
	ProfileToken string
	StreamURL    string
	StreamConfig StreamConfig
	PTZSupported bool
	GroupID      string
}

// UpdateRequest는 카메라 수정 요청이다. 필드가 nil이면 변경하지 않는다.
type UpdateRequest struct {
	Name         *string
	XAddr        *string
	Username     *string
	Password     *string // 평문 → 매니저가 재암호화
	ProfileToken *string
	StreamURL    *string
	StreamConfig *StreamConfig
	PTZSupported *bool
	GroupID      *string
	Enabled      *bool
}

// Manager는 카메라 도메인의 상위 연산을 제공한다.
type Manager struct {
	store Store
}

// NewManager는 저장소를 감싼 매니저를 생성한다.
func NewManager(store Store) *Manager {
	return &Manager{store: store}
}

// List는 카메라 목록을 반환한다.
func (m *Manager) List() ([]Camera, error) {
	return m.store.List()
}

// Get은 ID로 카메라를 반환한다.
func (m *Manager) Get(id string) (*Camera, error) {
	return m.store.Get(id)
}

// Create는 요청을 타입별로 검증하고 비밀번호를 암호화해 저장한다.
func (m *Manager) Create(req CreateRequest) (*Camera, error) {
	if err := m.validateCreate(req); err != nil {
		return nil, err
	}
	fillStreamDefaults(&req.StreamConfig, req.Type)

	id, err := NewID()
	if err != nil {
		return nil, err
	}
	cam := Camera{
		ID:           id,
		Name:         strings.TrimSpace(req.Name),
		Type:         req.Type,
		XAddr:        strings.TrimSpace(req.XAddr),
		Username:     req.Username,
		ProfileToken: req.ProfileToken,
		StreamURL:    strings.TrimSpace(req.StreamURL),
		StreamConfig: req.StreamConfig,
		PTZSupported: req.PTZSupported,
		GroupID:      req.GroupID,
		Enabled:      true,
	}
	if req.Password != "" {
		enc, err := m.encryptPassword(id, req.Password)
		if err != nil {
			return nil, err
		}
		cam.Password = enc
	}

	return m.store.Add(cam)
}

// encryptPassword는 카메라 ID 기반 파생 키로 평문을 암호화한다.
func (m *Manager) encryptPassword(cameraID, plaintext string) (string, error) {
	enc, err := config.EncryptSecret(cameraID, plaintext)
	if err != nil {
		return "", fmt.Errorf("비밀번호 암호화 실패: %w", err)
	}
	return enc, nil
}

// Update는 요청된 필드만 변경하고 비밀번호가 주어지면 재암호화한다.
func (m *Manager) Update(id string, req UpdateRequest) (*Camera, error) {
	cam, err := m.store.Get(id)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		cam.Name = strings.TrimSpace(*req.Name)
	}
	if req.XAddr != nil {
		cam.XAddr = strings.TrimSpace(*req.XAddr)
	}
	if req.Username != nil {
		cam.Username = *req.Username
	}
	if req.ProfileToken != nil {
		cam.ProfileToken = *req.ProfileToken
	}
	if req.StreamURL != nil {
		cam.StreamURL = strings.TrimSpace(*req.StreamURL)
	}
	if req.StreamConfig != nil {
		cam.StreamConfig = *req.StreamConfig
		fillStreamDefaults(&cam.StreamConfig, cam.Type)
	}
	if req.PTZSupported != nil {
		cam.PTZSupported = *req.PTZSupported
	}
	if req.GroupID != nil {
		cam.GroupID = *req.GroupID
	}
	if req.Enabled != nil {
		cam.Enabled = *req.Enabled
	}
	if err := m.validateCamera(*cam); err != nil {
		return nil, err
	}
	if req.Password != nil && *req.Password != "" {
		enc, err := m.encryptPassword(id, *req.Password)
		if err != nil {
			return nil, err
		}
		cam.Password = enc
	}
	return m.store.Update(*cam)
}

// Delete는 카메라를 삭제한다.
func (m *Manager) Delete(id string) error {
	return m.store.Delete(id)
}

// Reorder는 카메라 표시 순서를 변경한다.
func (m *Manager) Reorder(ids []string) error {
	return m.store.Reorder(ids)
}

// PasswordOf는 저장된 암호문을 평문으로 복호화한다. 스트림 연결 시 사용된다.
func (m *Manager) PasswordOf(cam *Camera) (string, error) {
	if cam.Password == "" {
		return "", nil
	}
	return config.DecryptSecret(cam.ID, cam.Password)
}

// validateCreate는 생성 요청을 검증한다.
func (m *Manager) validateCreate(req CreateRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("카메라 이름은 필수임")
	}
	switch req.Type {
	case TypeONVIF:
		if strings.TrimSpace(req.XAddr) == "" {
			return fmt.Errorf("onvif 타입은 xaddr이 필수임")
		}
		if req.Username == "" {
			return fmt.Errorf("onvif 타입은 username이 필수임")
		}
		if req.Password == "" {
			return fmt.Errorf("onvif 타입은 password가 필수임")
		}
	case TypeRTSP, TypeRTP, TypeRTMP:
		if err := validateDirectStreamURL(req.Type, req.StreamURL); err != nil {
			return err
		}
	default:
		return fmt.Errorf("지원하지 않는 카메라 타입: %q", req.Type)
	}
	return nil
}

// validateCamera는 저장된 카메라 전체를 검증한다(수정 경로에서 사용).
func (m *Manager) validateCamera(cam Camera) error {
	if strings.TrimSpace(cam.Name) == "" {
		return fmt.Errorf("카메라 이름은 필수임")
	}
	switch cam.Type {
	case TypeONVIF:
		if strings.TrimSpace(cam.XAddr) == "" {
			return fmt.Errorf("onvif 타입은 xaddr이 필수임")
		}
	case TypeRTSP, TypeRTP, TypeRTMP:
		return validateDirectStreamURL(cam.Type, cam.StreamURL)
	default:
		return fmt.Errorf("지원하지 않는 카메라 타입: %q", cam.Type)
	}
	return nil
}

// validateDirectStreamURL은 직접 입력 스트림 URL이 타입과 일치하는지 검사한다.
func validateDirectStreamURL(t CameraType, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s 타입은 stream_url이 필수임", t)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("stream_url 파싱 실패: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	var want string
	switch t {
	case TypeRTSP:
		want = "rtsp"
	case TypeRTP:
		want = "rtp"
	case TypeRTMP:
		want = "rtmp"
	}
	if scheme != want {
		return fmt.Errorf("%s 타입의 stream_url은 %s:// 스킴이어야 함: %q", t, want, raw)
	}
	return nil
}

// fillStreamDefaults는 비어 있는 스트림 옵션에 기본값을 채운다.
func fillStreamDefaults(sc *StreamConfig, t CameraType) {
	if sc.Transport == "" {
		sc.Transport = "tcp"
	}
	if sc.BufferSize <= 0 {
		sc.BufferSize = 1024 * 1024
	}
	if sc.Protocol == "" {
		switch t {
		case TypeRTSP:
			sc.Protocol = "rtsp"
		case TypeRTP:
			sc.Protocol = "rtp"
		case TypeRTMP:
			sc.Protocol = "rtmp"
		case TypeONVIF:
			sc.Protocol = "rtsp"
		default:
			slog.Warn("알 수 없는 카메라 타입", "type", t)
			sc.Protocol = "rtsp"
		}
	}
}

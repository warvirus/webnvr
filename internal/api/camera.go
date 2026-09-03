// 카메라 관리 서비스 계층이다. HTTP/WS 핸들러가 재사용한다 (v1.1).
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"webnvr/internal/camera"
	"webnvr/internal/config"
	"webnvr/internal/db"
	"webnvr/internal/onvif"
)

// CameraDTO는 프론트엔드로 전달되는 카메라 정보다. 비밀번호는 노출하지 않는다.
type CameraDTO struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Type         camera.CameraType   `json:"type"`
	XAddr        string              `json:"xaddr"`
	Username     string              `json:"username"`
	HasPassword  bool                `json:"hasPassword"`
	ProfileToken string              `json:"profileToken"`
	StreamURL    string              `json:"streamUrl"`
	StreamConfig camera.StreamConfig `json:"streamConfig"`
	PTZSupported bool                `json:"ptzSupported"`
	GroupID      string              `json:"groupId"`
	LayoutOrder  int                 `json:"layoutOrder"`
	Enabled      bool                `json:"enabled"`
	AddedAt      string              `json:"addedAt"`
	UpdatedAt    string              `json:"updatedAt"`
}

// CreateCameraRequest는 카메라 생성 요청이다.
type CreateCameraRequest struct {
	Name         string               `json:"name"`
	Type         camera.CameraType    `json:"type"`
	XAddr        string               `json:"xaddr"`
	Username     string               `json:"username"`
	Password     string               `json:"password"`
	ProfileToken string               `json:"profileToken"`
	StreamURL    string               `json:"streamUrl"`
	StreamConfig *camera.StreamConfig `json:"streamConfig"`
	PTZSupported bool                 `json:"ptzSupported"`
	GroupID      string               `json:"groupId"`
}

// UpdateCameraRequest는 카메라 수정 요청이다. nil 필드는 변경하지 않는다.
type UpdateCameraRequest struct {
	Name         *string              `json:"name"`
	XAddr        *string              `json:"xaddr"`
	Username     *string              `json:"username"`
	Password     *string              `json:"password"`
	ProfileToken *string              `json:"profileToken"`
	StreamURL    *string              `json:"streamUrl"`
	StreamConfig *camera.StreamConfig `json:"streamConfig"`
	PTZSupported *bool                `json:"ptzSupported"`
	GroupID      *string              `json:"groupId"`
	Enabled      *bool                `json:"enabled"`
}

// DiscoveredCamera는 WS-Discovery로 발견된 카메라 후보다.
type DiscoveredCamera struct {
	XAddr  string `json:"xaddr"`
	Scopes string `json:"scopes"`
}

// TestONVIFRequest는 ONVIF 연결 테스트 요청이다.
type TestONVIFRequest struct {
	XAddr    string `json:"xaddr"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// TestONVIFResponse는 ONVIF 연결 테스트 결과다.
type TestONVIFResponse struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Firmware     string `json:"firmware"`
}

// GetProfilesRequest는 프로필 조회 요청이다.
type GetProfilesRequest struct {
	XAddr    string `json:"xaddr"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ProfileDTO는 프론트엔드로 전달되는 미디어 프로필이다.
type ProfileDTO struct {
	Token  string `json:"token"`
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// GetStreamURIRequest는 스트림 URI 조회 요청이다.
type GetStreamURIRequest struct {
	XAddr        string `json:"xaddr"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	ProfileToken string `json:"profileToken"`
	Protocol     string `json:"protocol"`
}

// TestDirectStreamRequest는 직접 스트림(RTSP/RTP/RTMP) URL 검증 요청이다.
type TestDirectStreamRequest struct {
	URL       string `json:"url"`
	TimeoutMS int    `json:"timeoutMs"`
}

// TestDirectStreamResponse는 직접 스트림 검증 결과다.
type TestDirectStreamResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// PresetDTO는 카메라에 저장된 PTZ 프리셋이다.
type PresetDTO struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

// CameraService는 카메라 CRUD와 ONVIF 연산을 담당하는 서비스 계층이다.
type CameraService struct {
	mgr       *camera.Manager
	appCfg    *config.AppConfig
	configDir string
	database  *db.DB
}

// SetConfigDir는 설정 디렉토리를 지정한다. (App 조립 시 호출)
func (s *CameraService) SetConfigDir(dir string) { s.configDir = dir }

// ListCameras는 모든 카메라를 반환한다.
func (s *CameraService) ListCameras() ([]CameraDTO, error) {
	cams, err := s.mgr.List()
	if err != nil {
		return nil, err
	}
	out := make([]CameraDTO, 0, len(cams))
	for i := range cams {
		out = append(out, toDTO(&cams[i]))
	}
	return out, nil
}

// GetCamera는 ID로 카메라를 반환한다.
func (s *CameraService) GetCamera(id string) (*CameraDTO, error) {
	cam, err := s.mgr.Get(id)
	if err != nil {
		return nil, err
	}
	dto := toDTO(cam)
	return &dto, nil
}

// CreateCamera는 새 카메라를 추가한다.
func (s *CameraService) CreateCamera(req CreateCameraRequest) (*CameraDTO, error) {
	cr := camera.CreateRequest{
		Name:         req.Name,
		Type:         req.Type,
		XAddr:        req.XAddr,
		Username:     req.Username,
		Password:     req.Password,
		ProfileToken: req.ProfileToken,
		StreamURL:    req.StreamURL,
		PTZSupported: req.PTZSupported,
		GroupID:      req.GroupID,
	}
	if req.StreamConfig != nil {
		cr.StreamConfig = *req.StreamConfig
	}
	saved, err := s.mgr.Create(cr)
	if err != nil {
		return nil, err
	}
	dto := toDTO(saved)
	return &dto, nil
}

// UpdateCamera는 카메라 정보를 수정한다.
func (s *CameraService) UpdateCamera(id string, req UpdateCameraRequest) (*CameraDTO, error) {
	ur := camera.UpdateRequest{
		Name:         req.Name,
		XAddr:        req.XAddr,
		Username:     req.Username,
		Password:     req.Password,
		ProfileToken: req.ProfileToken,
		StreamURL:    req.StreamURL,
		StreamConfig: req.StreamConfig,
		PTZSupported: req.PTZSupported,
		GroupID:      req.GroupID,
		Enabled:      req.Enabled,
	}
	saved, err := s.mgr.Update(id, ur)
	if err != nil {
		return nil, err
	}
	dto := toDTO(saved)
	return &dto, nil
}

// DeleteCamera는 카메라를 삭제한다.
func (s *CameraService) DeleteCamera(id string) error {
	return s.mgr.Delete(id)
}

// ReorderCameras는 카메라 표시 순서를 변경한다.
func (s *CameraService) ReorderCameras(cameraIDs []string) error {
	return s.mgr.Reorder(cameraIDs)
}

// DiscoverONVIFCameras는 로컬 네트워크의 ONVIF 카메라를 검색한다.
func (s *CameraService) DiscoverONVIFCameras() ([]DiscoveredCamera, error) {
	found, err := onvif.Discover(s.appCfg.Discovery.ScanInterfaces)
	if err != nil {
		return nil, err
	}
	out := make([]DiscoveredCamera, 0, len(found))
	for _, d := range found {
		out = append(out, DiscoveredCamera{XAddr: d.XAddr, Scopes: d.Scopes})
	}
	return out, nil
}

// TestONVIFCamera는 ONVIF 연결 가능 여부를 확인한다.
func (s *CameraService) TestONVIFCamera(req TestONVIFRequest) (*TestONVIFResponse, error) {
	cli, err := onvif.New(req.XAddr, req.Username, req.Password)
	if err != nil {
		return &TestONVIFResponse{OK: false, Error: err.Error()}, nil
	}
	info, err := cli.DeviceInformation(context.Background())
	if err != nil {
		return &TestONVIFResponse{OK: false, Error: err.Error()}, nil
	}
	return &TestONVIFResponse{
		OK:           true,
		Manufacturer: info.Manufacturer,
		Model:        info.Model,
		Firmware:     info.Firmware,
	}, nil
}

// GetONVIFProfiles는 카메라의 미디어 프로필 목록을 조회한다.
func (s *CameraService) GetONVIFProfiles(req GetProfilesRequest) ([]ProfileDTO, error) {
	cli, err := onvif.New(req.XAddr, req.Username, req.Password)
	if err != nil {
		return nil, err
	}
	profiles, err := cli.Profiles(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]ProfileDTO, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, ProfileDTO{Token: p.Token, Name: p.Name, Width: p.Width, Height: p.Height})
	}
	return out, nil
}

// GetONVIFStreamURI는 프로필의 RTSP 스트림 URI를 조회한다.
func (s *CameraService) GetONVIFStreamURI(req GetStreamURIRequest) (string, error) {
	cli, err := onvif.New(req.XAddr, req.Username, req.Password)
	if err != nil {
		return "", err
	}
	return cli.StreamURI(context.Background(), req.ProfileToken, req.Protocol)
}

// GetCameraPresets는 저장된 자격증명으로 카메라의 PTZ 프리셋 목록을 조회한다.
// 자격증명이 프론트엔드로 노출되지 않도록 카메라 ID만 받는다.
func (s *CameraService) GetCameraPresets(cameraID string) ([]PresetDTO, error) {
	return onvifCall(s.mgr, cameraID, func(cli *onvif.Client, profile string) ([]PresetDTO, error) {
		presets, err := cli.Presets(context.Background(), profile)
		if err != nil {
			return nil, err
		}
		out := make([]PresetDTO, 0, len(presets))
		for _, p := range presets {
			out = append(out, PresetDTO{Token: p.Token, Name: p.Name})
		}
		return out, nil
	})
}

// GetCameraProfiles는 저장된 자격증명으로 카메라의 미디어 프로필을 조회한다.
func (s *CameraService) GetCameraProfiles(cameraID string) ([]ProfileDTO, error) {
	return onvifCall(s.mgr, cameraID, func(cli *onvif.Client, profile string) ([]ProfileDTO, error) {
		profiles, err := cli.Profiles(context.Background())
		if err != nil {
			return nil, err
		}
		out := make([]ProfileDTO, 0, len(profiles))
		for _, p := range profiles {
			out = append(out, ProfileDTO{Token: p.Token, Name: p.Name, Width: p.Width, Height: p.Height})
		}
		return out, nil
	})
}

// GetCameraStreamURI는 저장된 자격증명으로 카메라의 RTSP URI를 조회한다.
func (s *CameraService) GetCameraStreamURI(cameraID string) (string, error) {
	return onvifCall(s.mgr, cameraID, func(cli *onvif.Client, profile string) (string, error) {
		return cli.StreamURI(context.Background(), profile, "RTSP")
	})
}

// onvifCall은 카메라 저장 자격증명(복호화)으로 ONVIF 클라이언트를 준비하고
// 프로필 토큰을 확정한 뒤 콜백을 실행한다. 등록된 카메라에 대한 모든 ONVIF 조회의 공용 경로다.
func onvifCall[T any](mgr *camera.Manager, cameraID string, fn func(cli *onvif.Client, profile string) (T, error)) (T, error) {
	var zero T
	cam, err := mgr.Get(cameraID)
	if err != nil {
		return zero, err
	}
	pass, err := mgr.PasswordOf(cam)
	if err != nil {
		return zero, fmt.Errorf("비밀번호 복호화 실패: %w", err)
	}
	cli, err := onvif.New(cam.XAddr, cam.Username, pass)
	if err != nil {
		return zero, err
	}
	profile := cam.ProfileToken
	if profile == "" {
		profiles, err := cli.Profiles(context.Background())
		if err != nil {
			return zero, fmt.Errorf("프로필 조회 실패: %w", err)
		}
		if len(profiles) == 0 {
			return zero, fmt.Errorf("사용 가능한 프로필이 없음")
		}
		profile = profiles[0].Token
	}
	return fn(cli, profile)
}

// BackupFile은 내보내기/가져오기용 백업 파일 구조다. 비밀번호는 포함하지 않는다.
type BackupFile struct {
	Version    int         `json:"version"`
	ExportedAt string      `json:"exportedAt"`
	Cameras    []CameraDTO `json:"cameras"`
	AppConfig  any         `json:"appConfig,omitempty"`
}

// SecurityStatus는 보안 관련 상태다.
type SecurityStatus struct {
	MasterKeySource string `json:"masterKeySource"` // env | file | fallback
}

// AppConfig는 현재 앱 설정을 반환한다.
func (s *CameraService) AppConfig() *config.AppConfig {
	return s.appCfg
}

// UpdateAppConfig는 앱 설정을 검증 후 저장하고 메모리 값을 갱신한다.
// raw는 설정 JSON 전체(병합 아님 — 전체 교체)이다.
func (s *CameraService) UpdateAppConfig(raw map[string]any) (*config.AppConfig, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("설정 직렬화 실패: %w", err)
	}
	cfg := config.Default()
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("설정 파싱 실패: %w", err)
	}
	if err := config.Validate(cfg); err != nil {
		return nil, err
	}
	if err := s.database.SaveConfig(cfg); err != nil {
		return nil, err
	}
	s.appCfg = cfg
	return cfg, nil
}

// ExportBackup은 카메라 목록(비밀번호 제외)과 앱 설정을 백업 구조로 반환한다.
func (s *CameraService) ExportBackup() (BackupFile, error) {
	cams, err := s.ListCameras()
	if err != nil {
		return BackupFile{}, err
	}
	return BackupFile{
		Version:    1,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Cameras:    cams,
		AppConfig:  s.appCfg,
	}, nil
}

// RestoreBackup은 백업으로 카메라 목록을 대체한다. 비밀번호는 백업에 없으므로 재입력이 필요하다.
func (s *CameraService) RestoreBackup(backup BackupFile) (int, error) {
	if backup.Version != 1 {
		return 0, fmt.Errorf("지원하지 않는 백업 버전: %d", backup.Version)
	}
	// 기존 카메라 전체 삭제
	existing, err := s.ListCameras()
	if err != nil {
		return 0, err
	}
	for _, c := range existing {
		if err := s.mgr.Delete(c.ID); err != nil {
			return 0, err
		}
	}
	// 백업 복원 (비밀번호 제외)
	added := 0
	for _, dto := range backup.Cameras {
		req := camera.CreateRequest{
			Name:         dto.Name,
			Type:         dto.Type,
			XAddr:        dto.XAddr,
			Username:     dto.Username,
			Password:     "", // 백업에 비밀번호 없음
			ProfileToken: dto.ProfileToken,
			StreamURL:    dto.StreamURL,
			StreamConfig: dto.StreamConfig,
			PTZSupported: dto.PTZSupported,
			GroupID:      dto.GroupID,
		}
		saved, err := s.mgr.Create(req)
		if err != nil {
			return added, fmt.Errorf("복원 실패 (%s): %w", dto.Name, err)
		}
		// 순서/사용여부 복원 (ID는 신규 발급 — 프론트가 전체 재조회)
		enabled := dto.Enabled
		if _, err := s.mgr.Update(saved.ID, camera.UpdateRequest{Enabled: &enabled}); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// SecurityStatusOf는 현재 보안 상태를 반환한다.
func SecurityStatusOf() SecurityStatus {
	return SecurityStatus{MasterKeySource: string(config.KeySourceOf())}
}

// TestDirectStream은 직접 스트림 URL의 형식과 도달 가능성을 검증한다.
func (s *CameraService) TestDirectStream(req TestDirectStreamRequest) (*TestDirectStreamResponse, error) {
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil {
		return &TestDirectStreamResponse{OK: false, Error: "URL 파싱 실패: " + err.Error()}, nil
	}
	switch strings.ToLower(u.Scheme) {
	case "rtsp", "rtmp":
		host := u.Host
		if host == "" {
			return &TestDirectStreamResponse{OK: false, Error: "호스트가 없음"}, nil
		}
		if _, _, err := net.SplitHostPort(host); err != nil {
			// 포트가 없으면 기본 포트를 붙여 도달성을 확인한다.
			defaultPort := 554
			if strings.EqualFold(u.Scheme, "rtmp") {
				defaultPort = 1935
			}
			host = net.JoinHostPort(host, strconv.Itoa(defaultPort))
		}
		timeout := time.Duration(req.TimeoutMS) * time.Millisecond
		if timeout <= 0 {
			timeout = 3 * time.Second
		}
		conn, err := net.DialTimeout("tcp", host, timeout)
		if err != nil {
			return &TestDirectStreamResponse{OK: false, Error: "연결 실패: " + err.Error()}, nil
		}
		_ = conn.Close()
		return &TestDirectStreamResponse{OK: true}, nil

	case "rtp":
		// RTP는 UDP 기반이므로 형식 검증만 수행한다.
		if u.Host == "" {
			return &TestDirectStreamResponse{OK: false, Error: "호스트가 없음"}, nil
		}
		return &TestDirectStreamResponse{OK: true}, nil

	default:
		return &TestDirectStreamResponse{OK: false, Error: fmt.Sprintf("지원하지 않는 스킴: %q (rtsp|rtp|rtmp)", u.Scheme)}, nil
	}
}

// toDTO는 도메인 카메라를 DTO로 변환한다. 비밀번호는 포함하지 않는다.
func toDTO(c *camera.Camera) CameraDTO {
	return CameraDTO{
		ID:           c.ID,
		Name:         c.Name,
		Type:         c.Type,
		XAddr:        c.XAddr,
		Username:     c.Username,
		HasPassword:  c.Password != "",
		ProfileToken: c.ProfileToken,
		StreamURL:    c.StreamURL,
		StreamConfig: c.StreamConfig,
		PTZSupported: c.PTZSupported,
		GroupID:      c.GroupID,
		LayoutOrder:  c.LayoutOrder,
		Enabled:      c.Enabled,
		AddedAt:      c.AddedAt.Format(time.RFC3339),
		UpdatedAt:    c.UpdatedAt.Format(time.RFC3339),
	}
}

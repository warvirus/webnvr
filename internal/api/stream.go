// 프론트엔드에 노출되는 스트림 제어 Wails 바인딩 서비스다. WS 계층의 컨트롤러이기도 하다.
package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"webnvr/internal/camera"
	"webnvr/internal/onvif"
	"webnvr/internal/stream"
	"webnvr/internal/ws"
)

// StreamService는 스트림 시작/정지/PTZ를 제어한다.
type StreamService struct {
	mgr *camera.Manager
	hub *stream.Hub

	uriMu    sync.Mutex
	uriCache map[string]cachedURI // cameraID → ONVIF 스트림 URI (TTL 있음)
}

// cachedURI는 ONVIF에서 조회한 스트림 URI의 캐시 항목이다.
// 카메라 서버 재시작 시 RTSP 포트가 동적으로 바뀌므로 TTL을 둔다.
type cachedURI struct {
	uri       string
	fetchedAt time.Time
}

// uriCacheTTL은 스트림 URI 캐시 유효 시간이다.
const uriCacheTTL = 30 * time.Second

// StreamStatus는 스트림 상태 조회 결과다.
type StreamStatus struct {
	Running bool   `json:"running"`
	Codec   string `json:"codec,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
}

// NewStreamService는 카메라 매니저 기반으로 스트림 서비스를 만든다.
func NewStreamService(mgr *camera.Manager) *StreamService {
	s := &StreamService{mgr: mgr, uriCache: map[string]cachedURI{}}
	s.hub = stream.NewHub(s, nil)
	return s
}

// Hub는 내부 스트림 허브를 반환한다. (서버 조립용)
func (s *StreamService) Hub() *stream.Hub {
	return s.hub
}

// StreamURL은 stream.CameraSource 구현으로, 카메라의 RTSP URL을 제공한다.
// ONVIF 카메라는 GetStreamURI를 호출하며 결과를 캐시한다.
func (s *StreamService) StreamURL(cameraID string) (string, string, error) {
	cam, err := s.mgr.Get(cameraID)
	if err != nil {
		return "", "", err
	}
	switch cam.Type {
	case camera.TypeRTSP:
		return cam.StreamURL, cam.StreamConfig.Transport, nil
	case camera.TypeONVIF:
		if uri, ok := s.cachedURI(cameraID); ok {
			return uri, cam.StreamConfig.Transport, nil
		}
		uri, err := s.withONVIFClient(cam, func(cli *onvif.Client, profile string) (string, error) {
			return cli.StreamURI(context.Background(), profile, "RTSP")
		})
		if err != nil {
			return "", "", err
		}
		s.cacheURI(cameraID, uri)
		return uri, cam.StreamConfig.Transport, nil
	default:
		return "", "", fmt.Errorf("카메라 타입 %q의 스트림 수신은 아직 지원되지 않음", cam.Type)
	}
}

// withONVIFClient는 카메라 저장 자격증명으로 ONVIF 클라이언트를 준비해 콜백을 실행한다.
// (onvifCall 공용 헬퍼의 위임)
func (s *StreamService) withONVIFClient(cam *camera.Camera, fn func(cli *onvif.Client, profile string) (string, error)) (string, error) {
	return onvifCall(s.mgr, cam.ID, fn)
}

func (s *StreamService) cachedURI(id string) (string, bool) {
	s.uriMu.Lock()
	defer s.uriMu.Unlock()
	c, ok := s.uriCache[id]
	if !ok || time.Since(c.fetchedAt) > uriCacheTTL {
		return "", false // 만료 — 다음 시도에서 ONVIF로 재조회
	}
	return c.uri, true
}

func (s *StreamService) cacheURI(id, uri string) {
	s.uriMu.Lock()
	defer s.uriMu.Unlock()
	s.uriCache[id] = cachedURI{uri: uri, fetchedAt: time.Now()}
}

// OnStreamFailed는 스트림 실패를 통지받아 캐시된 URI를 폐기한다.
// (카메라 서버 재시작 등으로 RTSP 포트가 바뀌었을 수 있으므로 다음 시도가 재조회하게 한다)
func (s *StreamService) OnStreamFailed(cameraID string) {
	s.uriMu.Lock()
	defer s.uriMu.Unlock()
	delete(s.uriCache, cameraID)
}

// Start는 스트림을 시작한다. (ws.Controller 구현)
func (s *StreamService) Start(cameraID string) error {
	return s.hub.Start(cameraID)
}

// Stop은 스트림을 정지한다. (ws.Controller 구현)
func (s *StreamService) Stop(cameraID string) error {
	return s.hub.Stop(cameraID)
}

// StartAll은 활성화된 모든 카메라의 스트림을 시작한다. (ws.Controller 구현)
func (s *StreamService) StartAll() error {
	cams, err := s.mgr.List()
	if err != nil {
		return err
	}
	var firstErr error
	for i := range cams {
		if !cams[i].Enabled {
			continue
		}
		if err := s.hub.Start(cams[i].ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// StopAll은 모든 스트림을 정지한다. (ws.Controller 구현)
func (s *StreamService) StopAll() error {
	return s.hub.StopAll()
}

// Subscribe는 스트림 이벤트 채널을 구독한다. (ws.Controller 구현)
func (s *StreamService) Subscribe(cameraID string) (<-chan stream.Event, func(), error) {
	return s.hub.Subscribe(cameraID)
}

// PTZ는 카메라에 PTZ 명령을 전달한다. (ws.Controller 구현)
func (s *StreamService) PTZ(cameraID string, cmd ws.PTZCommand) error {
	cam, err := s.mgr.Get(cameraID)
	if err != nil {
		return err
	}
	if !cam.PTZSupported {
		return fmt.Errorf("PTZ 미지원 카메라: %s", cameraID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = s.withONVIFClient(cam, func(cli *onvif.Client, profile string) (string, error) {
		switch cmd.Action {
		case "move":
			return "", cli.ContinuousMove(ctx, profile, onvif.PTZMove{Pan: cmd.Pan, Tilt: cmd.Tilt, Zoom: cmd.Zoom})
		case "stop":
			return "", cli.Stop(ctx, profile)
		case "preset":
			if cmd.PresetToken == "" {
				return "", fmt.Errorf("presetToken이 비어 있음")
			}
			return "", cli.GotoPreset(ctx, profile, cmd.PresetToken)
		default:
			return "", fmt.Errorf("지원하지 않는 PTZ 동작: %q", cmd.Action)
		}
	})
	return err
}

// ──────────────────────────────────────────────
// Wails 바인딩 (doc §5.2)
// ──────────────────────────────────────────────

// StartStream은 카메라 스트림을 시작한다.
func (s *StreamService) StartStream(cameraID string) error {
	return s.hub.Start(cameraID)
}

// StopStream은 카메라 스트림을 정지한다.
func (s *StreamService) StopStream(cameraID string) error {
	return s.hub.Stop(cameraID)
}

// StartAllStreams는 활성화된 모든 카메라 스트림을 시작한다.
func (s *StreamService) StartAllStreams() error {
	return s.StartAll()
}

// StopAllStreams는 모든 카메라 스트림을 정지한다.
func (s *StreamService) StopAllStreams() error {
	return s.StopAll()
}

// GetStreamStatus는 스트림 상태를 반환한다.
func (s *StreamService) GetStreamStatus(cameraID string) StreamStatus {
	info, ok := s.hub.Info(cameraID)
	if !ok {
		return StreamStatus{Running: false}
	}
	return StreamStatus{
		Running: true,
		Codec:   string(info.Codec),
		Width:   info.Width,
		Height:  info.Height,
	}
}

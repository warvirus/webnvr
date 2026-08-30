// 카메라/스트림 서비스와 WebSocket 서버를 조립하는 애플리케이션 컨텍스트다.
package api

import (
	"fmt"
	"log/slog"

	"webnvr/internal/camera"
	"webnvr/internal/config"
	"webnvr/internal/ws"
)

// App는 webnvr 백엔드의 서비스 집합이다.
type App struct {
	Camera *CameraService
	Stream *StreamService

	wsServer *ws.Server
}

// New는 설정 디렉토리를 기준으로 모든 서비스를 초기화한다.
func New(configDir string) (*App, error) {
	appCfg, err := config.Load(fmt.Sprintf("%s/app.json", configDir))
	if err != nil {
		return nil, fmt.Errorf("앱 설정 로드 실패: %w", err)
	}
	if err := config.Validate(appCfg); err != nil {
		return nil, fmt.Errorf("앱 설정 검증 실패: %w", err)
	}
	store, err := camera.NewJSONCameraStore(fmt.Sprintf("%s/cameras.json", configDir))
	if err != nil {
		return nil, fmt.Errorf("카메라 저장소 초기화 실패: %w", err)
	}
	mgr := camera.NewManager(store)

	return &App{
		Camera: &CameraService{mgr: mgr, appCfg: appCfg},
		Stream: NewStreamService(mgr),
	}, nil
}

// StartWSServer는 로컬호스트에 WebSocket 서버를 시작한다.
func (a *App) StartWSServer() error {
	a.wsServer = ws.NewServer(a.Stream, fmt.Sprintf("127.0.0.1:%d", a.Camera.appCfg.Server.WSPort))
	if err := a.wsServer.Start(); err != nil {
		return err
	}
	return nil
}

// StopWSServer는 WebSocket 서버와 모든 스트림을 정지한다.
func (a *App) StopWSServer() {
	if a.wsServer != nil {
		if err := a.wsServer.Stop(); err != nil {
			slog.Warn("WS 서버 정지 실패", "err", err)
		}
	}
	_ = a.Stream.StopAll()
}

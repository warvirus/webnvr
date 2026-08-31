// 카메라/스트림 서비스와 WebSocket 서버를 조립하는 애플리케이션 컨텍스트다.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

	if err := ensureMasterKey(configDir, mgr); err != nil {
		return nil, err
	}

	cameraSvc := &CameraService{mgr: mgr, appCfg: appCfg, configDir: configDir}
	return &App{
		Camera: cameraSvc,
		Stream: NewStreamService(mgr),
	}, nil
}

// ensureMasterKey는 마스터 키를 확보하고 필요 시 레거시 폴백 키로 암호화된
// 비밀번호를 새 키로 마이그레이션한다.
// 우선순위: 환경변수 > 키 파일(config/.masterkey) > 신규 생성(폴백 키 마이그레이션).
func ensureMasterKey(configDir string, mgr *camera.Manager) error {
	keyFile := filepath.Join(configDir, ".masterkey")

	// 1) 환경변수가 최우선 — 파일/마이그레이션 없이 사용
	if os.Getenv(config.EnvMasterKey) != "" {
		config.SetSessionKey(nil, config.KeySourceEnv)
		return nil
	}

	// 2) 키 파일 존재 → 로드
	if b, err := os.ReadFile(keyFile); err == nil {
		key, err := hex.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(key) != 32 {
			return fmt.Errorf("마스터 키 파일이 손상되었습니다: %s (삭제 후 재생성 가능)", keyFile)
		}
		config.SetSessionKey(key, config.KeySourceFile)
		return nil
	}

	// 3) 신규 생성 — 기존 비밀번호가 폴백 키로 암호화되어 있으면 마이그레이션
	cams, err := mgr.List()
	if err != nil {
		return err
	}
	plaintexts := map[string]string{}
	for i := range cams {
		if cams[i].Password == "" {
			continue
		}
		pt, err := mgr.PasswordOf(&cams[i]) // 현재(폴백) 키로 복호화 시도
		if err != nil {
			slog.Warn("기존 비밀번호 복호화 실패 — 마이그레이션에서 제외", "camera", cams[i].ID)
			continue
		}
		plaintexts[cams[i].ID] = pt
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("마스터 키 생성 실패: %w", err)
	}
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(key)), 0o600); err != nil {
		return fmt.Errorf("마스터 키 파일 저장 실패: %w", err)
	}
	config.SetSessionKey(key, config.KeySourceFile)

	// 새 키로 재암호화
	for i := range cams {
		pt, ok := plaintexts[cams[i].ID]
		if !ok {
			continue
		}
		if _, err := mgr.Update(cams[i].ID, camera.UpdateRequest{Password: &pt}); err != nil {
			slog.Warn("비밀번호 재암호화 실패", "camera", cams[i].ID, "err", err)
		}
	}
	if len(plaintexts) > 0 {
		slog.Info("마스터 키 신규 생성 + 기존 비밀번호 마이그레이션 완료", "cameras", len(plaintexts), "file", keyFile)
	}
	return nil
}

// StartWSServer는 로컬호스트(:8080)에 HTTP/WS 서버를 시작한다.
// mux: /ws(스트림 중계) + /api/*(REST, v1.1). 포트 충돌 시 오류를 반환한다.
func (a *App) StartWSServer() error {
	a.wsServer = ws.NewServer(a.Stream, fmt.Sprintf("127.0.0.1:%d", a.Camera.appCfg.Server.WSPort))

	mux := http.NewServeMux()
	mux.Handle("/ws", a.wsServer.Mux())
	RegisterHTTP(mux, a)

	if err := a.wsServer.StartWithHandler(CORS(mux)); err != nil {
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

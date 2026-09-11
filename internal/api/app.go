// 카메라/스트림 서비스와 WebSocket 서버를 조립하는 애플리케이션 컨텍스트다.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"webnvr/internal/camera"
	"webnvr/internal/config"
	"webnvr/internal/db"
	"webnvr/internal/logging"
	"webnvr/internal/recording"
	"webnvr/internal/ws"
)

// App는 webnvr 백엔드의 서비스 집합이다.
type App struct {
	Camera *CameraService
	Stream *StreamService

	srvMu     sync.Mutex       // wsServer/uiAssets 교체 보호 (설정 저장 → 재바인딩)
	wsServer  *ws.Server       // 현재 서비스 중인 서버
	uiAssets  http.FileSystem  // UI 서빙용 자산 (재바인딩 시 재사용)
	logCloser io.Closer
	database  *db.DB
	recording *recording.Manager // 녹화 매니저 — 항상 생성(아이들), enabled 시 녹화
}

// New는 설정 디렉토리를 기준으로 모든 서비스를 초기화한다.
func New(configDir string) (*App, error) {
	// SQLite 저장소 초기화 (스키마 적용 + 기존 config/*.json 1회 이관)
	database, err := db.Open(configDir)
	if err != nil {
		return nil, fmt.Errorf("저장소 초기화 실패: %w", err)
	}

	appCfg, err := database.LoadConfig()
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("앱 설정 로드 실패: %w", err)
	}
	if err := config.Validate(appCfg); err != nil {
		database.Close()
		return nil, fmt.Errorf("앱 설정 검증 실패: %w", err)
	}

	// 로거 초기화 (설정 검증 직후, 이 시점부터 모든 slog 호출이 파일에 기록됨)
	logCloser, err := logging.Setup(appCfg.Logging)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("로거 설정 실패: %w", err)
	}

	mgr := camera.NewManager(camera.NewSQLCameraStore(database.SQL()))

	if err := ensureMasterKey(configDir, mgr); err != nil {
		logCloser.Close()
		database.Close()
		return nil, err
	}

	cameraSvc := &CameraService{mgr: mgr, appCfg: appCfg, configDir: configDir, database: database}
	streamSvc := NewStreamService(mgr)

	// 녹화 매니저 (Phase R) — 항상 생성(아이들). enabled 시 녹화 세션을 맺고,
	// 카메라 mutation/설정 변경을 즉시 반영한다(reconcile). Hub 영구 ref로 24/7 RTSP 세션 유지.
	recMgr, err := recording.NewManager(
		appCfg.Recording, recording.NewStore(database.SQL()), streamSvc.Hub(), mgr)
	if err != nil {
		logCloser.Close()
		database.Close()
		return nil, fmt.Errorf("녹화 매니저 초기화 실패: %w", err)
	}
	recMgr.Start(context.Background()) // janitor 포함 — 실패해도 서비스는 계속(로그로 추적)
	cameraSvc.recManager = recMgr

	app := &App{
		Camera:    cameraSvc,
		Stream:    streamSvc,
		logCloser: logCloser,
		database:  database,
		recording: recMgr,
	}
	return app, nil
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

// StartWSServer는 설정된 바인드 주소(설정 ws_port)에 HTTP/WS 서버를 시작한다.
// mux: /ws(스트림 중계) + /api/*(REST, v1.1) + /(프론트 UI). 포트 충돌 시 오류를 반환한다.
func (a *App) StartWSServer(assets http.FileSystem) error {
	a.srvMu.Lock()
	a.uiAssets = assets
	a.wsServer = a.newWSServer()
	a.srvMu.Unlock()
	a.wireNotifier(a.wsServer)

	if err := a.wsServer.StartWithHandler(a.buildMux()); err != nil {
		return err
	}
	if bind := a.Camera.appCfg.Server.Bind; bind != "" && bind != "127.0.0.1" && bind != "localhost" {
		slog.Warn("HTTP 서버가 비사설 루프백 주소에 바인딩되었습니다 — 인증(Phase 6) 전까지 LAN 노출에 유의", "bind", bind)
	}
	return nil
}

// newWSServer는 현재 설정으로 WS 서버를 만든다.
func (a *App) newWSServer() *ws.Server {
	bind := a.Camera.appCfg.Server.Bind
	if bind == "" {
		bind = "127.0.0.1" // 설정 누락 시 안전한 기본값
	}
	srv := ws.NewServer(a.Stream, fmt.Sprintf("%s:%d", bind, a.Camera.appCfg.Server.WSPort), func() int {
		return a.Camera.AppConfig().Server.MaxClients
	})
	srv.TLSPort = a.Camera.appCfg.Server.TLSPort // 0이면 서버가 기본값(8443) 사용
	return srv
}

// wireNotifier는 카메라/녹화 변경 브로드캐스트가 지정 서버로 향하게 연결한다.
func (a *App) wireNotifier(srv *ws.Server) {
	a.Camera.notifier = srv
	if a.recording != nil {
		a.recording.SetNotifier(srv) // 녹화 세션 변화 → recording_state 브로드캐스트
	}
}

// buildMux는 /ws + /api/* + /(UI) 라우트의 전체 핸들러 체인을 만든다.
// a.wsServer의 /ws 핸들러를 사용하므로 서버 교체(activate) 후에 호출해야 한다.
func (a *App) buildMux() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/ws", a.wsServer.Mux())
	RegisterHTTP(mux, a)
	RegisterRecordingHTTP(mux, a)
	if a.uiAssets != nil {
		registerUI(mux, a.uiAssets)
	}
	return AccessLog(CORS(mux))
}

// ApplyServerRestart는 Server 설정 변경(ws_port/bind/tls_port)을 실행 중인 리스너에 반영한다.
// 변경이 없으면 아무 것도 하지 않는다.
//
// 포트 변경: 기존 클라이언트에 새 포트를 통지(server_restarting) → 새 포트를 먼저 열고
// 성공 시 구서버를 닫는다(무중단 전환). 새 포트 바인딩 실패 시 구서버를 유지한다.
// 동일 포트(bind/tls 변경): 통지 → 구서버 정지 → 재바인딩(순간 끊김). 실패 시 이전
// 설정으로 롤백해 서버가 죽지 않게 유지한다(DB에는 사용자가 저장한 값이 남는다).
func (a *App) ApplyServerRestart(old config.ServerConfig) {
	a.srvMu.Lock()
	newCfg := a.Camera.appCfg.Server
	cur := a.wsServer
	a.srvMu.Unlock()
	if cur == nil || newCfg == old {
		return
	}

	// ① 기존 클라이언트에 새 포트 통지 — 프론트는 새 주소로 이동하거나 셸은 reload한다
	cur.Broadcast(ws.ServerMsg{Type: ws.MsgServerRestart, Port: newCfg.WSPort})
	time.Sleep(1200 * time.Millisecond) // 통지 전송 여유

	activate := func(srv *ws.Server) {
		a.srvMu.Lock()
		a.wsServer = srv
		a.srvMu.Unlock()
		a.wireNotifier(srv)
	}
	tryStart := func(srv *ws.Server) error {
		activate(srv) // buildMux가 새 서버의 /ws를 가리키도록 먼저 교체
		return srv.StartWithHandler(a.buildMux())
	}
	newSrv := a.newWSServer()
	sameAddr := old.WSPort == newCfg.WSPort && old.Bind == newCfg.Bind

	if !sameAddr {
		// 무중단: 새 포트를 먼저 연다 — 실패하면 구서버를 유지한다
		if err := tryStart(newSrv); err != nil {
			activate(cur) // 되돌림
			slog.Error("새 포트 바인딩 실패 — 기존 포트로 계속 서빙", "port", newCfg.WSPort, "err", err)
			cur.Broadcast(ws.ServerMsg{Type: ws.MsgStreamError, Error: fmt.Sprintf("포트 %d 바인딩 실패 — 기존 포트로 계속 서빙합니다", newCfg.WSPort)})
			return
		}
		time.Sleep(300 * time.Millisecond) // 새 리스너 안정화 후 구서버 정지
		if err := cur.Stop(); err != nil {
			slog.Warn("구서버 정지 실패", "err", err)
		}
		slog.Info("서버 포트 전환 완료", "port", newCfg.WSPort)
		return
	}

	// 동일 포트 — 정지 후 재바인딩 (순간 끊김)
	if err := cur.Stop(); err != nil {
		slog.Warn("구서버 정지 실패", "err", err)
	}
	if err := tryStart(newSrv); err != nil {
		slog.Error("재바인딩 실패 — 이전 설정으로 롤백", "err", err)
		a.Camera.appCfg.Server = old // 런타임 설정 복구 (DB는 사용자가 저장한 값 유지)
		rb := a.newWSServer()
		if err2 := tryStart(rb); err2 != nil {
			slog.Error("롤백 바인딩도 실패 — 설정과 서버 상태를 수동 확인하세요", "err", err2)
		}
	}
}

// registerUI는 임베디드 프론트엔드(dist)를 "/"로 서빙한다.
// 알 수 없는 경로는 SPA 폴백으로 index.html을 반환한다.
func registerUI(mux *http.ServeMux, assets http.FileSystem) {
	fileServer := http.FileServer(assets)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := assets.Open(p); err != nil {
			r.URL.Path = "/" // SPA 폴백
		}
		fileServer.ServeHTTP(w, r)
	})
}

// BackendPort는 설정된 WS 서비스 포트를 반환한다. (Wails 셸의 프론트 포트 주입용 —
// StartWSServer가 성공하면 실제 바인딩 포트와 동일하다)
func (a *App) BackendPort() int {
	return a.Camera.appCfg.Server.WSPort
}

// LANAddresses는 로컬 머신의 LAN IPv4 주소 목록을 반환한다. (접속 URL 안내용)
func LANAddresses(port int) []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, addr := range addrs {
			if ipn, ok := addr.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
				out = append(out, fmt.Sprintf("http://%s:%d", ipn.IP.String(), port))
			}
		}
	}
	return out
}

// StopWSServer는 WebSocket 서버와 모든 스트림을 정지한다.
func (a *App) StopWSServer() {
	a.srvMu.Lock()
	srv := a.wsServer
	a.srvMu.Unlock()
	if srv != nil {
		if err := srv.Stop(); err != nil {
			slog.Warn("WS 서버 정지 실패", "err", err)
		}
	}
}

// Close는 서버와 로거를 정지하고 리소스를 정리한다.
func (a *App) Close() {
	a.StopWSServer()
	if a.recording != nil {
		a.recording.Close() // 진행 중 세그먼트 flush + INSERT (DB 닫기 전)
	}
	if a.database != nil {
		_ = a.database.Close()
	}
	if a.logCloser != nil {
		_ = a.logCloser.Close()
	}
}

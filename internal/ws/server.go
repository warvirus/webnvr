// gorilla/websocket 기반 로컬 WebSocket 서버다. 프론트엔드와 스트림 제어/수신을 담당한다.
package ws

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// readWait/pongWait는 하트비트 타임아웃이다.
const (
	readWait     = 90 * time.Second
	writeWait    = 10 * time.Second
	upgraderBuff = 4096
)

// Server는 로컬 WebSocket 서버다. 인증이 없는 Phase에서는 로컬호스트에만 바인딩한다.
type Server struct {
	ctrl       Controller
	addr       string
	maxClients func() int // 최대 동시 접속 수 (nil 또는 0 반환 = 무제한)

	mu    sync.Mutex
	ln    net.Listener
	http  *http.Server
	conns map[*connState]struct{}
}

// NewServer는 컨트롤러와 바인딩 주소로 서버를 생성한다.
// maxClients는 최대 동시 접속 수를 반환하는 함수 (nil이면 무제한)
func NewServer(ctrl Controller, addr string, maxClients func() int) *Server {
	return &Server{ctrl: ctrl, addr: addr, maxClients: maxClients, conns: map[*connState]struct{}{}}
}

// Mux는 업그레이드 엔드포인트(/ws)를 등록한 mux를 반환한다.
// 추가 라우트(예: /api/*)는 호출자가 같은 mux에 등록해 하나의 포트로 제공한다.
func (s *Server) Mux() http.Handler {
	return s.mux()
}

// Start는 서버를 시작한다. (비블로킹)
func (s *Server) Start() error {
	return s.StartWithHandler(s.mux())
}

// StartWithHandler는 지정 핸들러로 서버를 시작한다. (비블로킹)
// 호출자가 /ws 외의 추가 라우트(예: /api/*)를 mux에 등록해 사용할 수 있다.
// HTTPS 인증서가 있으면 HTTP + HTTPS 동시 지원 (다른 포트: HTTP=:8080, HTTPS=:8443)
func (s *Server) StartWithHandler(h http.Handler) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("WS 서버 포트 바인딩 실패 (%s): %w", s.addr, err)
	}
	s.mu.Lock()
	s.ln = ln
	s.http = &http.Server{Handler: h}
	s.mu.Unlock()

	certFile := os.Getenv("WEB_CERT")
	keyFile := os.Getenv("WEB_KEY")

	// HTTP 서버 시작
	go func() {
		slog.Info("HTTP/WS 서버 시작", "addr", ln.Addr().String())
		if err := s.http.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP 서버 오류", "err", err)
		}
	}()

	// HTTPS 인증서가 있으면 HTTPS 서버도 별도 포트에서 시작
	if certFile != "" && keyFile != "" {
		httpsAddr := strings.Replace(s.addr, ":8080", ":8443", 1)
		lnTLS, err := net.Listen("tcp", httpsAddr)
		if err != nil {
			slog.Warn("HTTPS 포트 바인딩 실패", "addr", httpsAddr, "err", err)
			// HTTPS 실패는 경고만 하고 HTTP로 계속 진행
			return nil
		}

		go func() {
			httpsTLS := &http.Server{Handler: h}
			slog.Info("HTTPS/WSS 서버 시작", "addr", lnTLS.Addr().String())
			if err := httpsTLS.ServeTLS(lnTLS, certFile, keyFile); err != nil && err != http.ErrServerClosed {
				slog.Error("HTTPS 서버 오류", "err", err)
			}
		}()
	}

	return nil
}

// Broadcast는 접속 중인 모든 클라이언트에게 제어 메시지를 보낸다.
// conns 스냅샷을 뜬 뒤 conn별 goroutine으로 전송해 느린 구독자가 RTP 펌프를 막지 않게 한다
// (제어 메시지라 RTP 스트림과의 순서 보장은 필요 없다).
func (s *Server) Broadcast(m ServerMsg) {
	s.mu.Lock()
	targets := make([]*connState, 0, len(s.conns))
	for st := range s.conns {
		targets = append(targets, st)
	}
	s.mu.Unlock()
	for _, st := range targets {
		go st.sendMsg(m)
	}
}

// BroadcastCamerasChanged는 카메라 목록/설정 변경을 모든 클라이언트에 알린다. (api.changeBroadcaster)
func (s *Server) BroadcastCamerasChanged(reason, cameraID string) {
	s.Broadcast(ServerMsg{Type: MsgCamerasChanged, Reason: reason, CameraID: cameraID})
}

// BroadcastConfigChanged는 앱 설정 변경을 모든 클라이언트에 알린다. (api.changeBroadcaster)
func (s *Server) BroadcastConfigChanged() {
	s.Broadcast(ServerMsg{Type: MsgConfigChanged})
}

// Addr은 실제 바인딩된 주소다.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Stop은 서버를 정지하고 모든 연결을 닫는다.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.http == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := s.http.Shutdown(ctx)
	s.http = nil
	s.ln = nil
	return err
}

// mux는 업그레이드 엔드포인트를 구성한다.
func (s *Server) mux() http.Handler {
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{
		ReadBufferSize:  upgraderBuff,
		WriteBufferSize: upgraderBuff,
		CheckOrigin: func(r *http.Request) bool {
			return true // 로컬 앱이므로 오리진 제한 없음
		},
	}
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Warn("WS 업그레이드 실패", "err", err)
			return
		}

		// 동시 접속 제한 체크
		if s.maxClients != nil {
			if max := s.maxClients(); max > 0 {
				s.mu.Lock()
				current := len(s.conns)
				s.mu.Unlock()
				if current >= max {
					slog.Warn("📊 동시 접속 제한 도달", "current", current, "max", max)
					_ = conn.WriteJSON(ServerMsg{Type: MsgClientLimitExceeded})
					_ = conn.Close()
					return
				}
			}
		}

		s.serveConn(conn)
	})
	return mux
}

// serveConn은 연결별 읽기 루프와 하트비트를 실행한다.
func (s *Server) serveConn(conn *websocket.Conn) {
	st := &connState{conn: conn, cancels: map[string]func(){}}
	s.mu.Lock()
	s.conns[st] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, st)
		s.mu.Unlock()
		st.cleanupAll()
		_ = conn.Close()
	}()

	heartbeatStop := make(chan struct{})
	go st.heartbeat(heartbeatStop)
	defer close(heartbeatStop)

	conn.SetReadDeadline(time.Now().Add(readWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(readWait))
	})

	for {
		var m ClientMsg
		if err := conn.ReadJSON(&m); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				slog.Debug("WS 연결 종료", "err", err)
			}
			return
		}
		conn.SetReadDeadline(time.Now().Add(readWait))
		st.handleClientMsg(s.ctrl, m)
	}
}

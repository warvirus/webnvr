// gorilla/websocket 기반 로컬 WebSocket 서버다. 프론트엔드와 스트림 제어/수신을 담당한다.
package ws

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
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
	ctrl Controller
	addr string

	mu    sync.Mutex
	ln    net.Listener
	http  *http.Server
	conns map[*connState]struct{}
}

// NewServer는 컨트롤러와 바인딩 주소로 서버를 생성한다.
func NewServer(ctrl Controller, addr string) *Server {
	return &Server{ctrl: ctrl, addr: addr, conns: map[*connState]struct{}{}}
}

// Start는 서버를 시작한다. (비블로킹)
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("WS 서버 포트 바인딩 실패 (%s): %w", s.addr, err)
	}
	s.mu.Lock()
	s.ln = ln
	s.http = &http.Server{Handler: s.mux()}
	s.mu.Unlock()

	go func() {
		if err := s.http.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("WS 서버 오류", "err", err)
		}
	}()
	slog.Info("WS 서버 시작", "addr", ln.Addr().String())
	return nil
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

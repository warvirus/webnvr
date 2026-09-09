// webnvr 백엔드(HTTP API + WS + UI 서빙)를 데스크톱 창 없이 단독 실행하는 진입점이다.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"webnvr/internal/api"
)

func main() {
	// 백엔드 서비스 초기화 (설정 디렉토리: ./config — 저장소 루트에서 실행할 것)
	appCtx, err := api.New("config")
	if err != nil {
		slog.Error("백엔드 서비스 초기화 실패", "err", err)
		os.Exit(1)
	}

	// 빌드된 프론트엔드가 있으면 :8080/ 로 함께 서빙, 없으면 API/WS만 제공
	var ui http.FileSystem
	if st, err := os.Stat("frontend/dist"); err == nil && st.IsDir() {
		ui = http.Dir("frontend/dist")
	} else {
		slog.Warn("frontend/dist 없음 — UI 서빙 생략, API/WS만 제공 (npm run build 후 재실행하면 UI도 서빙)")
	}

	if err := appCtx.StartWSServer(ui); err != nil {
		slog.Error("HTTP/WS 서버 시작 실패 — 8080 포트를 점유한 프로세스를 종료하거나 config/app.json의 ws_port를 변경하세요", "err", err)
		appCtx.Close() // 녹화 세그먼트 flush 포함 정리
		os.Exit(1)
	}
	for _, u := range api.LANAddresses(8080) {
		slog.Info("LAN 접속 가능: " + u + " (server.bind가 0.0.0.0일 때)")
	}

	slog.Info("백엔드 단독 실행 중 — 종료하려면 Ctrl+C")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	appCtx.Close()
}

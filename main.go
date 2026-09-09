// webnvr Wails 앱의 진입점이다. Wails는 창(셸) 역할만 하며,
// 프론트엔드는 백엔드의 HTTP API(8080)와 WS로 통신한다 (doc v1.1 §5).
package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"webnvr/internal/api"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	headless := flag.Bool("headless", false, "데스크톱 창 없이 백엔드(HTTP API + WS + UI)만 실행")
	flag.Parse()

	// 백엔드 서비스 초기화 (설정 디렉토리: ./config)
	appCtx, err := api.New("config")
	if err != nil {
		slog.Error("백엔드 서비스 초기화 실패", "err", err)
		os.Exit(1)
	}

	// HTTP API + UI 서빙 + 스트림 중계 WS 서버 (server.bind에 바인딩)
	// v1.1: 포트 점유 시 프론트엔드도 동작할 수 없으므로 명확히 종료한다.
	// 임베디드 자산은 frontend/dist/ 프리픽스를 가지므로 UI 서빙용으로 서브트리를 잘라 넘긴다.
	uiFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		slog.Error("임베디드 UI 서브트리 접근 실패", "err", err)
		appCtx.Close() // 녹화 세그먼트 flush 포함 정리
		os.Exit(1)
	}
	if err := appCtx.StartWSServer(http.FS(uiFS)); err != nil {
		slog.Error("HTTP/WS 서버 시작 실패 — 8080 포트를 점유한 프로세스를 종료하거나 config/app.json의 ws_port를 변경하세요", "err", err)
		appCtx.Close() // 녹화 세그먼트 flush 포함 정리
		os.Exit(1)
	}

	for _, u := range api.LANAddresses(8080) {
		slog.Info("LAN 접속 가능: " + u + " (server.bind가 0.0.0.0일 때)")
	}

	// 헤드리스 모드: 데스크톱 창 없이 백엔드만 상주한다. (터미널 실행 전용)
	if *headless {
		slog.Info("헤드리스 모드 — 데스크톱 창 없이 백엔드만 실행. 종료하려면 Ctrl+C")
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		appCtx.Close()
		return
	}

	err = wails.Run(&options.App{
		Title:  "webnvr",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnShutdown: func(_ context.Context) {
			appCtx.Close()
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

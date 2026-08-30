// webnvr Wails 앱의 진입점이다. 백엔드 서비스를 생성해 프론트엔드에 바인딩한다.
package main

import (
	"context"
	"embed"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"webnvr/internal/api"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 백엔드 서비스 초기화 (설정 디렉토리: ./config)
	appCtx, err := api.New("config")
	if err != nil {
		slog.Error("백엔드 서비스 초기화 실패", "err", err)
		os.Exit(1)
	}

	// 스트림 전달용 로컬 WebSocket 서버
	if err := appCtx.StartWSServer(); err != nil {
		slog.Warn("WebSocket 서버 시작 실패 (모니터링 스트림 사용 불가)", "err", err)
	}

	// Create application with options
	err = wails.Run(&options.App{
		Title:  "webnvr",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnShutdown: func(_ context.Context) {
			appCtx.StopWSServer()
		},
		Bind: []interface{}{
			appCtx.Camera,
			appCtx.Stream,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

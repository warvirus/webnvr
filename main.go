// webnvr Wails 앱의 진입점이다. 백엔드 서비스를 생성해 프론트엔드에 바인딩한다.
package main

import (
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
	// Create an instance of the app structure
	app := NewApp()

	// 카메라 관리 서비스 초기화 (설정 디렉토리: ./config)
	cameraSvc, err := api.NewCameraService("config")
	if err != nil {
		slog.Error("카메라 서비스 초기화 실패", "err", err)
		os.Exit(1)
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
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
			cameraSvc,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

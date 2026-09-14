package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:             "Downloader - 多线程下载工具",
		Width:             980,
		Height:            660,
		MinWidth:          720,
		MinHeight:         480,
		// 关闭行为由 OnBeforeClose 按设置决定：询问 / 直接退出 / 最小化到托盘
		HideWindowOnClose: false,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 24, G: 28, B: 38, A: 1},
		OnStartup:        app.startup,
		OnBeforeClose:    app.onBeforeClose,
		OnShutdown:       app.shutdown,
		// 单实例锁：第二个实例启动时自动退出，并由首个实例唤起主窗口
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "downloader-single-instance-7c1f4a92",
			OnSecondInstanceLaunch: app.onSecondInstanceLaunch,
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}

	// 托盘/WebView2 可能残留非 daemon 线程，强制结束进程，
	// 避免窗口已关、任务管理器里仍有 downloader。
	os.Exit(0)
}

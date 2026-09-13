package main

import (
	"embed"

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
		HideWindowOnClose: true, // 点关闭最小化到托盘，由托盘菜单退出
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 24, G: 28, B: 38, A: 1},
		OnStartup:        app.startup,
		// 单实例锁：第二个实例启动时自动退出，并由首个实例唤起主窗口
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:              "downloader-single-instance-7c1f4a92",
			OnSecondInstanceLaunch: app.onSecondInstanceLaunch,
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

package main

import (
	_ "embed"

	"fyne.io/systray"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

// initTray 在系统托盘创建图标。Wails v2 本身不提供托盘 API，
// 使用 fyne-io/systray 以 Register 方式嵌入（不阻塞主线程）。
func (a *App) initTray() {
	go func() {
		systray.Register(a.onTrayReady, func() {})
	}()
}

func (a *App) onTrayReady() {
	systray.SetIcon(trayIcon)
	systray.SetTooltip("Downloader - 多线程下载工具")

	mShow := systray.AddMenuItem("显示主窗口", "显示下载器主窗口")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "退出程序")

	go func() {
		for {
			select {
			case <-mShow.ClickedCh:
				a.ShowWindow()
			case <-mQuit.ClickedCh:
				a.Quit()
				return
			}
		}
	}()
}

package main

import (
	_ "embed"
	"runtime"
	"sync"

	"fyne.io/systray"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

var trayQuitOnce sync.Once

// quitTray 关闭托盘图标并结束 systray.Run 消息循环。
// 必须在进程退出前调用，否则 Windows 上会出现图标残留/进程不退。
func quitTray() {
	trayQuitOnce.Do(systray.Quit)
}

// initTray 在系统托盘创建图标。Wails v2 本身不提供托盘 API，
// 使用 fyne.io/systray。
//
// 注意必须用 Run 而不是 Register：Windows 上 Register 只创建图标、
// 不启动消息泵，goroutine 退出后托盘窗口将收不到任何点击消息；
// 同时用 LockOSThread 把窗口创建与 GetMessage 循环固定在同一
// OS 线程，避免 goroutine 线程迁移导致消息丢失。
func (a *App) initTray() {
	go func() {
		runtime.LockOSThread()
		systray.Run(a.onTrayReady, func() {})
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

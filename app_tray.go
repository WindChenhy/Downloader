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

// onTrayReady 托盘消息循环就绪后初始化菜单与点击处理。
// 菜单文案按当前界面语言生成；语言变更在下次启动时应用到托盘。
func (a *App) onTrayReady() {
	en := a.uiLanguage() == "en-US"
	systray.SetIcon(trayIcon)
	if en {
		systray.SetTooltip("Downloader - Multi-threaded Download Manager")
	} else {
		systray.SetTooltip("Downloader - 多线程下载工具")
	}

	mShow := systray.AddMenuItem(a.text("显示主窗口", "Show Main Window"), a.text("显示下载器主窗口", "Show the main window"))
	systray.AddSeparator()
	mQuit := systray.AddMenuItem(a.text("退出", "Quit"), a.text("退出程序", "Quit the application"))

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

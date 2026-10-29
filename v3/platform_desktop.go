//go:build !android

package main

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
	"time"

	"downloader/engine"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

// appDataDir 任务列表与设置所在目录（用户配置目录下）。
func appDataDir() string {
	dataDir, err := os.UserConfigDir()
	if err != nil {
		dataDir = "."
	}
	return filepath.Join(dataDir, "downloader")
}

// stageRoot 下载暂存目录的上级（用户缓存目录下）：
// 下载中的 .part 文件不出现在下载目录里，完成时才移动过去。
func stageRoot() string {
	if cacheDir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cacheDir, "downloader")
	}
	return appDataDir()
}

// registerNotifications 桌面平台：注册 Wails 通知服务（Windows/macOS/Linux）
// 并把服务实例绑定到 AppService 的 notifier。
func (s *AppService) registerNotifications(services []application.Service) []application.Service {
	nsvc := notifications.New()
	s.notifier = &desktopNotifier{svc: nsvc}
	return append(services, application.NewService(nsvc))
}

// desktopNotifier 基于 Wails v3 通知服务的平台后端。
type desktopNotifier struct {
	svc *notifications.NotificationService
}

func (d *desktopNotifier) requestAuth() {
	_, _ = d.svc.RequestNotificationAuthorization()
}

func (d *desktopNotifier) send(id, title, body string) {
	_ = d.svc.SendNotification(notifications.NotificationOptions{
		ID:    id,
		Title: title,
		Body:  body,
	})
}

// showWindow 显示并激活主窗口（最小化时先还原）。
func (s *AppService) showWindow() {
	if s.window == nil {
		return
	}
	s.window.UnMinimise()
	s.window.Show()
	s.window.Focus()
}

// initTray 在系统托盘创建图标与菜单。Wails v3 内置托盘支持，
// 不再需要 v2 时代的 fyne.io/systray 与 LockOSThread 处理。
func (s *AppService) initTray() {
	if s.app == nil {
		return
	}
	tray := s.app.SystemTray.New()
	tray.SetIcon(trayIcon)
	if s.uiLanguage() == "en-US" {
		tray.SetTooltip("Downloader - Multi-threaded Download Manager")
	} else {
		tray.SetTooltip("Downloader - 多线程下载工具")
	}

	menu := s.app.NewMenu()
	menu.Add(s.text("显示主窗口", "Show Main Window")).OnClick(func(*application.Context) {
		s.ShowWindow()
	})
	menu.AddSeparator()
	menu.Add(s.text("退出", "Quit")).OnClick(func(*application.Context) {
		s.Quit()
	})
	tray.SetMenu(menu)
	s.tray = tray
}

// quitTray 移除托盘图标。必须在进程退出前调用，
// 否则 Windows 上会出现图标残留。
func (s *AppService) quitTray() {
	if s.tray != nil {
		s.tray.Destroy()
		s.tray = nil
	}
}

// startClipboardWatch 启动剪贴板轮询（桌面专属）。
func (s *AppService) startClipboardWatch() {
	go s.watchClipboard()
}

// watchClipboard 轮询剪贴板：发现新的可下载链接时向前端推送
// clipboard:url 事件，由前端弹预填好的新建下载对话框。
func (s *AppService) watchClipboard() {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	var last string
	for {
		select {
		case <-s.clipboardStop:
			return // 应用退出：停止轮询，别在收尾阶段继续调运行时
		case <-ticker.C:
		}
		if s.app == nil || s.mgr == nil {
			continue
		}
		if !s.mgr.GetSettings().ClipboardWatch {
			continue
		}
		text, ok := s.app.Clipboard.Text()
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" || text == last {
			continue
		}
		last = text
		if !engine.IsDownloadableURL(text) {
			continue
		}
		if s.mgr.HasTaskWithURL(text) {
			continue
		}
		s.app.Event.Emit("clipboard:url", text)
	}
}

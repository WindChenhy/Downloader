//go:build !android

package main

import (
	_ "embed"
	"fmt"
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
	tray.SetTooltip(s.trayIdleTooltip())
	// 左键单击唤起主窗口：Windows 上左键默认只打一条调试日志、右键才弹菜单
	//（见 wails systemtray_windows.go 的 WndProc 分派），下载器的惯常预期是
	// 单击图标回到主窗口
	tray.OnClick(s.ShowWindow)
	// 先落字段再建菜单：buildTrayMenu 经 s.tray.SetMenu 挂菜单
	s.tray = tray
	s.buildTrayMenu()
}

// buildTrayMenu 构建托盘菜单并记录文案项引用，语言切换时原地改标签。
func (s *AppService) buildTrayMenu() {
	if s.tray == nil || s.app == nil {
		return
	}
	menu := s.app.NewMenu()
	s.trayShowItem = menu.Add(s.text("显示主窗口", "Show Main Window"))
	s.trayShowItem.OnClick(func(*application.Context) { s.ShowWindow() })
	menu.AddSeparator()
	s.trayQuitItem = menu.Add(s.text("退出", "Quit"))
	s.trayQuitItem.OnClick(func(*application.Context) { s.Quit() })
	s.tray.SetMenu(menu)
	s.trayMenu = menu
}

// refreshTray 界面语言变化时刷新托盘：菜单标签原地更新 + 提示按当前
// 任务状态重算（下载中会切到新语言的「N 个任务下载中」文案）。
func (s *AppService) refreshTray() {
	if s.tray == nil || s.trayShowItem == nil {
		return
	}
	s.trayShowItem.SetLabel(s.text("显示主窗口", "Show Main Window"))
	s.trayQuitItem.SetLabel(s.text("退出", "Quit"))
	s.trayMenu.Update()
	if s.mgr != nil {
		s.updateTrayStatus(s.mgr.GetTasks())
	}
}

// updateTrayStatus 用活跃任务数与总速度刷新托盘提示；全部空闲时回到
// 品牌文案。进度节拍 500ms 一推，文案没变化时跳过系统调用。
func (s *AppService) updateTrayStatus(tasks []engine.Task) {
	if s.tray == nil {
		return
	}
	status := s.trayStatusString(tasks)
	s.trayStatusMu.Lock()
	unchanged := status == s.trayStatus
	s.trayStatus = status
	s.trayStatusMu.Unlock()
	if unchanged {
		return
	}
	s.tray.SetTooltip(status)
}

// trayStatusString 拼托盘提示文案：N 个任务下载中 · 总速度。
func (s *AppService) trayStatusString(tasks []engine.Task) string {
	var active int
	var speed int64
	for _, t := range tasks {
		if t.Status == engine.StatusRunning {
			active++
			speed += t.Speed
		}
	}
	if active == 0 {
		return s.trayIdleTooltip()
	}
	if s.uiLanguage() == "en-US" {
		return fmt.Sprintf("Downloader - %d downloading · %s/s", active, humanBytes(speed))
	}
	return fmt.Sprintf("Downloader - %d 个任务下载中 · %s/s", active, humanBytes(speed))
}

// trayIdleTooltip 空闲时的品牌提示文案（按界面语言）。
func (s *AppService) trayIdleTooltip() string {
	if s.uiLanguage() == "en-US" {
		return "Downloader - Multi-threaded Download Manager"
	}
	return "Downloader - 多线程下载工具"
}

// humanBytes 托盘提示用的速度格式化（1 位小数，KMGTPE 进制 1024）。
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
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

//go:build android

package main

import (
	"fmt"
	"path/filepath"

	"downloader/engine"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// appDataDir 任务列表与设置所在目录。Android 没有用户配置目录概念，
// 使用应用私有文件目录（activity.getFilesDir()）下的 downloader 子目录，
// 该目录始终可写、随应用卸载清除。
func appDataDir() string {
	return filepath.Join(application.Mobile.StoragePath(), "downloader")
}

// stageRoot 下载暂存目录的上级：与数据目录同盘，完成时才移动到保存目录。
func stageRoot() string {
	return appDataDir()
}

// registerNotifications Android 平台：无需注册 Wails 通知服务（该服务无
// Android 实现），直接使用原生 Android 特性桥接发送通知。
func (s *AppService) registerNotifications(services []application.Service) []application.Service {
	s.notifier = androidNotifier{}
	return services
}

// androidNotifier 经 wails 的 Android 特性桥发送本地通知。
// Notify 的载荷为 JSON：{"title","body","delay":seconds}。
type androidNotifier struct{}

func (androidNotifier) requestAuth() {
	// POST_NOTIFICATIONS 权限在 Android 13+ 运行时授予，
	// wails 模板的 Manifest 已声明；首次通知由系统自行引导，无需预授权
}

func (androidNotifier) send(id, title, body string) {
	application.Android.Notify(fmt.Sprintf(`{"title":%q,"body":%q}`, title, body))
}

// showWindow 移动端单窗口常驻，无需唤起逻辑。
func (s *AppService) showWindow() {}

// initTray / quitTray Android 无系统托盘。
func (s *AppService) initTray() {}

func (s *AppService) quitTray() {}

// startClipboardWatch Android 不做剪贴板轮询：
// 后台读剪贴板受系统限制（Android 10+ 仅前台应用可读），
// 且常驻轮询耗电；粘贴由用户在新建对话框手动触发。
func (s *AppService) startClipboardWatch() {}

// updateTrayStatus / refreshTray Android 无系统托盘。
func (s *AppService) updateTrayStatus(tasks []engine.Task) {}

func (s *AppService) refreshTray() {}

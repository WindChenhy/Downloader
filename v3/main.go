package main

import (
	"embed"
	"log"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	runApp()
	if isDesktop() {
		// 托盘/WebView 可能残留非 daemon 线程，强制结束进程，
		// 避免窗口已关、任务管理器里仍有 downloader。
		// Android 上进程归 JVM 管（前台服务保后台下载），禁止 os.Exit。
		os.Exit(0)
	}
}

// runApp 构建并运行 Wails 应用。桌面由 main() 调用；Android 上
// build/android/gen/main_android.gen.go（由 `wails3 android overlay:gen` 生成）
// 把 main 注册为 JNI nativeInit 的入口。
func runApp() {
	svc := NewAppService()

	opts := application.Options{
		Name:        "Downloader",
		Description: "多线程下载工具",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	}
	// 系统通知按平台装配：桌面注册 Wails 通知服务，Android 走原生 Notify
	opts.Services = svc.registerNotifications(opts.Services)

	if isDesktop() {
		// 单实例锁：第二个实例启动时自动退出，并由首个实例唤起主窗口。
		// Android 上应用本身单实例（singleTask），无需此机制。
		opts.SingleInstance = &application.SingleInstanceOptions{
			UniqueID:               "downloader-single-instance-7c1f4a92",
			OnSecondInstanceLaunch: svc.onSecondInstanceLaunch,
		}
	}

	app := application.New(opts)
	svc.attach(app)
	app.OnShutdown(svc.onShutdown)

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Downloader - 多线程下载工具",
		Width:            980,
		Height:           660,
		MinWidth:         720,
		MinHeight:        480,
		BackgroundColour: application.NewRGB(24, 28, 38),
		URL:              "/",
	})
	svc.setWindow(win)
	if isDesktop() {
		// 关闭行为由设置决定：每次询问 / 直接退出 / 最小化到托盘；
		// Android 无窗口关闭按钮，不注册拦截
		win.OnWindowEvent(events.Common.WindowClosing, svc.onWindowClosing)
		// 「最小化时隐藏到托盘」：按设置生效，v2 没有的最小化钩子
		win.OnWindowEvent(events.Common.WindowMinimise, svc.onWindowMinimising)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// isDesktop 是否桌面平台。桌面专属能力（托盘/单实例/剪贴板监听/窗口关闭拦截）
// 都以此门控；iOS 暂不在支持范围，按桌面处理。
func isDesktop() bool {
	return runtime.GOOS != "android" && runtime.GOOS != "ios"
}

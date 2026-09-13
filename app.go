package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"downloader/engine"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 暴露给前端的绑定对象，所有导出方法均可在 JS 中调用。
type App struct {
	ctx context.Context
	mgr *engine.Manager
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	dataDir, err := os.UserConfigDir()
	if err != nil {
		dataDir = "."
	}
	dataDir = filepath.Join(dataDir, "downloader")

	mgr, err := engine.NewManager(dataDir, func(name string, data ...interface{}) {
		runtime.EventsEmit(ctx, name, data...)
		if name == "task:finished" && len(data) > 0 {
			if t, ok := data[0].(engine.Task); ok {
				a.notifyTaskFinished(t)
			}
		}
	})
	if err != nil {
		runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:    runtime.ErrorDialog,
			Title:   "初始化失败",
			Message: "无法初始化下载数据目录：" + err.Error(),
		})
		runtime.Quit(ctx)
		return
	}
	a.mgr = mgr

	// 系统通知
	_ = runtime.InitializeNotifications(ctx)
	_, _ = runtime.RequestNotificationAuthorization(ctx)

	// 本地 REST API
	if st := mgr.GetSettings(); st.APIEnabled && st.APIPort > 0 {
		mgr.StartAPI(fmt.Sprintf("127.0.0.1:%d", st.APIPort))
	}

	// 剪贴板监听
	go a.watchClipboard()

	a.initTray()
}

// GetTasks 返回全部任务（含实时进度）。
func (a *App) GetTasks() []engine.Task { return a.mgr.GetTasks() }

// AddTask 新建下载任务；saveDir 为空时使用设置中的默认目录，
// connections <= 0 时使用默认连接数，customName 为空则自动从链接/响应头获取文件名。
func (a *App) AddTask(url string, saveDir string, connections int, customName string) (engine.Task, error) {
	return a.mgr.AddTask(url, saveDir, connections, customName)
}

func (a *App) PauseTask(id string) error  { return a.mgr.PauseTask(id) }
func (a *App) ResumeTask(id string) error { return a.mgr.ResumeTask(id) }
// RemoveTask 删除任务记录；deleteFiles 为 true 时连同已下载文件一起删除。
func (a *App) RemoveTask(id string, deleteFiles bool) error {
	return a.mgr.RemoveTask(id, deleteFiles)
}

func (a *App) GetSettings() engine.Settings { return a.mgr.GetSettings() }

func (a *App) SaveSettings(s engine.Settings) error { return a.mgr.SaveSettings(s) }

// ShowWindow 显示并激活主窗口（托盘菜单使用）。
func (a *App) ShowWindow() {
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

// OpenFolder 在资源管理器中打开任务所在目录；
// 目标文件已存在时（如已完成）直接选中该文件。
func (a *App) OpenFolder(saveDir string, fileName string) error {
	dir, err := filepath.Abs(saveDir)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, fileName)
	if st, err := os.Stat(target); err == nil && !st.IsDir() {
		// explorer 接受 "/select,带空格路径" 整体加引号的形式
		return exec.Command("explorer", "/select,"+target).Start()
	}
	return exec.Command("explorer", dir).Start()
}

// onSecondInstanceLaunch 用户再次双击 exe 时被首个实例调用：
// 唤起主窗口；若第二个实例携带了下载链接参数，则自动创建任务。
func (a *App) onSecondInstanceLaunch(data options.SecondInstanceData) {
	if a.mgr == nil {
		return
	}
	a.ShowWindow()
	for _, arg := range data.Args {
		if engine.IsDownloadableURL(arg) {
			if _, err := a.mgr.AddTask(arg, "", 0, ""); err != nil {
				runtime.LogWarningf(a.ctx, "通过命令行创建任务失败: %v", err)
			}
		}
	}
}

// Quit 退出整个程序（托盘菜单使用）。
func (a *App) Quit() { runtime.Quit(a.ctx) }

package main

import (
	"context"
	"os"
	"path/filepath"

	"downloader/engine"

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
	a.initTray()
}

// GetTasks 返回全部任务（含实时进度）。
func (a *App) GetTasks() []engine.Task { return a.mgr.GetTasks() }

// AddTask 新建下载任务；saveDir 为空时使用设置中的默认目录，
// connections <= 0 时使用默认连接数。
func (a *App) AddTask(url string, saveDir string, connections int) (engine.Task, error) {
	return a.mgr.AddTask(url, saveDir, connections)
}

func (a *App) PauseTask(id string) error  { return a.mgr.PauseTask(id) }
func (a *App) ResumeTask(id string) error { return a.mgr.ResumeTask(id) }
func (a *App) RemoveTask(id string) error { return a.mgr.RemoveTask(id) }

func (a *App) GetSettings() engine.Settings { return a.mgr.GetSettings() }

func (a *App) SaveSettings(s engine.Settings) error { return a.mgr.SaveSettings(s) }

// ShowWindow 显示并激活主窗口（托盘菜单使用）。
func (a *App) ShowWindow() {
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

// Quit 退出整个程序（托盘菜单使用）。
func (a *App) Quit() { runtime.Quit(a.ctx) }

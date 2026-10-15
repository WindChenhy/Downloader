package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"downloader/engine"
	"downloader/engine/api"
	"downloader/sysaction"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// errNotReady 引擎尚未初始化。startup 里数据目录不可用时应用会走退出流程，
// 但在真正退出之前前端仍可能调用绑定方法，这里统一挡住而不是让 nil 解引用崩掉。
var errNotReady = errors.New("应用尚未初始化")

// App 暴露给前端的绑定对象，所有导出方法均可在 JS 中调用。
type App struct {
	ctx context.Context
	mgr *engine.Manager
	api *api.Server
	// quitting 主动退出标志：为 true 时 OnBeforeClose 不再拦截，
	// 否则托盘/弹窗选「退出」后 close 生命周期会再次进入询问逻辑，
	// 把窗口关掉却拦下进程退出，任务管理器里留下幽灵进程。
	quitting atomic.Bool
	// 通知开关缓存：notify 回调可能在引擎事件路径触发，禁止再抢 Manager 锁
	notifyCreate, notifyPause, notifyComplete, notifyFail atomic.Bool
	// uiLang 界面语言缓存（zh-CN / en-US），随设置同步；通知与托盘文案使用
	uiLang atomic.Value
	// clipboardStop 关闭后剪贴板监听退出；clipboardOnce 保证只关一次
	// （requestQuit 与 shutdown 都可能走到收尾）
	clipboardStop chan struct{}
	clipboardOnce sync.Once
}

func NewApp() *App { return &App{clipboardStop: make(chan struct{})} }

// startup Wails 启动回调：初始化引擎、通知、REST API、剪贴板与托盘。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	dataDir, err := os.UserConfigDir()
	if err != nil {
		dataDir = "."
	}
	dataDir = filepath.Join(dataDir, "downloader")

	// 分块暂存目录放系统本地应用数据（Windows = %LOCALAPPDATA%\downloader\staging）：
	// 下载中的 .part 文件不出现在下载目录里，完成时才移动过去
	stageRoot := dataDir
	if cacheDir, err := os.UserCacheDir(); err == nil {
		stageRoot = filepath.Join(cacheDir, "downloader")
	}

	mgr, err := engine.NewManager(dataDir,
		func(ev engine.Event) {
			// Wails 事件载荷与旧格式保持一致：列表事件发 []Task，其余发单个 Task
			payload := []interface{}{ev.Task}
			if ev.Name == engine.EventTasksChanged {
				payload = []interface{}{ev.Tasks}
			}
			runtime.EventsEmit(ctx, string(ev.Name), payload...)
			switch ev.Name {
			case engine.EventTaskCreated:
				a.notifyTaskCreated(ev.Task)
			case engine.EventTaskPaused:
				a.notifyTaskPaused(ev.Task)
			case engine.EventTaskFinished:
				a.notifyTaskFinished(ev.Task)
			case engine.EventQueueIdle:
				a.onQueueIdle()
			}
		},
		engine.WithStagingDir(filepath.Join(stageRoot, "staging")))
	if err != nil {
		runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:    runtime.ErrorDialog,
			Title:   "初始化失败",
			Message: "无法初始化下载数据目录：" + err.Error(),
		})
		a.quitting.Store(true)
		runtime.Quit(ctx)
		return
	}
	a.mgr = mgr
	a.syncNotifyFlags()

	// 系统通知
	_ = runtime.InitializeNotifications(ctx)
	_, _ = runtime.RequestNotificationAuthorization(ctx)

	// 本地 REST API（按设置启停；设置保存后由 SaveSettings 同步，无需重启）
	a.api = api.New(mgr)
	a.api.Sync()

	// 剪贴板监听
	go a.watchClipboard()

	a.initTray()
}

// GetTasks 返回全部任务（含实时进度）。
func (a *App) GetTasks() []engine.Task {
	if a.mgr == nil {
		return nil
	}
	return a.mgr.GetTasks()
}

// AddTask 新建下载任务；saveDir 为空时使用设置中的默认目录，
// connections <= 0 时使用默认连接数，customName 为空则自动从链接/响应头获取文件名。
func (a *App) AddTask(url string, saveDir string, connections int, customName string) (engine.Task, error) {
	if a.mgr == nil {
		return engine.Task{}, errNotReady
	}
	return a.mgr.AddTask(url, saveDir, connections, customName)
}

// AddTasks 批量新建下载任务；每条含可选校验和。
func (a *App) AddTasks(items []engine.AddTaskParams) engine.BatchAddResult {
	if a.mgr == nil {
		return engine.BatchAddResult{}
	}
	return a.mgr.AddTasks(items)
}

func (a *App) PauseTask(id string) error {
	if a.mgr == nil {
		return errNotReady
	}
	return a.mgr.PauseTask(id)
}

func (a *App) ResumeTask(id string) error {
	if a.mgr == nil {
		return errNotReady
	}
	return a.mgr.ResumeTask(id)
}

// PauseAll 暂停全部未结束的任务（排队中立即暂停，运行中的取消本轮），返回受影响任务数。
func (a *App) PauseAll() int {
	if a.mgr == nil {
		return 0
	}
	return a.mgr.PauseAll()
}

// ResumeAll 恢复全部已暂停与已失败的任务；队列总闸关闭时任务停在排队中。
func (a *App) ResumeAll() int {
	if a.mgr == nil {
		return 0
	}
	return a.mgr.ResumeAll()
}

// RetryAllFailed 仅重试失败任务，不动已暂停的任务。
func (a *App) RetryAllFailed() int {
	if a.mgr == nil {
		return 0
	}
	return a.mgr.RetryAllFailed()
}

// ClearCompleted 移除全部已完成任务的记录，磁盘上的文件保留。
func (a *App) ClearCompleted() int {
	if a.mgr == nil {
		return 0
	}
	return a.mgr.ClearCompleted()
}

// SetQueueSuspended 开关队列总闸：闭闸后不再启动任何任务，状态跨重启保持。
func (a *App) SetQueueSuspended(v bool) error {
	if a.mgr == nil {
		return errNotReady
	}
	return a.mgr.SetQueueSuspended(v)
}

// QueueStatus 队列放行状态：用户总闸、带宽时间窗、因磁盘空间不足停在队列里的任务数。
// 三者都会让任务停在「排队中」，界面据此区分文案。
func (a *App) QueueStatus() engine.QueueStatus {
	if a.mgr == nil {
		return engine.QueueStatus{}
	}
	return a.mgr.QueueStatus()
}

// SetQueueWindow 配置带宽时间窗：窗外不启动新任务（正在下载的不受影响），状态持久化。
func (a *App) SetQueueWindow(enabled bool, start, end string) error {
	if a.mgr == nil {
		return errNotReady
	}
	return a.mgr.SetQueueWindow(enabled, start, end)
}

// GetStats 返回下载统计与速度曲线（统计页轮询用）。
func (a *App) GetStats() engine.Stats {
	if a.mgr == nil {
		return engine.Stats{}
	}
	return a.mgr.GetStats()
}

// GetTaskStats 返回单任务曲线（统计页选中任务时按需拉取；GetStats 的
// 任务汇总不再携带 Samples，避免每次轮询全量序列化）。
func (a *App) GetTaskStats(id string) (engine.TaskStat, bool) {
	if a.mgr == nil {
		return engine.TaskStat{}, false
	}
	return a.mgr.GetTaskStats(id)
}

// SetTaskPriority 设置任务优先级（0 低 / 1 普通 / 2 高）。
func (a *App) SetTaskPriority(id string, priority int) error {
	if a.mgr == nil {
		return errNotReady
	}
	return a.mgr.SetTaskPriority(id, priority)
}

// MoveTask 列表内上下移动（-1 上移 / +1 下移）。
func (a *App) MoveTask(id string, delta int) error {
	if a.mgr == nil {
		return errNotReady
	}
	return a.mgr.MoveTask(id, delta)
}

// SetTaskSpeedLimit 设置每任务限速（字节/秒，0 跟随全局）。
func (a *App) SetTaskSpeedLimit(id string, limit int64) error {
	if a.mgr == nil {
		return errNotReady
	}
	return a.mgr.SetTaskSpeedLimit(id, limit)
}

// SetTaskStartAt 设置定时开始时间（RFC3339，空串表示立即）。
func (a *App) SetTaskStartAt(id string, startAt string) error {
	if a.mgr == nil {
		return errNotReady
	}
	return a.mgr.SetTaskStartAt(id, startAt)
}

// ExportTasksJSON 导出任务列表 JSON 字符串（前端触发下载）。
// withAuth 为 true 时才带出每任务的 Cookie/Referer——这两项常含会话凭据，默认剥离。
func (a *App) ExportTasksJSON(withAuth bool) (string, error) {
	if a.mgr == nil {
		return "", errNotReady
	}
	data, err := a.mgr.ExportTasksJSON(withAuth)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ImportTasksJSON 导入任务列表 JSON。
func (a *App) ImportTasksJSON(data string) engine.BatchAddResult {
	if a.mgr == nil {
		return engine.BatchAddResult{Errors: []string{errNotReady.Error()}}
	}
	res, err := a.mgr.ImportTasksJSON([]byte(data))
	if err != nil {
		return engine.BatchAddResult{Errors: []string{err.Error()}}
	}
	return res
}

// onQueueIdle 全部下载结束后按设置执行动作（通知回调，须锁外）。
func (a *App) onQueueIdle() {
	if a.mgr == nil {
		return
	}
	action := a.mgr.GetSettings().AfterComplete
	if action == engine.AfterCompleteNone || action == "" {
		return
	}
	openDir := a.mgr.GetSettings().SaveDir
	// 打开最后一个完成任务所在目录更实用
	for _, t := range a.mgr.GetTasks() {
		if t.Status == engine.StatusCompleted && t.SaveDir != "" {
			openDir = t.SaveDir
			break
		}
	}
	err := sysaction.Run(action, openDir)
	if errors.Is(err, sysaction.ErrExitApp) {
		a.requestQuit()
		return
	}
	if err != nil {
		runtime.LogWarningf(a.ctx, "完成后动作失败: %v", err)
	}
}

// RemoveTask 删除任务记录；deleteFiles 为 true 时连同已下载文件一起删除。
func (a *App) RemoveTask(id string, deleteFiles bool) error {
	if a.mgr == nil {
		return errNotReady
	}
	return a.mgr.RemoveTask(id, deleteFiles)
}

// GetSettings 返回全局设置。
func (a *App) GetSettings() engine.Settings {
	if a.mgr == nil {
		return engine.Settings{}
	}
	return a.mgr.GetSettings()
}

// SaveSettings 保存设置并同步通知开关缓存与 REST API 启停。
func (a *App) SaveSettings(s engine.Settings) error {
	if a.mgr == nil {
		return errNotReady
	}
	if err := a.mgr.SaveSettings(s); err != nil {
		return err
	}
	a.syncNotifyFlags()
	go a.api.Sync() // 异步：避免与在途请求互相等待
	return nil
}

// ShowWindow 显示并激活主窗口（托盘菜单使用）。
func (a *App) ShowWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

// OpenFolder 在文件管理器中定位任务文件；目标文件不存在时打开所在目录。
// 各平台的命令差异交给 sysaction，避免把 explorer 硬编码在这里
// （项目有 macOS 构建产物，explorer 在那里不存在）。
func (a *App) OpenFolder(saveDir string, fileName string) error {
	dir, err := filepath.Abs(saveDir)
	if err != nil {
		return err
	}
	return sysaction.RevealFile(dir, fileName)
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

// onBeforeClose 点击窗口关闭按钮时的统一入口。
// 按设置决定：每次询问（默认）、直接退出、最小化到系统托盘。
func (a *App) onBeforeClose(ctx context.Context) (prevent bool) {
	// 已在主动退出流程中（托盘退出 / 弹窗选直接退出），放行
	if a.quitting.Load() {
		return false
	}
	if a.mgr == nil {
		return false
	}
	switch a.mgr.GetSettings().CloseAction {
	case engine.CloseActionMinimize:
		runtime.WindowHide(ctx)
		return true
	case engine.CloseActionExit:
		return false
	default:
		// ask：交给前端弹窗，先阻止关闭
		runtime.EventsEmit(ctx, "app:confirm-close")
		return true
	}
}

// ApplyCloseAction 前端确认弹窗的回调。
// action: exit | minimize；remember 为 true 时写入设置，之后不再询问。
func (a *App) ApplyCloseAction(action string, remember bool) error {
	if a.mgr == nil {
		return errNotReady
	}
	if action != engine.CloseActionExit && action != engine.CloseActionMinimize {
		return errors.New("无效的关闭操作: " + action)
	}
	if remember {
		st := a.mgr.GetSettings()
		st.CloseAction = action
		if err := a.mgr.SaveSettings(st); err != nil {
			return err
		}
	}
	if action == engine.CloseActionMinimize {
		runtime.WindowHide(a.ctx)
		return nil
	}
	a.requestQuit()
	return nil
}

// Quit 退出整个程序（托盘菜单使用）。
func (a *App) Quit() {
	a.requestQuit()
}

// requestQuit 统一退出入口：置标志、摘托盘、通知 Wails 退出。
func (a *App) requestQuit() {
	if !a.quitting.CompareAndSwap(false, true) {
		return
	}
	quitTray()
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

// shutdown Wails 关闭回调：覆盖窗口关闭选「直接退出」等不经 requestQuit 的路径。
func (a *App) shutdown(ctx context.Context) {
	a.quitting.Store(true)
	quitTray()
	a.stopClipboard()
	// 停掉后台进度循环并强制落盘统计（平时 30s 节流），否则最后一段采样白丢
	if a.mgr != nil {
		_ = a.mgr.Close()
	}
}

// stopClipboard 停止剪贴板监听 goroutine（幂等）。
// 不关的话它会一直每 1.5s 轮询剪贴板，退出过程中还在调 Wails 运行时。
func (a *App) stopClipboard() {
	a.clipboardOnce.Do(func() { close(a.clipboardStop) })
}

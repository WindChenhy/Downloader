package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"downloader/engine"
	"downloader/engine/api"
	"downloader/sysaction"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// errNotReady 引擎尚未初始化。ServiceStartup 里数据目录不可用时应用会走退出流程，
// 但在真正退出之前前端仍可能调用绑定方法，这里统一挡住而不是让 nil 解引用崩掉。
var errNotReady = errors.New("应用尚未初始化")

// notifier 系统通知的平台后端（桌面 = Wails 通知服务，Android = 原生 Notify）。
// 抽象成接口让 notify.go 的通知分流逻辑跨平台复用。
type notifier interface {
	requestAuth()
	send(id, title, body string)
}

// AppService 暴露给前端的绑定服务，所有导出方法均可在 JS 中调用。
// 方法名与 v2 的 App 保持一致，前端 api.ts 仅需更换导入路径。
type AppService struct {
	app    *application.App
	window application.Window
	mgr    *engine.Manager
	api    *api.Server
	// notifier 平台通知后端；registerNotifications 按平台装配
	notifier notifier
	tray     *application.SystemTray
	// quitting 主动退出标志：为 true 时窗口关闭拦截不再阻止退出，
	// 否则托盘/弹窗选「退出」后 close 生命周期会再次进入询问逻辑，
	// 把窗口关掉却拦下进程退出，任务管理器里留下幽灵进程。
	quitting atomic.Bool
	// 通知开关缓存：notify 回调可能在引擎事件路径触发，禁止再抢 Manager 锁
	notifyCreate, notifyPause, notifyComplete, notifyFail atomic.Bool
	// uiLang 界面语言缓存（zh-CN / en-US），随设置同步；通知与托盘文案使用
	uiLang atomic.Value
	// clipboardStop 关闭后剪贴板监听退出；clipboardOnce 保证只关一次
	// （requestQuit 与 onShutdown 都可能走到收尾）
	clipboardStop chan struct{}
	clipboardOnce sync.Once
}

func NewAppService() *AppService { return &AppService{clipboardStop: make(chan struct{})} }

// attach 注入应用实例（Run 之前调用）；事件桥接依赖它。
func (s *AppService) attach(app *application.App) { s.app = app }

// setWindow 注入主窗口（托盘唤起与关闭拦截使用）。
func (s *AppService) setWindow(w *application.WebviewWindow) { s.window = w }

// ServiceStartup Wails 服务生命周期回调：初始化引擎、REST API、剪贴板与托盘。
func (s *AppService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	dataDir := appDataDir()
	stageRoot := stageRoot()

	mgr, err := engine.NewManager(dataDir,
		func(ev engine.Event) {
			// 事件载荷保持旧格式语义：列表事件发 []Task，其余发单个 Task；
			// 前端经 ev.data 取值
			data := any(ev.Task)
			if ev.Name == engine.EventTasksChanged {
				data = ev.Tasks
			}
			s.app.Event.Emit(string(ev.Name), data)
			switch ev.Name {
			case engine.EventTaskCreated:
				s.notifyTaskCreated(ev.Task)
			case engine.EventTaskPaused:
				s.notifyTaskPaused(ev.Task)
			case engine.EventTaskFinished:
				s.notifyTaskFinished(ev.Task)
			case engine.EventQueueIdle:
				s.onQueueIdle()
			}
		},
		engine.WithStagingDir(filepath.Join(stageRoot, "staging")))
	if err != nil {
		s.app.Dialog.Error().SetTitle("初始化失败").SetMessage("无法初始化下载数据目录：" + err.Error()).Show()
		os.Exit(1)
		return nil
	}
	s.mgr = mgr
	s.syncNotifyFlags()

	// Android 默认保存目录收敛到应用私有目录：engine 默认的 ~/Downloads
	// 在 Android 上不可用（无公共存储权限时 os.Create 会失败）
	if runtime.GOOS == "android" {
		st := mgr.GetSettings()
		if st.SaveDir == "" || !filepath.IsAbs(st.SaveDir) {
			st.SaveDir = filepath.Join(dataDir, "downloads")
			_ = mgr.SaveSettings(st)
		}
	}

	// 通知权限（macOS 上需要；其余平台为 no-op）
	if s.notifier != nil {
		s.notifier.requestAuth()
	}

	// 本地 REST API（按设置启停；设置保存后由 SaveSettings 同步，无需重启）
	s.api = api.New(mgr)
	s.api.Sync()

	// 剪贴板监听（桌面专属；Android 上无常驻剪贴板读取权限模型，不启用）
	s.startClipboardWatch()

	s.initTray()

	// 命令行直启带下载链接：首个实例直接建任务
	// （应用已在运行时走 onSecondInstanceLaunch，两条路径不会重叠）
	for _, arg := range os.Args[1:] {
		if engine.IsDownloadableURL(arg) {
			if _, err := s.mgr.AddTask(arg, "", 0, ""); err != nil {
				s.app.Logger.Warn("通过命令行创建任务失败", "error", err)
			}
		}
	}
	return nil
}

// ServiceShutdown Wails 关闭回调：覆盖「直接退出」等不经 requestQuit 的路径。
func (s *AppService) ServiceShutdown() error {
	s.quitting.Store(true)
	s.quitTray()
	s.stopClipboard()
	// 停掉后台进度循环并强制落盘统计（平时 30s 节流），否则最后一段采样白丢
	if s.mgr != nil {
		_ = s.mgr.Close()
	}
	return nil
}

// GetTasks 返回全部任务（含实时进度）。
func (s *AppService) GetTasks() []engine.Task {
	if s.mgr == nil {
		return nil
	}
	return s.mgr.GetTasks()
}

// AddTask 新建下载任务；saveDir 为空时使用设置中的默认目录，
// connections <= 0 时使用默认连接数，customName 为空则自动从链接/响应头获取文件名。
func (s *AppService) AddTask(url string, saveDir string, connections int, customName string) (engine.Task, error) {
	if s.mgr == nil {
		return engine.Task{}, errNotReady
	}
	return s.mgr.AddTask(url, saveDir, connections, customName)
}

// AddTasks 批量新建下载任务；每条含可选校验和。
func (s *AppService) AddTasks(items []engine.AddTaskParams) engine.BatchAddResult {
	if s.mgr == nil {
		return engine.BatchAddResult{}
	}
	return s.mgr.AddTasks(items)
}

func (s *AppService) PauseTask(id string) error {
	if s.mgr == nil {
		return errNotReady
	}
	return s.mgr.PauseTask(id)
}

func (s *AppService) ResumeTask(id string) error {
	if s.mgr == nil {
		return errNotReady
	}
	return s.mgr.ResumeTask(id)
}

// PauseAll 暂停全部未结束的任务（排队中立即暂停，运行中的取消本轮），返回受影响任务数。
func (s *AppService) PauseAll() int {
	if s.mgr == nil {
		return 0
	}
	return s.mgr.PauseAll()
}

// ResumeAll 恢复全部已暂停与已失败的任务；队列总闸关闭时任务停在排队中。
func (s *AppService) ResumeAll() int {
	if s.mgr == nil {
		return 0
	}
	return s.mgr.ResumeAll()
}

// RetryAllFailed 仅重试失败任务，不动已暂停的任务。
func (s *AppService) RetryAllFailed() int {
	if s.mgr == nil {
		return 0
	}
	return s.mgr.RetryAllFailed()
}

// ClearCompleted 移除全部已完成任务的记录，磁盘上的文件保留。
func (s *AppService) ClearCompleted() int {
	if s.mgr == nil {
		return 0
	}
	return s.mgr.ClearCompleted()
}

// SetQueueSuspended 开关队列总闸：闭闸后不再启动任何任务，状态跨重启保持。
func (s *AppService) SetQueueSuspended(v bool) error {
	if s.mgr == nil {
		return errNotReady
	}
	return s.mgr.SetQueueSuspended(v)
}

// QueueStatus 队列放行状态：用户总闸、带宽时间窗、因磁盘空间不足停在队列里的任务数。
// 三者都会让任务停在「排队中」，界面据此区分文案。
func (s *AppService) QueueStatus() engine.QueueStatus {
	if s.mgr == nil {
		return engine.QueueStatus{}
	}
	return s.mgr.QueueStatus()
}

// SetQueueWindow 配置带宽时间窗：窗外不启动新任务（正在下载的不受影响），状态持久化。
func (s *AppService) SetQueueWindow(enabled bool, start, end string) error {
	if s.mgr == nil {
		return errNotReady
	}
	return s.mgr.SetQueueWindow(enabled, start, end)
}

// GetStats 返回下载统计与速度曲线（统计页轮询用）。
func (s *AppService) GetStats() engine.Stats {
	if s.mgr == nil {
		return engine.Stats{}
	}
	return s.mgr.GetStats()
}

// GetTaskStats 返回单任务曲线（统计页选中任务时按需拉取；GetStats 的
// 任务汇总不再携带 Samples，避免每次轮询全量序列化）。
func (s *AppService) GetTaskStats(id string) (engine.TaskStat, bool) {
	if s.mgr == nil {
		return engine.TaskStat{}, false
	}
	return s.mgr.GetTaskStats(id)
}

// SetTaskPriority 设置任务优先级（0 低 / 1 普通 / 2 高）。
func (s *AppService) SetTaskPriority(id string, priority int) error {
	if s.mgr == nil {
		return errNotReady
	}
	return s.mgr.SetTaskPriority(id, priority)
}

// MoveTask 列表内上下移动（-1 上移 / +1 下移）。
func (s *AppService) MoveTask(id string, delta int) error {
	if s.mgr == nil {
		return errNotReady
	}
	return s.mgr.MoveTask(id, delta)
}

// SetTaskSpeedLimit 设置每任务限速（字节/秒，0 跟随全局）。
func (s *AppService) SetTaskSpeedLimit(id string, limit int64) error {
	if s.mgr == nil {
		return errNotReady
	}
	return s.mgr.SetTaskSpeedLimit(id, limit)
}

// SetTaskStartAt 设置定时开始时间（RFC3339，空串表示立即）。
func (s *AppService) SetTaskStartAt(id string, startAt string) error {
	if s.mgr == nil {
		return errNotReady
	}
	return s.mgr.SetTaskStartAt(id, startAt)
}

// ExportTasksJSON 导出任务列表 JSON 字符串（前端触发下载）。
// withAuth 为 true 时才带出每任务的 Cookie/Referer——这两项常含会话凭据，默认剥离。
func (s *AppService) ExportTasksJSON(withAuth bool) (string, error) {
	if s.mgr == nil {
		return "", errNotReady
	}
	data, err := s.mgr.ExportTasksJSON(withAuth)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ImportTasksJSON 导入任务列表 JSON。
func (s *AppService) ImportTasksJSON(data string) engine.BatchAddResult {
	if s.mgr == nil {
		return engine.BatchAddResult{Errors: []string{errNotReady.Error()}}
	}
	res, err := s.mgr.ImportTasksJSON([]byte(data))
	if err != nil {
		return engine.BatchAddResult{Errors: []string{err.Error()}}
	}
	return res
}

// onQueueIdle 全部下载结束后按设置执行动作（通知回调，须锁外）。
func (s *AppService) onQueueIdle() {
	if s.mgr == nil {
		return
	}
	action := s.mgr.GetSettings().AfterComplete
	if action == engine.AfterCompleteNone || action == "" {
		return
	}
	openDir := s.mgr.GetSettings().SaveDir
	// 打开最后一个完成任务所在目录更实用
	for _, t := range s.mgr.GetTasks() {
		if t.Status == engine.StatusCompleted && t.SaveDir != "" {
			openDir = t.SaveDir
			break
		}
	}
	err := sysaction.Run(action, openDir)
	if errors.Is(err, sysaction.ErrExitApp) {
		s.requestQuit()
		return
	}
	if err != nil {
		s.app.Logger.Warn("完成后动作失败", "error", err)
	}
}

// RemoveTask 删除任务记录；deleteFiles 为 true 时连同已下载文件一起删除。
func (s *AppService) RemoveTask(id string, deleteFiles bool) error {
	if s.mgr == nil {
		return errNotReady
	}
	return s.mgr.RemoveTask(id, deleteFiles)
}

// GetSettings 返回全局设置。
func (s *AppService) GetSettings() engine.Settings {
	if s.mgr == nil {
		return engine.Settings{}
	}
	return s.mgr.GetSettings()
}

// SaveSettings 保存设置并同步通知开关缓存与 REST API 启停。
func (s *AppService) SaveSettings(st engine.Settings) error {
	if s.mgr == nil {
		return errNotReady
	}
	if err := s.mgr.SaveSettings(st); err != nil {
		return err
	}
	s.syncNotifyFlags()
	go s.api.Sync() // 异步：避免与在途请求互相等待
	return nil
}

// OpenFolder 在文件管理器中定位任务文件；目标文件不存在时打开所在目录。
// 各平台的命令差异交给 sysaction，避免把 explorer 硬编码在这里
// （项目有 macOS 构建产物，explorer 在那里不存在）。
func (s *AppService) OpenFolder(saveDir string, fileName string) error {
	dir, err := filepath.Abs(saveDir)
	if err != nil {
		return err
	}
	return sysaction.RevealFile(dir, fileName)
}

// ShowWindow 显示并激活主窗口（托盘菜单使用）；实现按平台拆分。
func (s *AppService) ShowWindow() { s.showWindow() }

// onSecondInstanceLaunch 用户再次双击 exe 时被首个实例调用：
// 唤起主窗口；若第二个实例携带了下载链接参数，则自动创建任务。
func (s *AppService) onSecondInstanceLaunch(data application.SecondInstanceData) {
	if s.mgr == nil {
		return
	}
	s.ShowWindow()
	for _, arg := range data.Args {
		if engine.IsDownloadableURL(arg) {
			if _, err := s.mgr.AddTask(arg, "", 0, ""); err != nil {
				s.app.Logger.Warn("通过命令行创建任务失败", "error", err)
			}
		}
	}
}

// onWindowClosing 点击窗口关闭按钮时的统一入口（仅桌面注册）。
// 按设置决定：每次询问（默认）、直接退出、最小化到系统托盘。
func (s *AppService) onWindowClosing(ev *application.WindowEvent) {
	// 已在主动退出流程中（托盘退出 / 弹窗选直接退出），放行
	if s.quitting.Load() {
		return
	}
	if s.mgr == nil {
		return
	}
	switch s.mgr.GetSettings().CloseAction {
	case engine.CloseActionMinimize:
		s.window.Hide()
		ev.Cancel()
	case engine.CloseActionExit:
		// 放行，走正常关闭 → ServiceShutdown
	default:
		// ask：交给前端弹窗，先阻止关闭
		s.app.Event.Emit("app:confirm-close")
		ev.Cancel()
	}
}

// ApplyCloseAction 前端确认弹窗的回调。
// action: exit | minimize；remember 为 true 时写入设置，之后不再询问。
func (s *AppService) ApplyCloseAction(action string, remember bool) error {
	if s.mgr == nil {
		return errNotReady
	}
	if action != engine.CloseActionExit && action != engine.CloseActionMinimize {
		return errors.New("无效的关闭操作: " + action)
	}
	if remember {
		st := s.mgr.GetSettings()
		st.CloseAction = action
		if err := s.mgr.SaveSettings(st); err != nil {
			return err
		}
	}
	if action == engine.CloseActionMinimize {
		if s.window != nil {
			s.window.Hide()
		}
		return nil
	}
	s.requestQuit()
	return nil
}

// Quit 退出整个程序（托盘菜单使用）。
func (s *AppService) Quit() {
	s.requestQuit()
}

// requestQuit 统一退出入口：置标志、摘托盘、通知 Wails 退出。
func (s *AppService) requestQuit() {
	if !s.quitting.CompareAndSwap(false, true) {
		return
	}
	s.quitTray()
	if s.app != nil {
		s.app.Quit()
	}
}

// stopClipboard 停止剪贴板监听 goroutine（幂等）。
// 不关的话它会一直每 1.5s 轮询剪贴板，退出过程中还在调运行时。
func (s *AppService) stopClipboard() {
	s.clipboardOnce.Do(func() { close(s.clipboardStop) })
}

// onShutdown 挂在 app.OnShutdown 上，兜底不经 ServiceShutdown 的退出路径。
func (s *AppService) onShutdown() {
	_ = s.ServiceShutdown()
}

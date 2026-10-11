package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// taskHandle 一个任务的运行期状态（Task 本体 + 当前运行的控制柄）。
type taskHandle struct {
	task         Task
	cancel       context.CancelFunc
	done         chan struct{} // 本轮运行的 goroutine 退出后关闭
	live         atomic.Int64  // 本轮运行已写入字节数
	base         int64         // 本轮起点（续传时为已完成字节数），mu 保护
	prev         int64         // 速度计算的上一次快照，mu 保护
	prevAt       time.Time
	runStartedAt time.Time    // 本轮运行开始时刻；零值表示当前未在下载，mu 保护
	limiter      *rateLimiter // 本轮运行的每任务限速器；mu 保护，非运行期为 nil
}

// runningView 填充运行中任务的实时进度快照（展示用）。
// settleSpeed 为 true 时同时结算瞬时速度并推进采样点（仅进度循环使用，
// 快照路径不能推进，否则速度恒为 0）。调用方须持有 mu。
func (h *taskHandle) runningView(now time.Time, settleSpeed bool) Task {
	t := h.task
	d := h.base + h.live.Load()
	if t.TotalSize > 0 && d > t.TotalSize {
		d = t.TotalSize
	}
	if settleSpeed {
		if dt := now.Sub(h.prevAt).Seconds(); dt > 0 {
			sp := int64(float64(d-h.prev) / dt)
			if sp < 0 {
				sp = 0
			}
			t.Speed = sp
		}
		h.prev = d
		h.prevAt = now
	}
	t.Downloaded = d
	// 展示用：把本轮尚未结算的活跃时长并入快照
	if !h.runStartedAt.IsZero() {
		t.ActiveMs += now.Sub(h.runStartedAt).Milliseconds()
	}
	t.AvgSpeed = avgSpeedOf(t.Downloaded, t.ActiveMs)
	return t
}

// Manager 管理全部下载任务：排队、并发控制、持久化与事件通知。
// 所有公开方法并发安全。
type Manager struct {
	store  *Store
	client *http.Client
	notify func(Event)
	// now 时间源；测试可经 WithClock 注入假时钟
	now func() time.Time

	stagingRoot string // 下载暂存目录（分块 .part 所在），运行期只读

	mu       sync.Mutex
	settings Settings
	handles  map[string]*taskHandle
	order    []string // 按创建顺序，保持列表稳定
	running  int
	limiter  *rateLimiter
	// queueIdleFired 队列清空后只触发一次「全部完成」动作，新任务会重置
	queueIdleFired bool
}

// Option 定制 Manager 的可选依赖。
type Option func(*Manager)

// WithHTTPClient 注入自定义 HTTP 客户端（测试时替换为 httptest/fake 用）。
// 为 nil 时忽略；保存设置后仍会按设置重建客户端。
func WithHTTPClient(c *http.Client) Option {
	return func(m *Manager) {
		if c != nil {
			m.client = c
		}
	}
}

// WithClock 注入时间源（测试用假时钟）；为 nil 时忽略。
// 影响调度判定、进度采样与各时间戳；不影响限速器与 progressLoop 的 tick 频率。
func WithClock(now func() time.Time) Option {
	return func(m *Manager) {
		if now != nil {
			m.now = now
		}
	}
}

// WithStagingDir 覆盖下载暂存目录。默认 <数据目录>/staging；应用层传入
// 系统本地应用数据目录（Windows = %LOCALAPPDATA%\downloader\staging），
// 使下载中的分块文件不出现在下载目录里，完成时才移动过去。
func WithStagingDir(dir string) Option {
	return func(m *Manager) {
		if strings.TrimSpace(dir) != "" {
			m.stagingRoot = dir
		}
	}
}

// NewManager 创建管理器并加载磁盘上的任务与设置。
// 上次退出时仍在运行的任务一律视为暂停；排队中的任务自动继续排队。
// 可通过 Option 注入依赖（如 WithHTTPClient / WithClock）。
func NewManager(dataDir string, notify func(Event), opts ...Option) (*Manager, error) {
	store := NewStore(dataDir)
	settings, err := store.LoadSettings()
	if err != nil {
		return nil, err
	}
	m := &Manager{
		store:       store,
		client:      buildHTTPClient(settings),
		notify:      notify,
		now:         time.Now,
		settings:    settings,
		handles:     make(map[string]*taskHandle),
		limiter:     newRateLimiter(settings.SpeedLimit),
		stagingRoot: filepath.Join(dataDir, "staging"),
	}
	for _, opt := range opts {
		opt(m)
	}
	tasks, err := store.LoadTasks()
	if err != nil {
		return nil, err
	}
	now := m.now()
	for _, t := range tasks {
		if t.Status == StatusRunning {
			t.Status = StatusPaused
			t.Speed = 0
		}
		m.handles[t.ID] = &taskHandle{task: t, done: make(chan struct{}), prevAt: now}
		m.order = append(m.order, t.ID)
	}
	m.mu.Lock()
	m.persistTasksLocked()
	m.cleanupOrphanStagingLocked()
	m.dispatchLocked()
	m.mu.Unlock()
	// API 开启时确保存在访问令牌（首次运行生成并持久化）
	if m.settings.APIEnabled && strings.TrimSpace(m.settings.APIToken) == "" {
		m.settings.APIToken = newID()
		if err := m.store.SaveSettings(m.settings); err != nil {
			fmt.Fprintln(os.Stderr, "保存 API 令牌失败:", err)
		}
	}
	go m.progressLoop()
	return m, nil
}

// emitTasks 推送任务列表快照事件。
func (m *Manager) emitTasks(tasks []Task) {
	m.notify(Event{Name: EventTasksChanged, Tasks: tasks})
}

// emitTask 推送单任务事件。
func (m *Manager) emitTask(name EventName, t Task) {
	m.notify(Event{Name: name, Task: t})
}

// AddTaskParams 新建任务参数；批量与单条共用。
type AddTaskParams struct {
	URL              string `json:"url"`
	SaveDir          string `json:"saveDir"`
	Connections      int    `json:"connections"`
	CustomName       string `json:"customName"`
	ChecksumAlgo     string `json:"checksumAlgo"`
	ChecksumExpected string `json:"checksumExpected"`
	Priority         int    `json:"priority"`   // 0 低 / 1 普通 / 2 高；非法值回落为普通
	StartAt          string `json:"startAt"`    // RFC3339；空 = 立即
	SpeedLimit       int64  `json:"speedLimit"` // 每任务限速，0 跟随全局
}

// BatchAddResult 批量新建结果：成功任务列表 + 失败说明（与输入顺序对应可不保证）。
type BatchAddResult struct {
	Tasks  []Task   `json:"tasks"`
	Errors []string `json:"errors"`
}

// AddTask 新建单个下载任务（不含校验和/优先级等扩展字段）。
func (m *Manager) AddTask(rawURL, saveDir string, connections int, customName string) (Task, error) {
	return m.AddTaskWithChecksum(AddTaskParams{
		URL:         rawURL,
		SaveDir:     saveDir,
		Connections: connections,
		CustomName:  customName,
	})
}

// AddTaskWithChecksum 按完整参数新建任务；事件在锁外发出。
func (m *Manager) AddTaskWithChecksum(p AddTaskParams) (Task, error) {
	rawURL := p.URL
	u, err := validateURL(rawURL)
	if err != nil {
		return Task{}, err
	}
	m.mu.Lock()
	t, err := m.addTaskLocked(u, p)
	var snap []Task
	if err == nil {
		snap = m.snapshotLocked()
	}
	m.mu.Unlock()
	if err != nil {
		return Task{}, err
	}
	// 事件必须在解锁后发出：回调可能再次进入 Manager（如 GetSettings）
	m.emitTasks(snap)
	m.emitTask(EventTaskCreated, t)
	return t, nil
}

// AddTasks 批量新建；逐条校验，失败不影响已成功项。
func (m *Manager) AddTasks(items []AddTaskParams) BatchAddResult {
	var out BatchAddResult
	for i, p := range items {
		t, err := m.AddTaskWithChecksum(p)
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("第 %d 条（%s）: %v", i+1, truncateURL(p.URL), err))
			continue
		}
		out.Tasks = append(out.Tasks, t)
	}
	return out
}

func truncateURL(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 48 {
		return s[:45] + "..."
	}
	if s == "" {
		return "(空)"
	}
	return s
}

func (m *Manager) addTaskLocked(u *url.URL, p AddTaskParams) (Task, error) {
	rawURL := p.URL
	saveDir := p.SaveDir
	connections := p.Connections
	customName := p.CustomName
	if strings.TrimSpace(saveDir) == "" {
		saveDir = m.settings.SaveDir
	}
	// 目录分类若指向子目录则先确保存在（创建失败时仍尝试下载，由后续写入报错）
	if abs, err := filepath.Abs(saveDir); err == nil {
		_ = os.MkdirAll(abs, 0o755)
		saveDir = abs
	}
	abs, err := filepath.Abs(saveDir)
	if err != nil {
		return Task{}, err
	}
	if connections <= 0 {
		connections = m.settings.Connections
	}
	if connections > MaxConnections {
		connections = MaxConnections
	}
	// 自定义文件名：留空则任务先行用 URL 推断的名字占位，
	// 运行时优先级为 自定义名 > Content-Disposition > URL 路径
	custom := sanitizeFileName(strings.TrimSpace(customName))
	name := custom
	if name == "" {
		name = fileNameFromURL(u)
	}
	if name == "" {
		name = "download"
	}
	algo := normalizeChecksumAlgo(p.ChecksumAlgo, p.ChecksumExpected)
	prio := p.Priority
	if prio < PriorityLow {
		prio = PriorityNormal
	}
	if prio > PriorityHigh {
		prio = PriorityHigh
	}
	var startAt time.Time
	if s := strings.TrimSpace(p.StartAt); s != "" {
		if ts, err := time.Parse(time.RFC3339, s); err == nil {
			startAt = ts
		}
	}
	// 定时未到点仍为 queued：dispatchLocked 会按 StartAt 跳过
	t := Task{
		ID:               newID(),
		URL:              rawURL,
		FileName:         name,
		CustomName:       custom,
		SaveDir:          abs,
		Status:           StatusQueued,
		Connections:      connections,
		CreatedAt:        m.now(),
		ChecksumAlgo:     algo,
		ChecksumExpected: strings.ToLower(strings.TrimSpace(p.ChecksumExpected)),
		Priority:         prio,
		StartAt:          startAt,
		SpeedLimit:       p.SpeedLimit,
	}
	m.handles[t.ID] = &taskHandle{task: t, done: make(chan struct{})}
	m.order = append(m.order, t.ID)
	m.queueIdleFired = false
	m.dispatchLocked()
	m.persistTasksLocked()
	return t, nil
}

// PauseTask 暂停任务。排队中立即改状态；运行中则取消本轮下载。
func (m *Manager) PauseTask(id string) error {
	m.mu.Lock()
	h, ok := m.handles[id]
	if !ok {
		m.mu.Unlock()
		return ErrTaskNotFound
	}
	var (
		t           Task
		notifySnap  []Task
		notifyPause bool
	)
	switch h.task.Status {
	case StatusQueued:
		h.task.Status = StatusPaused
		t = h.task
		notifyPause = true
		m.persistTasksLocked()
		notifySnap = m.snapshotLocked()
	case StatusRunning:
		if h.cancel != nil {
			h.cancel() // run goroutine 退出后由 finishTask 置为 Paused 并通知
		}
	}
	m.mu.Unlock()
	if notifyPause {
		m.emitTasks(notifySnap)
		m.emitTask(EventTaskPaused, t)
	}
	return nil
}

// ResumeTask 恢复已暂停/失败的任务（重新入队）。
func (m *Manager) ResumeTask(id string) error {
	m.mu.Lock()
	h, ok := m.handles[id]
	if !ok {
		m.mu.Unlock()
		return ErrTaskNotFound
	}
	if h.task.Status != StatusPaused && h.task.Status != StatusFailed {
		status := h.task.Status
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrInvalidState, status)
	}
	h.task.Status = StatusQueued
	h.task.Error = ""
	m.dispatchLocked()
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.emitTasks(snap)
	return nil
}

// SetTaskPriority 设置任务优先级（0/1/2）。
func (m *Manager) SetTaskPriority(id string, priority int) error {
	if priority < PriorityLow || priority > PriorityHigh {
		return fmt.Errorf("无效优先级: %d", priority)
	}
	m.mu.Lock()
	h, ok := m.handles[id]
	if !ok {
		m.mu.Unlock()
		return ErrTaskNotFound
	}
	h.task.Priority = priority
	m.dispatchLocked()
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.emitTasks(snap)
	return nil
}

// MoveTask 在列表中上下移动任务（-1 上移 / +1 下移）。
func (m *Manager) MoveTask(id string, delta int) error {
	if delta != -1 && delta != 1 {
		return fmt.Errorf("delta 只能是 -1 或 1")
	}
	m.mu.Lock()
	idx := -1
	for i, oid := range m.order {
		if oid == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.mu.Unlock()
		return ErrTaskNotFound
	}
	j := idx + delta
	if j < 0 || j >= len(m.order) {
		m.mu.Unlock()
		return nil // 已在边界，静默成功
	}
	m.order[idx], m.order[j] = m.order[j], m.order[idx]
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.emitTasks(snap)
	return nil
}

// SetTaskSpeedLimit 设置每任务限速（字节/秒，0 跟随全局）。
// 对运行中的任务立即生效（调整其限速器），对排队任务在下一轮运行生效。
func (m *Manager) SetTaskSpeedLimit(id string, limit int64) error {
	if limit < 0 {
		limit = 0
	}
	m.mu.Lock()
	h, ok := m.handles[id]
	if !ok {
		m.mu.Unlock()
		return ErrTaskNotFound
	}
	h.task.SpeedLimit = limit
	if h.limiter != nil {
		h.limiter.SetLimit(limit) // 0 = 不限，运行中的下载立即放开
	}
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.emitTasks(snap)
	return nil
}

// SetTaskStartAt 设置定时开始（RFC3339；空字符串表示立即）。
func (m *Manager) SetTaskStartAt(id string, startAtRFC3339 string) error {
	var ts time.Time
	if s := strings.TrimSpace(startAtRFC3339); s != "" {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return fmt.Errorf("无效时间: %w", err)
		}
		ts = parsed
	}
	m.mu.Lock()
	h, ok := m.handles[id]
	if !ok {
		m.mu.Unlock()
		return ErrTaskNotFound
	}
	h.task.StartAt = ts
	m.dispatchLocked()
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.emitTasks(snap)
	return nil
}

// RemoveTask 删除任务记录。deleteFiles 为 false 时仅删除记录与未完成的
// .part 数据，已下载完成的正式文件保留在磁盘上；为 true 时连同正式文件一起删除。
func (m *Manager) RemoveTask(id string, deleteFiles bool) error {
	m.mu.Lock()
	h, ok := m.handles[id]
	if !ok {
		m.mu.Unlock()
		return ErrTaskNotFound
	}
	running := h.task.Status == StatusRunning
	if h.cancel != nil {
		h.cancel()
	}
	if running {
		// 锁内捕获本轮 done：等待期间任务可能被 ResumeTask 重新出队并
		// 换上新的 done，拿到旧引用才能确认被删除的这一轮已经退出
		done := h.done
		m.mu.Unlock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		m.mu.Lock()
	}
	t := h.task
	delete(m.handles, id)
	for i, oid := range m.order {
		if oid == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.emitTasks(snap)

	// 未完成的暂存数据（<暂存目录>/<任务ID>.part）随记录一起清理；
	// 正式文件是否删除由 deleteFiles 决定
	_ = os.Remove(m.stagingPath(t.ID))
	if deleteFiles {
		_ = os.Remove(filepath.Join(t.SaveDir, t.FileName))
	}
	_ = os.Remove(m.store.StatePath(id))
	_ = os.Remove(m.stagingDir()) // 暂存目录已空则一并移除
	return nil
}

// GetTasks 返回全部任务快照（含运行中实时进度与活跃耗时）。
func (m *Manager) GetTasks() []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

// GetSettings 返回当前全局设置快照。
func (m *Manager) GetSettings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

// SaveSettings 持久化并应用设置。
// 即时生效：代理/UA/自定义请求头/镜像/全局限速/通知开关，以及本地 REST API 的启停与端口。
// 对下一轮任务生效：连接数、每任务限速（运行中的任务沿用其开始时的快照）。
func (m *Manager) SaveSettings(s Settings) error {
	s.normalize()
	m.mu.Lock()
	// 客户端未回传令牌时保留现有值：令牌由应用生成管理，不随表单清空
	if strings.TrimSpace(s.APIToken) == "" {
		s.APIToken = m.settings.APIToken
	}
	// 运行中开启 API 且从未生成过令牌（首启时 API 关闭不会生成）→ 现在补发
	if s.APIEnabled && strings.TrimSpace(s.APIToken) == "" {
		s.APIToken = newID()
	}
	m.mu.Unlock()
	if err := m.store.SaveSettings(s); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = s
	m.client = buildHTTPClient(s) // 代理/UA 等即时生效
	m.limiter.SetLimit(s.SpeedLimit)
	m.mu.Unlock()
	return nil
}

// httpClient 返回当前客户端（保存设置后会被重建，须加锁读取）。
func (m *Manager) httpClient() *http.Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.client
}

// HasTaskWithURL 报告是否已存在相同 URL 的任务（剪贴板去重用）。
func (m *Manager) HasTaskWithURL(rawURL string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, h := range m.handles {
		if h.task.URL == rawURL {
			return true
		}
	}
	return false
}

// dispatchLocked 启动排队任务直到占满并发额度。调用方须持有 mu。
// 出队顺序：优先级高者先，同级按创建顺序；未到 StartAt 的定时任务跳过。
func (m *Manager) dispatchLocked() {
	now := m.now()
	for m.running < m.settings.ConcurrentTasks {
		var nextID string
		bestPrio := -1
		for _, id := range m.order {
			h := m.handles[id]
			if h.task.Status != StatusQueued {
				continue
			}
			if !h.task.StartAt.IsZero() && h.task.StartAt.After(now) {
				continue // 定时未到点
			}
			if h.task.Priority > bestPrio {
				bestPrio = h.task.Priority
				nextID = id
			}
		}
		if nextID == "" {
			return
		}
		h := m.handles[nextID]
		h.task.Status = StatusRunning
		h.task.Error = ""
		// 基准先取任务已记录的进度：probe 期间界面不闪回 0，
		// runner 探测完成后会用续传状态里的精确值覆盖
		h.base = h.task.Downloaded
		h.live.Store(0)
		h.prev = h.task.Downloaded
		h.prevAt = now
		h.runStartedAt = now // 活跃下载耗时从本轮开始累计
		// 每任务限速器按本轮开始的值创建；运行中可用 SetTaskSpeedLimit 调整
		h.limiter = newRateLimiter(h.task.SpeedLimit)
		m.running++

		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		h.done = make(chan struct{}) // 每轮运行一个新 done，上一轮的已被关闭
		r := &taskRunner{m: m, h: h, task: h.task, cancel: cancel, taskLimiter: h.limiter}
		go func() {
			err := r.run(ctx)
			// 先关闭 done 再做收尾登记：ResumeTask 在看到 Paused 状态后
			// 可能立刻重建 h.done，必须发生在本轮 close 读取之后
			close(h.done)
			m.finishTask(h, err)
		}()
	}
}

// finishTask 收尾一轮运行：更新状态、持久化、通知、调度下一个任务。
func (m *Manager) finishTask(h *taskHandle, err error) {
	m.mu.Lock()
	m.running--
	h.cancel = nil
	h.limiter = nil // 本轮限速器随运行结束作废
	h.task.Speed = 0
	// 结算本轮活跃下载耗时（暂停前一直在下的时间）
	if !h.runStartedAt.IsZero() {
		h.task.ActiveMs += m.now().Sub(h.runStartedAt).Milliseconds()
		h.runStartedAt = time.Time{}
	}

	switch {
	case err == nil:
		h.task.Status = StatusCompleted
		h.task.Error = ""
		if h.task.TotalSize > 0 {
			h.task.Downloaded = h.task.TotalSize
		}
		h.task.FinishedAt = m.now()
		h.task.AvgSpeed = avgSpeedOf(h.task.Downloaded, h.task.ActiveMs)
		_ = os.Remove(m.store.StatePath(h.task.ID))
	case errors.Is(err, ErrPaused):
		h.task.Status = StatusPaused
		h.task.Error = ""
		h.task.Downloaded = m.exactDownloadedLocked(h)
		h.task.AvgSpeed = avgSpeedOf(h.task.Downloaded, h.task.ActiveMs)
	default:
		h.task.Status = StatusFailed
		h.task.Error = err.Error()
		h.task.Downloaded = m.exactDownloadedLocked(h)
		h.task.FinishedAt = m.now()
		h.task.AvgSpeed = avgSpeedOf(h.task.Downloaded, h.task.ActiveMs)
	}
	t := h.task
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	status := h.task.Status
	idle := m.queueIdleLocked()
	m.dispatchLocked()
	m.mu.Unlock()
	m.emitTasks(snap)
	if status == StatusPaused {
		m.emitTask(EventTaskPaused, t)
	} else {
		m.emitTask(EventTaskFinished, t) // 供系统通知等上层钩子使用（完成/失败）
	}
	if idle {
		m.emitTask(EventQueueIdle, t)
	}
}

// queueIdleLocked 检测本轮结束后是否已无排队/运行任务，并保证只通知一次。
// 调用方须持有 mu。
func (m *Manager) queueIdleLocked() bool {
	if m.running > 0 {
		return false
	}
	for _, h := range m.handles {
		if h.task.Status == StatusRunning || h.task.Status == StatusQueued {
			return false // 仍有人在跑，或还有排队/定时待启动
		}
	}
	if m.queueIdleFired || len(m.handles) == 0 {
		return false
	}
	m.queueIdleFired = true
	return true
}

// stagingDir 下载暂存目录：应用数据目录下的 staging（应用层默认指向
// 系统本地应用数据，Windows = %LOCALAPPDATA%\downloader\staging）。
// 下载中的分块文件不再出现在保存目录里；完成时才移动进保存目录，
// 同盘保存目录时仍是原地改名，跨盘则退化为一次性复制（见 download.go moveFile）。
func (m *Manager) stagingDir() string {
	return m.stagingRoot
}

func (m *Manager) stagingPath(taskID string) string {
	return filepath.Join(m.stagingDir(), taskID+".part")
}

// exactDownloadedLocked 从磁盘上的续传状态精确计算已下载字节数
// （进度快照中的 base+live 含被丢弃的分段局部重试字节，仅用于展示）。
func (m *Manager) exactDownloadedLocked(h *taskHandle) int64 {
	if sc, err := loadSidecar(m.store.StatePath(h.task.ID)); err == nil {
		if sc.Single {
			if st, err := os.Stat(m.stagingPath(h.task.ID)); err == nil {
				return st.Size()
			}
			return 0
		}
		return sc.ProgressBytes()
	}
	return h.base + h.live.Load()
}

// cleanupOrphanStagingLocked 启动时的一次性暂存整理：
//  1. 把旧版本遗留在保存目录的暂存数据迁入统一暂存目录
//     （v0：<保存目录>/<文件名>.part；v1：<保存目录>/.downloader/<任务ID>.part）
//  2. 移除保存目录里的旧暂存目录
//  3. 清理统一暂存目录中不再属于任何现存任务的孤儿 part 文件
//
// 调用方须持有 mu；仅在启动时执行一次。
func (m *Manager) cleanupOrphanStagingLocked() {
	_ = os.MkdirAll(m.stagingDir(), 0o755)
	for _, h := range m.handles {
		v0 := filepath.Join(h.task.SaveDir, h.task.FileName+".part")
		if _, err := os.Stat(v0); err == nil {
			_ = moveFile(v0, m.stagingPath(h.task.ID))
		}
		v1 := filepath.Join(h.task.SaveDir, ".downloader", h.task.ID+".part")
		if _, err := os.Stat(v1); err == nil {
			if _, err := os.Stat(m.stagingPath(h.task.ID)); err != nil {
				_ = moveFile(v1, m.stagingPath(h.task.ID))
			}
		}
	}
	// 旧暂存目录已无要保留的数据（活任务的 part 已迁走），整目录移除；
	// 目录里的孤儿数据也随之一并清理
	seen := make(map[string]bool)
	for _, h := range m.handles {
		if seen[h.task.SaveDir] {
			continue
		}
		seen[h.task.SaveDir] = true
		_ = os.RemoveAll(filepath.Join(h.task.SaveDir, ".downloader"))
	}
	// 统一暂存目录内的孤儿清理（任务记录被外部删除等遗留）
	entries, err := os.ReadDir(m.stagingDir())
	if err != nil {
		return
	}
	keep := make(map[string]bool, len(m.handles))
	for id := range m.handles {
		keep[id+".part"] = true
	}
	for _, e := range entries {
		if !keep[e.Name()] {
			_ = os.Remove(filepath.Join(m.stagingDir(), e.Name()))
		}
	}
	_ = os.Remove(m.stagingDir()) // 已空则移除
}

func (m *Manager) snapshotLocked() []Task {
	out := make([]Task, 0, len(m.order))
	now := m.now()
	for _, id := range m.order {
		h := m.handles[id]
		if h.task.Status == StatusRunning {
			// 快照只读，不推进速度采样点（由 progressLoop 结算瞬时速度）
			out = append(out, h.runningView(now, false))
			continue
		}
		out = append(out, h.task)
	}
	return out
}

func (m *Manager) persistTasksLocked() {
	tasks := make([]Task, 0, len(m.order))
	for _, id := range m.order {
		tasks = append(tasks, m.handles[id].task)
	}
	if err := m.store.SaveTasks(tasks); err != nil {
		fmt.Fprintln(os.Stderr, "保存任务列表失败:", err)
	}
}

// progressLoop 每 500ms 推送一次运行中任务的进度与速度，并唤醒到点的定时任务。
func (m *Manager) progressLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		m.dispatchLocked() // 定时任务到点后可启动
		if m.running == 0 {
			m.mu.Unlock()
			continue
		}
		now := m.now()
		out := make([]Task, 0, len(m.order))
		for _, id := range m.order {
			h := m.handles[id]
			if h.task.Status == StatusRunning {
				// 结算瞬时速度并推进采样点
				out = append(out, h.runningView(now, true))
				continue
			}
			out = append(out, h.task)
		}
		m.mu.Unlock()
		m.emitTasks(out)
	}
}

func (m *Manager) updateTask(h *taskHandle, fn func(*Task)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&h.task)
	m.persistTasksLocked()
}

func (m *Manager) setBase(h *taskHandle, base int64) {
	m.mu.Lock()
	h.base = base
	m.mu.Unlock()
}

// avgSpeedOf 平均速度（字节/秒）：下载量 / 活跃时长。
func avgSpeedOf(downloaded, activeMs int64) int64 {
	if activeMs <= 0 || downloaded <= 0 {
		return 0
	}
	return downloaded * 1000 / activeMs
}

// SaveDirAllowed 报告目录是否为空（=使用默认目录）或落在
// 「默认下载目录 + 设置中的白名单目录」之内。用于 REST API 新建、
// API 删除文件、JSON 导入等"目录并非用户当面选择"的路径，
// 阻断借接口向任意目录写文件或删文件。
func (m *Manager) SaveDirAllowed(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return true // 空 = 使用默认下载目录
	}
	m.mu.Lock()
	bases := make([]string, 0, 1+len(m.settings.AllowSaveDirs))
	bases = append(bases, m.settings.SaveDir)
	bases = append(bases, m.settings.AllowSaveDirs...)
	m.mu.Unlock()
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for _, b := range bases {
		if strings.TrimSpace(b) == "" {
			continue
		}
		if baseAbs, err := filepath.Abs(b); err == nil && dirWithin(abs, baseAbs) {
			return true
		}
	}
	return false
}

// TaskSaveDir 返回任务当前的保存目录；任务不存在时 ok 为 false。
func (m *Manager) TaskSaveDir(id string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.handles[id]
	if !ok {
		return "", false
	}
	return h.task.SaveDir, true
}

// dirWithin 报告 path 等于 base 或位于 base 目录树下（Windows 不区分大小写）。
func dirWithin(path, base string) bool {
	path = filepath.Clean(path)
	base = filepath.Clean(base)
	if path == base {
		return true
	}
	if !strings.HasSuffix(base, string(os.PathSeparator)) {
		base += string(os.PathSeparator)
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
		base = strings.ToLower(base)
	}
	return strings.HasPrefix(path, base)
}

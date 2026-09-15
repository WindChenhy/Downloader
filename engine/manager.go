package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	runStartedAt time.Time // 本轮运行开始时刻；零值表示当前未在下载，mu 保护
}

// Manager 管理全部下载任务：排队、并发控制、持久化与事件通知。
// 所有公开方法并发安全。
type Manager struct {
	store  *Store
	client *http.Client
	notify func(name string, data ...interface{})

	mu       sync.Mutex
	settings Settings
	handles  map[string]*taskHandle
	order    []string // 按创建顺序，保持列表稳定
	running  int
	limiter  *rateLimiter
	// queueIdleFired 队列清空后只触发一次「全部完成」动作，新任务会重置
	queueIdleFired bool
}

// NewManager 创建管理器并加载磁盘上的任务与设置。
// 上次退出时仍在运行的任务一律视为暂停；排队中的任务自动继续排队。
func NewManager(dataDir string, notify func(name string, data ...interface{})) (*Manager, error) {
	store := NewStore(dataDir)
	settings, err := store.LoadSettings()
	if err != nil {
		return nil, err
	}
	m := &Manager{
		store:    store,
		client:   buildHTTPClient(settings),
		notify:   notify,
		settings: settings,
		handles:  make(map[string]*taskHandle),
		limiter:  newRateLimiter(settings.SpeedLimit),
	}
	tasks, err := store.LoadTasks()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, t := range tasks {
		if t.Status == StatusRunning {
			t.Status = StatusPaused
			t.Speed = 0
		}
		m.handles[t.ID] = &taskHandle{task: t, done: make(chan struct{}), prevAt: now}
		m.order = append(m.order, t.ID)
	}
	m.persistTasksLocked()
	m.mu.Lock()
	m.cleanupOrphanStagingLocked()
	m.dispatchLocked()
	m.mu.Unlock()
	go m.progressLoop()
	return m, nil
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
	m.notify("tasks:changed", snap)
	m.notify("task:created", t)
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
		CreatedAt:        time.Now(),
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
		return fmt.Errorf("任务不存在")
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
		m.notify("tasks:changed", notifySnap)
		m.notify("task:paused", t)
	}
	return nil
}

// ResumeTask 恢复已暂停/失败的任务（重新入队）。
func (m *Manager) ResumeTask(id string) error {
	m.mu.Lock()
	h, ok := m.handles[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("任务不存在")
	}
	if h.task.Status != StatusPaused && h.task.Status != StatusFailed {
		status := h.task.Status
		m.mu.Unlock()
		return fmt.Errorf("当前状态不可恢复: %s", status)
	}
	h.task.Status = StatusQueued
	h.task.Error = ""
	m.dispatchLocked()
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.notify("tasks:changed", snap)
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
		return fmt.Errorf("任务不存在")
	}
	h.task.Priority = priority
	m.dispatchLocked()
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.notify("tasks:changed", snap)
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
		return fmt.Errorf("任务不存在")
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
	m.notify("tasks:changed", snap)
	return nil
}

// SetTaskSpeedLimit 设置每任务限速（字节/秒，0 跟随全局）。
func (m *Manager) SetTaskSpeedLimit(id string, limit int64) error {
	if limit < 0 {
		limit = 0
	}
	m.mu.Lock()
	h, ok := m.handles[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("任务不存在")
	}
	h.task.SpeedLimit = limit
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.notify("tasks:changed", snap)
	return nil
}

// SetTaskStartAt 设置定时开始（RFC3339；空字符串表示立即）。
func (m *Manager) SetTaskStartAt(id string, startAtRFC3339 string) error {
	var ts time.Time
	if s := strings.TrimSpace(startAtRFC3339); s != "" {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return fmt.Errorf("无效时间: %v", err)
		}
		ts = parsed
	}
	m.mu.Lock()
	h, ok := m.handles[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("任务不存在")
	}
	h.task.StartAt = ts
	m.dispatchLocked()
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.notify("tasks:changed", snap)
	return nil
}

// RemoveTask 删除任务记录。deleteFiles 为 false 时仅删除记录与未完成的
// .part 数据，已下载完成的正式文件保留在磁盘上；为 true 时连同正式文件一起删除。
func (m *Manager) RemoveTask(id string, deleteFiles bool) error {
	m.mu.Lock()
	h, ok := m.handles[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("任务不存在")
	}
	running := h.task.Status == StatusRunning
	if h.cancel != nil {
		h.cancel()
	}
	if running {
		m.mu.Unlock()
		select {
		case <-h.done:
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
	m.notify("tasks:changed", snap)

	// 未完成的暂存数据（.downloader/<任务ID>.part）随记录一起清理；
	// 正式文件是否删除由 deleteFiles 决定
	_ = os.Remove(m.stagingPath(t.SaveDir, t.ID))
	if deleteFiles {
		_ = os.Remove(filepath.Join(t.SaveDir, t.FileName))
	}
	_ = os.Remove(m.store.StatePath(id))
	_ = os.Remove(m.stagingDir(t.SaveDir)) // 暂存目录已空则一并移除
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

// SaveSettings 持久化并应用设置（代理/UA/全局限速即时生效）。
func (m *Manager) SaveSettings(s Settings) error {
	s.normalize()
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
	now := time.Now()
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
		h.prevAt = time.Now()
		h.runStartedAt = time.Now() // 活跃下载耗时从本轮开始累计
		m.running++

		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		h.done = make(chan struct{}) // 每轮运行一个新 done，上一轮的已被关闭
		r := &taskRunner{m: m, h: h, task: h.task, cancel: cancel}
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
	h.task.Speed = 0
	// 结算本轮活跃下载耗时（暂停前一直在下的时间）
	if !h.runStartedAt.IsZero() {
		h.task.ActiveMs += time.Since(h.runStartedAt).Milliseconds()
		h.runStartedAt = time.Time{}
	}

	switch {
	case err == nil:
		h.task.Status = StatusCompleted
		h.task.Error = ""
		if h.task.TotalSize > 0 {
			h.task.Downloaded = h.task.TotalSize
		}
		h.task.FinishedAt = time.Now()
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
		h.task.FinishedAt = time.Now()
		h.task.AvgSpeed = avgSpeedOf(h.task.Downloaded, h.task.ActiveMs)
	}
	t := h.task
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	status := h.task.Status
	idle := m.queueIdleLocked()
	m.dispatchLocked()
	m.mu.Unlock()
	m.notify("tasks:changed", snap)
	if status == StatusPaused {
		m.notify("task:paused", t)
	} else {
		m.notify("task:finished", t) // 供系统通知等上层钩子使用（完成/失败）
	}
	if idle {
		m.notify("queue:idle", t)
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

// stagingDir 任务的下载暂存目录：<保存目录>/.downloader。
// 刻意放在保存目录同盘而非系统临时目录——完成后的"移动"是同盘原地
// 改名（瞬时）；放系统盘会导致跨盘改名退化为整文件复制。
// 点前缀命名避免被用户顺手误删，下载中的数据不再散落在下载目录里。
func (m *Manager) stagingDir(saveDir string) string {
	return filepath.Join(saveDir, ".downloader")
}

func (m *Manager) stagingPath(saveDir, taskID string) string {
	return filepath.Join(m.stagingDir(saveDir), taskID+".part")
}

// exactDownloadedLocked 从磁盘上的续传状态精确计算已下载字节数
// （进度快照中的 base+live 含被丢弃的分段局部重试字节，仅用于展示）。
func (m *Manager) exactDownloadedLocked(h *taskHandle) int64 {
	if sc, err := loadSidecar(m.store.StatePath(h.task.ID)); err == nil {
		if sc.Single {
			if st, err := os.Stat(m.stagingPath(h.task.SaveDir, h.task.ID)); err == nil {
				return st.Size()
			}
			return 0
		}
		return sc.ProgressBytes()
	}
	return h.base + h.live.Load()
}

// cleanupOrphanStagingLocked 清理各暂存目录中不再属于任何现存任务的
// 孤儿 part 文件（任务记录被外部删除等遗留），并移除清空后的暂存目录。
// 调用方须持有 mu；仅在启动时执行一次。
func (m *Manager) cleanupOrphanStagingLocked() {
	keep := map[string]map[string]bool{} // 暂存目录 -> 应保留的文件名集合
	// 旧版本把 part 放在保存目录（<文件名>.part），迁移到暂存目录，
	// 避免升级后已暂停任务从头重下
	for _, h := range m.handles {
		legacy := filepath.Join(h.task.SaveDir, h.task.FileName+".part")
		if _, err := os.Stat(legacy); err == nil {
			_ = os.MkdirAll(m.stagingDir(h.task.SaveDir), 0o755)
			_ = os.Rename(legacy, m.stagingPath(h.task.SaveDir, h.task.ID))
		}
	}
	for _, h := range m.handles {
		dir := m.stagingDir(h.task.SaveDir)
		if keep[dir] == nil {
			keep[dir] = map[string]bool{}
		}
		keep[dir][h.task.ID+".part"] = true
	}
	for dir, kept := range keep {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // 目录尚不存在
		}
		for _, e := range entries {
			if !kept[e.Name()] {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
		_ = os.Remove(dir) // 已清空则一并移除；非空则失败忽略
	}
}

func (m *Manager) snapshotLocked() []Task {
	out := make([]Task, 0, len(m.order))
	now := time.Now()
	for _, id := range m.order {
		h := m.handles[id]
		t := h.task
		if t.Status == StatusRunning {
			d := h.base + h.live.Load()
			if t.TotalSize > 0 && d > t.TotalSize {
				d = t.TotalSize
			}
			t.Downloaded = d
			// 展示用：把本轮尚未结算的活跃时长并入快照
			if !h.runStartedAt.IsZero() {
				t.ActiveMs += now.Sub(h.runStartedAt).Milliseconds()
			}
			t.AvgSpeed = avgSpeedOf(t.Downloaded, t.ActiveMs)
		}
		out = append(out, t)
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
		now := time.Now()
		out := make([]Task, 0, len(m.order))
		for _, id := range m.order {
			h := m.handles[id]
			t := h.task
			if t.Status == StatusRunning {
				d := h.base + h.live.Load()
				if t.TotalSize > 0 && d > t.TotalSize {
					d = t.TotalSize
				}
				if dt := now.Sub(h.prevAt).Seconds(); dt > 0 {
					sp := int64(float64(d-h.prev) / dt)
					if sp < 0 {
						sp = 0
					}
					t.Speed = sp
				}
				h.prev = d
				h.prevAt = now
				t.Downloaded = d
				if !h.runStartedAt.IsZero() {
					t.ActiveMs += now.Sub(h.runStartedAt).Milliseconds()
				}
				t.AvgSpeed = avgSpeedOf(t.Downloaded, t.ActiveMs)
			}
			out = append(out, t)
		}
		m.mu.Unlock()
		m.notify("tasks:changed", out)
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

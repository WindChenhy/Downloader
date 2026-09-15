package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	m.mu.Unlock()
	go m.progressLoop()
	m.dispatchLocked()
	return m, nil
}

func (m *Manager) AddTask(rawURL, saveDir string, connections int, customName string) (Task, error) {
	u, err := validateURL(rawURL)
	if err != nil {
		return Task{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
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
	t := Task{
		ID:          newID(),
		URL:         rawURL,
		FileName:    name,
		CustomName:  custom,
		SaveDir:     abs,
		Status:      StatusQueued,
		Connections: connections,
		CreatedAt:   time.Now(),
	}
	m.handles[t.ID] = &taskHandle{task: t, done: make(chan struct{})}
	m.order = append(m.order, t.ID)
	m.dispatchLocked()
	m.changedLocked()
	m.notify("task:created", t) // 供系统通知等上层钩子使用
	return t, nil
}

func (m *Manager) PauseTask(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.handles[id]
	if !ok {
		return fmt.Errorf("任务不存在")
	}
	switch h.task.Status {
	case StatusQueued:
		h.task.Status = StatusPaused
		t := h.task
		m.changedLocked()
		m.notify("task:paused", t) // 排队中暂停不经 finishTask，这里直接通知
	case StatusRunning:
		if h.cancel != nil {
			h.cancel() // run goroutine 退出后由 finishTask 置为 Paused 并通知
		}
	}
	return nil
}

func (m *Manager) ResumeTask(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.handles[id]
	if !ok {
		return fmt.Errorf("任务不存在")
	}
	if h.task.Status != StatusPaused && h.task.Status != StatusFailed {
		return fmt.Errorf("当前状态不可恢复: %s", h.task.Status)
	}
	h.task.Status = StatusQueued
	h.task.Error = ""
	m.dispatchLocked()
	m.changedLocked()
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
	m.changedLocked()
	m.mu.Unlock()

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

func (m *Manager) GetTasks() []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Manager) GetSettings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

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
func (m *Manager) dispatchLocked() {
	for m.running < m.settings.ConcurrentTasks {
		var nextID string
		for _, id := range m.order {
			if m.handles[id].task.Status == StatusQueued {
				nextID = id
				break
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
	defer m.mu.Unlock()
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
		_ = os.Remove(m.store.StatePath(h.task.ID))
	case errors.Is(err, ErrPaused):
		h.task.Status = StatusPaused
		h.task.Error = ""
		h.task.Downloaded = m.exactDownloadedLocked(h)
	default:
		h.task.Status = StatusFailed
		h.task.Error = err.Error()
		h.task.Downloaded = m.exactDownloadedLocked(h)
		h.task.FinishedAt = time.Now()
	}
	t := h.task
	m.persistTasksLocked()
	m.notify("tasks:changed", m.snapshotLocked())
	if h.task.Status == StatusPaused {
		m.notify("task:paused", t)
	} else {
		m.notify("task:finished", t) // 供系统通知等上层钩子使用（完成/失败）
	}
	m.dispatchLocked()
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
		}
		out = append(out, t)
	}
	return out
}

func (m *Manager) changedLocked() {
	m.persistTasksLocked()
	m.notify("tasks:changed", m.snapshotLocked())
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

// progressLoop 每 500ms 推送一次运行中任务的进度与速度。
func (m *Manager) progressLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
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

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
	task   Task
	cancel context.CancelFunc
	done   chan struct{} // 本轮运行的 goroutine 退出后关闭
	live   atomic.Int64  // 本轮运行已写入字节数
	base   int64         // 本轮起点（续传时为已完成字节数），mu 保护
	prev   int64         // 速度计算的上一次快照，mu 保护
	prevAt time.Time
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
	go m.progressLoop()
	m.dispatchLocked()
	return m, nil
}

func (m *Manager) AddTask(rawURL, saveDir string, connections int) (Task, error) {
	u, err := validateURL(rawURL)
	if err != nil {
		return Task{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(saveDir) == "" {
		saveDir = m.settings.SaveDir
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
	t := Task{
		ID:          newID(),
		URL:         rawURL,
		FileName:    fileNameFromURL(u),
		SaveDir:     abs,
		Status:      StatusQueued,
		Connections: connections,
		CreatedAt:   time.Now(),
	}
	if t.FileName == "" {
		t.FileName = "download"
	}
	m.handles[t.ID] = &taskHandle{task: t, done: make(chan struct{})}
	m.order = append(m.order, t.ID)
	m.dispatchLocked()
	m.changedLocked()
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
		m.changedLocked()
	case StatusRunning:
		if h.cancel != nil {
			h.cancel() // run goroutine 退出后由 finishTask 置为 Paused
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

func (m *Manager) RemoveTask(id string) error {
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

	// 清理未完成残留；已完成的正式文件保留在原处
	_ = os.Remove(filepath.Join(t.SaveDir, t.FileName+".part"))
	_ = os.Remove(m.store.StatePath(id))
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
		h.base = 0
		h.live.Store(0)
		h.prev = 0
		h.prevAt = time.Now()
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

	switch {
	case err == nil:
		h.task.Status = StatusCompleted
		h.task.Error = ""
		if h.task.TotalSize > 0 {
			h.task.Downloaded = h.task.TotalSize
		}
		_ = os.Remove(m.store.StatePath(h.task.ID))
	case errors.Is(err, ErrPaused):
		h.task.Status = StatusPaused
		h.task.Error = ""
		h.task.Downloaded = m.exactDownloadedLocked(h)
	default:
		h.task.Status = StatusFailed
		h.task.Error = err.Error()
		h.task.Downloaded = m.exactDownloadedLocked(h)
	}
	t := h.task
	m.persistTasksLocked()
	m.notify("tasks:changed", m.snapshotLocked())
	m.notify("task:finished", t) // 供系统通知等上层钩子使用
	m.dispatchLocked()
}

// exactDownloadedLocked 从磁盘上的续传状态精确计算已下载字节数
// （进度快照中的 base+live 含被丢弃的分段局部重试字节，仅用于展示）。
func (m *Manager) exactDownloadedLocked(h *taskHandle) int64 {
	if sc, err := loadSidecar(m.store.StatePath(h.task.ID)); err == nil {
		if sc.Single {
			if st, err := os.Stat(filepath.Join(h.task.SaveDir, h.task.FileName+".part")); err == nil {
				return st.Size()
			}
			return 0
		}
		return sc.DoneBytes()
	}
	return h.base + h.live.Load()
}

func (m *Manager) snapshotLocked() []Task {
	out := make([]Task, 0, len(m.order))
	for _, id := range m.order {
		h := m.handles[id]
		t := h.task
		if t.Status == StatusRunning {
			d := h.base + h.live.Load()
			if t.TotalSize > 0 && d > t.TotalSize {
				d = t.TotalSize
			}
			t.Downloaded = d
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

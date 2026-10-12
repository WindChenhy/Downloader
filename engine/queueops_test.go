package engine

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// eventLog 线程安全地累计各事件名的推送次数。
type eventLog struct {
	mu   sync.Mutex
	seen map[EventName]int
}

func newEventLog() *eventLog { return &eventLog{seen: map[EventName]int{}} }

func (l *eventLog) notify(ev Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen[ev.Name]++
}

func (l *eventLog) count(name EventName) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seen[name]
}

// --- 仅测试使用的引擎内部钩子（同包可见，避免为了测试导出实现细节）---

func (m *Manager) setStatusForTest(id string, s Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.handles[id]; ok {
		h.task.Status = s
	}
}

func runningCount(m *Manager) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// statusOf 读取指定任务的当前状态。
func statusOf(m *Manager, id string) Status {
	for _, t := range m.GetTasks() {
		if t.ID == id {
			return t.Status
		}
	}
	return ""
}

func TestQueueGateBlocksDispatch(t *testing.T) {
	content := newContent(t, 4000)
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)
	if err := m.SetQueueSuspended(true); err != nil {
		t.Fatal(err)
	}
	task, err := m.AddTask(srv.URL, saveDir, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	// 闭闸：任务不得占用并发额度，也不得自行启动
	time.Sleep(200 * time.Millisecond)
	if got := statusOf(m, task.ID); got != StatusQueued {
		t.Fatalf("闭闸期间状态 = %s, want queued", got)
	}
	if n := runningCount(m); n != 0 {
		t.Fatalf("闭闸期间运行数 = %d, want 0", n)
	}

	// 开闸后立即放行
	if err := m.SetQueueSuspended(false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return statusOf(m, task.ID) == StatusCompleted }, 10*time.Second)
}

func TestQueueGatePersistsAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	m, err := NewManager(dataDir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	if err := m.SetQueueSuspended(true); err != nil {
		t.Fatal(err)
	}

	// 同一数据目录重开：闭闸状态应保持
	m2, err := NewManager(dataDir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m2)
	if !m2.GetSettings().QueueSuspended {
		t.Fatal("重启后队列总闸应仍为闭闸")
	}
	content := newContent(t, 2000)
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	task, err := m2.AddTask(srv.URL, t.TempDir(), 2, "")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := statusOf(m2, task.ID); got != StatusQueued {
		t.Fatalf("重启后闭闸期间状态 = %s, want queued", got)
	}
}

// TestPauseAllKeepsQueueIdleSilent 是本批的行为修正：
// 暂停全部之后队列看似「没有人在跑」，但仍有未完成任务，
// 此时绝不能发 EventQueueIdle —— 上层收到它会执行完成后动作（可能是关机）。
func TestPauseAllKeepsQueueIdleSilent(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	goRelease := func() { once.Do(func() { close(release) }) }
	defer goRelease()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "1000")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			return
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Range", "bytes 0-999/1000")
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(make([]byte, 1000))
	}))
	defer srv.Close()

	log := newEventLog()
	m, err := NewManager(t.TempDir(), log.notify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	saveDir := t.TempDir()
	var ids []string
	for _, name := range []string{"a.bin", "b.bin"} {
		task, err := m.AddTaskWithChecksum(AddTaskParams{URL: srv.URL, SaveDir: saveDir, Connections: 1, CustomName: name})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
	}
	waitFor(t, func() bool {
		return statusOf(m, ids[0]) == StatusRunning && statusOf(m, ids[1]) == StatusRunning
	}, 5*time.Second)

	if n := m.PauseAll(); n != 2 {
		t.Fatalf("PauseAll = %d, want 2", n)
	}
	waitFor(t, func() bool {
		return statusOf(m, ids[0]) == StatusPaused && statusOf(m, ids[1]) == StatusPaused
	}, 5*time.Second)
	if c := log.count(EventQueueIdle); c != 0 {
		t.Fatalf("暂停全部后不应触发队列空闲事件, got %d 次", c)
	}

	// 恢复全部 → 放行 → 正常结束，且空闲事件恰好一次
	if n := m.ResumeAll(); n != 2 {
		t.Fatalf("ResumeAll = %d, want 2", n)
	}
	goRelease()
	waitFor(t, func() bool {
		for _, id := range ids {
			if statusOf(m, id) != StatusCompleted {
				return false
			}
		}
		return true
	}, 10*time.Second)
	if c := log.count(EventQueueIdle); c != 1 {
		t.Fatalf("全部完成应恰好触发一次队列空闲事件, got %d 次", c)
	}
}

// TestBatchOpsEmitSingleSnapshot 闭闸时所有任务停在 queued，
// 批量暂停只推送一次列表事件（逐个调 PauseTask 会是 N 次）。
func TestBatchOpsEmitSingleSnapshot(t *testing.T) {
	content := newContent(t, 1000)
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	log := newEventLog()
	m, err := NewManager(t.TempDir(), log.notify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	if err := m.SetQueueSuspended(true); err != nil {
		t.Fatal(err) // 让任务停在 queued，便于观察批量事件
	}
	for i := 0; i < 5; i++ {
		if _, err := m.AddTask(srv.URL, t.TempDir(), 1, ""); err != nil {
			t.Fatal(err)
		}
	}
	before := log.count(EventTasksChanged)
	if n := m.PauseAll(); n != 5 {
		t.Fatalf("PauseAll = %d, want 5", n)
	}
	if got := log.count(EventTasksChanged) - before; got != 1 {
		t.Fatalf("批量暂停推送列表事件 = %d 次, want 1", got)
	}
	if n := m.ResumeAll(); n != 5 {
		t.Fatalf("ResumeAll = %d, want 5", n)
	}
	for _, tsk := range m.GetTasks() {
		if tsk.Status != StatusQueued {
			t.Fatalf("闭闸时恢复全部后应停在 queued: %+v", tsk)
		}
	}
}

// TestRetryAllFailedSkipsPaused 只重试失败项，不动已暂停的任务。
func TestRetryAllFailedSkipsPaused(t *testing.T) {
	content := newContent(t, 1000)
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, err := NewManager(t.TempDir(), nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	if err := m.SetQueueSuspended(true); err != nil {
		t.Fatal(err)
	}
	failed, err := m.AddTask(srv.URL, t.TempDir(), 1, "failed.bin")
	if err != nil {
		t.Fatal(err)
	}
	paused, err := m.AddTask(srv.URL, t.TempDir(), 1, "paused.bin")
	if err != nil {
		t.Fatal(err)
	}
	m.setStatusForTest(failed.ID, StatusFailed)
	m.setStatusForTest(paused.ID, StatusPaused)

	if n := m.RetryAllFailed(); n != 1 {
		t.Fatalf("RetryAllFailed = %d, want 1", n)
	}
	if got := statusOf(m, failed.ID); got != StatusQueued {
		t.Fatalf("失败任务应重新入队, got %s", got)
	}
	if got := statusOf(m, paused.ID); got != StatusPaused {
		t.Fatalf("已暂停任务不应被动, got %s", got)
	}
}

// TestClearCompletedKeepsFiles 清理已完成只移除记录，磁盘文件保留。
func TestClearCompletedKeepsFiles(t *testing.T) {
	content := newContent(t, 6000)
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, err := NewManager(t.TempDir(), nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	saveDir := t.TempDir()
	for i := 0; i < 2; i++ {
		if _, err := m.AddTask(srv.URL, saveDir, 3, ""); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool {
		done := 0
		for _, tsk := range m.GetTasks() {
			if tsk.Status == StatusCompleted {
				done++
			}
		}
		return done == 2
	}, 10*time.Second)

	// 再留一个暂停任务，确认清理不碰它
	if err := m.SetQueueSuspended(true); err != nil {
		t.Fatal(err)
	}
	extra, err := m.AddTask(srv.URL, saveDir, 1, "extra.bin")
	if err != nil {
		t.Fatal(err)
	}
	m.setStatusForTest(extra.ID, StatusPaused)

	if n := m.ClearCompleted(); n != 2 {
		t.Fatalf("ClearCompleted = %d, want 2", n)
	}
	left := m.GetTasks()
	if len(left) != 1 || left[0].Status != StatusPaused {
		t.Fatalf("清理后应只剩暂停任务: %+v", left)
	}
	if _, err := os.Stat(filepath.Join(saveDir, "test.bin")); err != nil {
		t.Fatalf("已完成文件应保留: %v", err)
	}
	if n := m.ClearCompleted(); n != 0 {
		t.Fatalf("重复清理 = %d, want 0", n)
	}
}

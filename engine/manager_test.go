package engine

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testServer 是可控制行为的本地 HTTP 文件服务器：
// 支持 HEAD 与 Range 请求，可模拟错误、Range 不可用、以及阻塞请求。
type testServer struct {
	content []byte
	etag    string
	noRange bool // 模拟不支持 Range 的服务器：忽略 Range 头，始终 200 全量

	mu        sync.Mutex
	gate      chan struct{} // 非空时阻塞下一个 Range 请求直到释放
	rangeHits atomic.Int64
	failNext  atomic.Bool // 下一个 Range 请求返回 500
	ranges    []string    // 收到的每个 Range 请求头（断言续传偏移用）
}

// requestedRanges 返回已记录的 Range 请求头列表。
func (s *testServer) requestedRanges() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ranges...)
}

// releaseGate 放行被阻塞的分段请求；幂等且并发安全：
// 若 gate 已被某个请求取走（字段被置 nil），该请求会自行感知客户端断开。
func (s *testServer) releaseGate() {
	s.mu.Lock()
	g := s.gate
	s.gate = nil
	s.mu.Unlock()
	if g != nil {
		close(g)
	}
}

func (s *testServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.Itoa(len(s.content)))
		if !s.noRange {
			w.Header().Set("Accept-Ranges", "bytes")
		}
		if s.etag != "" {
			w.Header().Set("ETag", s.etag)
		}
		w.Header().Set("Content-Disposition", `attachment; filename="test.bin"`)
		w.WriteHeader(http.StatusOK)
		return
	}

	rng := r.Header.Get("Range")
	if s.noRange || rng == "" {
		w.Header().Set("Content-Length", strconv.Itoa(len(s.content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(s.content)
		return
	}

	spec, _ := strings.CutPrefix(rng, "bytes=")
	a, b, hasB := strings.Cut(spec, "-")
	start, _ := strconv.ParseInt(a, 10, 64)
	end := int64(len(s.content)) - 1
	if hasB && b != "" {
		end, _ = strconv.ParseInt(b, 10, 64)
	}
	if start < 0 || start >= int64(len(s.content)) {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if end > int64(len(s.content))-1 {
		end = int64(len(s.content)) - 1
	}

	s.rangeHits.Add(1)
	s.mu.Lock()
	s.ranges = append(s.ranges, rng)
	s.mu.Unlock()
	if s.failNext.CompareAndSwap(true, false) {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	gate := s.gate
	s.gate = nil
	s.mu.Unlock()
	if gate != nil {
		// 客户端断开（如暂停取消）时立即返回，避免 handler 泄漏
		select {
		case <-gate:
		case <-r.Context().Done():
		}
	}

	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.content)))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(s.content[start : end+1])
}

func newContent(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("等待超时")
}

func nopNotify(string, ...interface{}) {}

func mustManager(t *testing.T) (*Manager, string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	return m, t.TempDir()
}

func TestManagerMultiChunkDownload(t *testing.T) {
	content := newContent(t, 10000)
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)
	task, err := m.AddTask(srv.URL, saveDir, 4, "")
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		return m.GetTasks()[0].Status == StatusCompleted
	}, 10*time.Second)

	got, err := os.ReadFile(filepath.Join(saveDir, "test.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatal("下载内容与原始内容不一致")
	}
	// 完成后应清理状态文件与暂存数据，暂存目录也应移除
	if _, err := os.Stat(m.stagingPath(saveDir, task.ID)); !os.IsNotExist(err) {
		t.Fatal("暂存数据文件应被改名移除")
	}
	if _, err := os.Stat(m.stagingDir(saveDir)); !os.IsNotExist(err) {
		t.Fatal("完成后的暂存目录应被移除")
	}
	if _, err := os.Stat(m.store.StatePath(task.ID)); !os.IsNotExist(err) {
		t.Fatal("完成后的 sidecar 状态文件应被删除")
	}
	tk := m.GetTasks()[0]
	if tk.Downloaded != tk.TotalSize {
		t.Fatalf("Downloaded = %d, want %d", tk.Downloaded, tk.TotalSize)
	}
}

func TestManagerRetryOnServerError(t *testing.T) {
	content := newContent(t, 4000)
	ts := &testServer{content: content, etag: `"abc"`}
	ts.failNext.Store(true) // 第一次分段请求返回 500
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)
	if _, err := m.AddTask(srv.URL, saveDir, 2, ""); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		return m.GetTasks()[0].Status == StatusCompleted
	}, 10*time.Second)

	got, _ := os.ReadFile(filepath.Join(saveDir, "test.bin"))
	if string(got) != string(content) {
		t.Fatal("重试后下载内容不一致")
	}
}

func TestManagerPauseAndResume(t *testing.T) {
	content := newContent(t, 10000)
	ts := &testServer{content: content, etag: `"abc"`}
	ts.gate = make(chan struct{}) // 阻塞第一个分段请求，保证暂停发生在下载中
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)
	task, err := m.AddTask(srv.URL, saveDir, 4, "")
	if err != nil {
		t.Fatal(err)
	}
	partPath := m.stagingPath(saveDir, task.ID)

	waitFor(t, func() bool {
		return m.GetTasks()[0].Status == StatusRunning
	}, 5*time.Second)
	// 等 .part 落盘后再暂停，确保测的是"下载中暂停"而不是"探测期取消"
	waitFor(t, func() bool {
		_, err := os.Stat(partPath)
		return err == nil
	}, 5*time.Second)
	if err := m.PauseTask(m.GetTasks()[0].ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		return m.GetTasks()[0].Status == StatusPaused
	}, 5*time.Second)

	if _, err := os.Stat(partPath); err != nil {
		t.Fatal("暂停后暂存数据文件应保留")
	}
	statePath := m.store.StatePath(m.GetTasks()[0].ID)
	if _, err := os.Stat(statePath); err != nil {
		t.Fatal("暂停后 sidecar 状态文件应保留")
	}

	// 放行被阻塞的请求，恢复任务
	ts.releaseGate()
	if err := m.ResumeTask(m.GetTasks()[0].ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		return m.GetTasks()[0].Status == StatusCompleted
	}, 10*time.Second)

	got, _ := os.ReadFile(filepath.Join(saveDir, "test.bin"))
	if string(got) != string(content) {
		t.Fatal("续传后下载内容不一致")
	}
}

func TestManagerNoRangeFallback(t *testing.T) {
	content := newContent(t, 3000)
	ts := &testServer{content: content, noRange: true}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)
	if _, err := m.AddTask(srv.URL, saveDir, 8, ""); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		return m.GetTasks()[0].Status == StatusCompleted
	}, 10*time.Second)

	got, _ := os.ReadFile(filepath.Join(saveDir, "test.bin"))
	if string(got) != string(content) {
		t.Fatal("单连接回退下载内容不一致")
	}
	tk := m.GetTasks()[0]
	if tk.Connections != 8 {
		t.Fatalf("Connections = %d, want 8", tk.Connections)
	}
}

func TestManagerResumeFromSeededSidecar(t *testing.T) {
	content := newContent(t, 10000)
	ts := &testServer{content: content, etag: `"abc"`}
	srv := httptest.NewServer(ts)
	defer srv.Close()

	dataDir := t.TempDir()
	saveDir := t.TempDir()
	m, err := NewManager(dataDir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}

	const id = "fixed-id"
	chunks := CalculateChunks(10000, 4) // 4 段,每段 2500
	// 暂存数据已包含全部内容,sidecar 标记第 0、2 段完成、第 1 段收到 1000 字节——
	// 恢复后服务器只应收到:第 1 段的 bytes=3500-4999 与第 3 段的 bytes=7500-9999
	if err := os.MkdirAll(m.stagingDir(saveDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.stagingPath(saveDir, id), content, 0o644); err != nil {
		t.Fatal(err)
	}
	sc := &Sidecar{
		URL:       srv.URL,
		TotalSize: 10000,
		Chunks: []SidecarChunk{
			{Start: chunks[0].Start, End: chunks[0].End, Done: true},
			{Start: chunks[1].Start, End: chunks[1].End, Received: 1000},
			{Start: chunks[2].Start, End: chunks[2].End, Done: true},
			{Start: chunks[3].Start, End: chunks[3].End},
		},
	}
	if err := saveSidecar(m.store.StatePath(id), sc); err != nil {
		t.Fatal(err)
	}

	h := &taskHandle{task: Task{
		ID: id, URL: srv.URL, FileName: "test.bin", SaveDir: saveDir,
		TotalSize: 10000, Connections: 4, Status: StatusRunning,
	}, done: make(chan struct{})}
	m.mu.Lock()
	m.handles[id] = h
	m.order = append(m.order, id)
	m.running++
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &taskRunner{m: m, h: h, task: h.task, cancel: cancel}
	if err := r.run(ctx); err != nil {
		t.Fatalf("续传运行失败: %v", err)
	}
	close(h.done)

	if hits := ts.rangeHits.Load(); hits != 2 {
		t.Fatalf("续传应只请求缺失的 2 个分段, 实际请求 %d 次", hits)
	}
	// 字节级续传断言：第 1 段必须从已收的 1000 字节偏移继续，而不是整段重来
	ranges := ts.requestedRanges()
	want := map[string]bool{
		"bytes=3500-4999": false,
		"bytes=7500-9999": false,
	}
	for _, rng := range ranges {
		if _, ok := want[rng]; ok {
			want[rng] = true
		} else {
			t.Errorf("收到非预期的 Range 请求: %q", rng)
		}
	}
	for rng, seen := range want {
		if !seen {
			t.Errorf("缺少预期的 Range 请求: %q (实际: %v)", rng, ranges)
		}
	}
	got, err := os.ReadFile(filepath.Join(saveDir, "test.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatal("续传后内容不一致")
	}
	if _, err := os.Stat(m.stagingPath(saveDir, id)); !os.IsNotExist(err) {
		t.Fatal("暂存数据应被改名移除")
	}
}

func TestManagerAddTaskValidation(t *testing.T) {
	m, _ := mustManager(t)
	if _, err := m.AddTask("ftp://example.com/a.zip", "", 0, ""); err == nil {
		t.Fatal("ftp 协议应被拒绝")
	}
	if _, err := m.AddTask("not-a-url", "", 0, ""); err == nil {
		t.Fatal("无效 URL 应被拒绝")
	}
}

func TestManagerPersistenceAcrossRestart(t *testing.T) {
	content := newContent(t, 10000)
	ts := &testServer{content: content, etag: `"abc"`}
	ts.gate = make(chan struct{})
	srv := httptest.NewServer(ts)
	defer srv.Close()

	dataDir := t.TempDir()
	saveDir := t.TempDir()
	m, err := NewManager(dataDir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddTask(srv.URL, saveDir, 4, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.GetTasks()[0].Status == StatusRunning }, 5*time.Second)
	// 停掉第一个 Manager 的运行，避免两个 Manager 同时写同一数据目录
	if err := m.PauseTask(m.GetTasks()[0].ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.GetTasks()[0].Status == StatusPaused }, 5*time.Second)

	// 模拟进程退出：直接重建 Manager，运行中的任务应视为暂停
	m2, err := NewManager(dataDir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	tasks := m2.GetTasks()
	if len(tasks) != 1 {
		t.Fatalf("重启后任务数 = %d, want 1", len(tasks))
	}
	if tasks[0].Status != StatusPaused {
		t.Fatalf("重启后状态 = %s, want paused", tasks[0].Status)
	}

	ts.releaseGate()
	if err := m2.ResumeTask(tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m2.GetTasks()[0].Status == StatusCompleted }, 10*time.Second)
	got, _ := os.ReadFile(filepath.Join(saveDir, "test.bin"))
	if string(got) != string(content) {
		t.Fatal("重启恢复后内容不一致")
	}
}

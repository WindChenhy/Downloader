package engine

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// spaceStub 可控的卷剩余空间探针：磁盘预检查的判定全部走真实路径，
// 只把系统调用换成一个能随时改值的数。
type spaceStub struct {
	mu   sync.Mutex
	free int64
}

func (s *spaceStub) set(v int64) {
	s.mu.Lock()
	s.free = v
	s.mu.Unlock()
}

func (s *spaceStub) probe(string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.free, nil
}

func mustManagerWithSpace(t *testing.T, stub *spaceStub) (*Manager, string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), nopNotify, WithFreeSpaceProbe(stub.probe))
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	return m, t.TempDir()
}

// TestSpaceGuardBlocksUntilSpaceFreed 装不下时任务不启动也不报错：停在 queued 并写明原因，
// 空间腾出来由进度循环自动复检放行（无需用户手动重试）。
func TestSpaceGuardBlocksUntilSpaceFreed(t *testing.T) {
	content := newContent(t, 4000)
	srv := httptest.NewServer(&testServer{content: content, etag: `"sp"`})
	defer srv.Close()

	stub := &spaceStub{free: 100} // 远小于文件大小
	m, saveDir := mustManagerWithSpace(t, stub)

	task, err := m.AddTask(srv.URL, saveDir, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, tk := range m.GetTasks() {
			if tk.ID == task.ID && tk.Status == StatusQueued && tk.BlockReason != "" {
				return true
			}
		}
		return false
	}, 10*time.Second)

	tk := m.GetTasks()[0]
	if tk.Status != StatusQueued {
		t.Fatalf("空间不足应保持排队, got %s", tk.Status)
	}
	if !strings.Contains(tk.BlockReason, "磁盘空间不足") {
		t.Fatalf("阻塞原因未说明空间不足: %q", tk.BlockReason)
	}
	if tk.Error != "" {
		t.Fatalf("空间不足不该记错误: %q", tk.Error)
	}
	if tk.ActiveMs != 0 {
		t.Fatalf("只探测的一轮不该计入下载耗时: %dms", tk.ActiveMs)
	}
	// 一个字节都不该落盘：拦截发生在创建暂存文件之前
	if _, err := os.Stat(m.stagingPath(task.ID)); err == nil {
		t.Fatal("被拦住的任务不应创建暂存文件")
	}
	// 队列本身是允许跑东西的，只是这一个任务写不下：两个维度分开报
	if st := m.QueueStatus(); st.BlockedTasks != 1 || !st.Active || st.Suspended {
		t.Fatalf("QueueStatus = %+v, want blocked=1 active=true suspended=false", st)
	}

	// 腾出空间：进度循环复检后自己起跑（缓存 TTL 2s + tick 500ms）
	stub.set(1 << 20)
	waitFor(t, func() bool {
		for _, tk := range m.GetTasks() {
			if tk.ID == task.ID {
				return tk.Status == StatusCompleted
			}
		}
		return false
	}, 20*time.Second)
	if st := m.QueueStatus(); st.BlockedTasks != 0 {
		t.Fatalf("起跑后阻塞计数应清零: %+v", st)
	}
}

// TestSpaceGuardAllowsUnknownSize 大小未知（响应走 chunked，拿不到 Content-Length）时
// 无从判断需求量，只按余量阈值检查；阈值为 0 就放行——预检查不能把流式下载一律拦死。
func TestSpaceGuardAllowsUnknownSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK) // 不给 Content-Length：大小未知
			return
		}
		// 先冲一次再写剩下的：响应头提前发出，标准库改用 chunked，不填 Content-Length
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("01234"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = w.Write([]byte("56789"))
	}))
	defer srv.Close()

	stub := &spaceStub{free: 100} // 只剩 100 字节：有长度信息才拦得住
	m, saveDir := mustManagerWithSpace(t, stub)
	if _, err := m.AddTask(srv.URL, saveDir, 1, "tiny.bin"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		tk := m.GetTasks()[0]
		return tk.Status == StatusCompleted
	}, 15*time.Second)
}

// TestSpaceGuardKeepsMargin 余量设置：文件本身装得下，但会把盘写到贴边时同样拦下。
func TestSpaceGuardKeepsMargin(t *testing.T) {
	content := newContent(t, 1000)
	srv := httptest.NewServer(&testServer{content: content, etag: `"mg"`})
	defer srv.Close()

	stub := &spaceStub{free: 1 << 20} // 1MB，装得下 1000 字节
	m, saveDir := mustManagerWithSpace(t, stub)
	st := m.GetSettings()
	st.DiskMinFree = 2 << 20 // 但要求留 2MB
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	task, err := m.AddTask(srv.URL, saveDir, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		tk := m.GetTasks()[0]
		return tk.ID == task.ID && tk.Status == StatusQueued && tk.BlockReason != ""
	}, 10*time.Second)
	if reason := m.GetTasks()[0].BlockReason; !strings.Contains(reason, "预留") {
		t.Fatalf("应说明预留余量: %q", reason)
	}

	// 关掉预检查：同样的空间条件下直接放行并下完（开关是真的开关，不是摆设）。
	// 等到真正结束再结束用例：否则下载还在引用暂存文件，t.TempDir() 删不掉。
	off := m.GetSettings()
	off.DiskGuardOff = true
	if err := m.SaveSettings(off); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		return m.GetTasks()[0].Status == StatusCompleted
	}, 20*time.Second)
}

// TestSpaceGuardPersistsBlockReasonAfterRestart 重启后由预检查重新算出原因，
// 不依赖上次落盘的瞬时值：盘还是满的，任务仍应停在队列里。
func TestSpaceGuardPersistsBlockReasonAfterRestart(t *testing.T) {
	content := newContent(t, 3000)
	srv := httptest.NewServer(&testServer{content: content, etag: `"rs"`})
	defer srv.Close()

	dataDir := t.TempDir()
	stub := &spaceStub{free: 10}
	m, err := NewManager(dataDir, nopNotify, WithFreeSpaceProbe(stub.probe))
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	saveDir := t.TempDir()
	if _, err := m.AddTask(srv.URL, saveDir, 1, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.GetTasks()[0].BlockReason != "" }, 10*time.Second)

	// 换一个数据目录重启会丢任务记录，这里复用同一数据目录重建 Manager
	m2, err := NewManager(dataDir, nopNotify, WithFreeSpaceProbe(stub.probe))
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m2)
	waitFor(t, func() bool {
		tk := m2.GetTasks()[0]
		return tk.Status == StatusQueued && strings.Contains(tk.BlockReason, "磁盘空间不足")
	}, 10*time.Second)
}

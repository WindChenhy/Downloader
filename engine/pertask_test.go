package engine

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestSetTaskSpeedLimitHotUpdate 运行中任务调整每任务限速应立即生效到
// 该轮运行的限速器（含放开为 0 = 不限）。
func TestSetTaskSpeedLimitHotUpdate(t *testing.T) {
	content := newContent(t, 1<<20)
	ts := &testServer{content: content, etag: `"h"`}
	ts.gate = make(chan struct{})
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, saveDir := mustManager(t)
	st := m.GetSettings()
	st.APIEnabled = false // 测试不真正监听端口
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}

	task, err := m.AddTask(srv.URL+"/big.bin", saveDir, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		return m.GetTasks()[0].Status == StatusRunning
	}, 5*time.Second)

	// 运行中把限速从 0 调到 100KB/s：handle 上的限速器立即生效
	if err := m.SetTaskSpeedLimit(task.ID, 100<<10); err != nil {
		t.Fatal(err)
	}
	if got := handleLimiterLimit(m, task.ID); got != 100<<10 {
		t.Fatalf("运行中任务限速应更新为 100KB/s, got %d", got)
	}

	// 再放开为 0（跟随全局，全局也不限）→ 立即不限速
	if err := m.SetTaskSpeedLimit(task.ID, 0); err != nil {
		t.Fatal(err)
	}
	if got := handleLimiterLimit(m, task.ID); got != 0 {
		t.Fatalf("运行中任务限速应放开为 0, got %d", got)
	}

	ts.releaseGate()
	waitFor(t, func() bool {
		return m.GetTasks()[0].Status == StatusCompleted
	}, 10*time.Second)
}

// handleLimiterLimit 读取任务当前运行轮次的限速器值（白盒）。
func handleLimiterLimit(m *Manager, id string) int64 {
	m.mu.Lock()
	h := m.handles[id]
	lim := h.limiter
	m.mu.Unlock()
	if lim == nil {
		return -1
	}
	lim.mu.Lock()
	defer lim.mu.Unlock()
	return lim.limit
}

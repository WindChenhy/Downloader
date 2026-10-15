package engine

import (
	"context"
	"io"
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

// shortReader 每次最多返回 max 个字节，模拟 HTTP body 的短读。
type shortReader struct {
	max int
}

func (s *shortReader) Read(p []byte) (int, error) {
	if s.max <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > s.max {
		n = s.max
	}
	return n, nil
}

// TestLimitedReaderRefundsShortRead 覆盖短读计费：Wait 预扣整块配额后，
// 若底层 Read 只返回少量字节，剩余配额必须退还，否则实际速率会被压成
// 设定值的几分之一（限速是用户可感知的功能，压低了同样算缺陷）。
func TestLimitedReaderRefundsShortRead(t *testing.T) {
	l := newRateLimiter(1 << 20)
	// 预置满额配额，让 Wait 立即通过，避免测试引入真实等待
	l.mu.Lock()
	l.credit = float64(readChunkSize)
	l.last = time.Now()
	l.mu.Unlock()

	lr := &limitedReader{r: &shortReader{max: 100}, ctx: context.Background(), l: l}
	buf := make([]byte, readChunkSize)
	n, err := lr.Read(buf)
	if err != nil || n != 100 {
		t.Fatalf("Read = %d, %v; want 100, nil", n, err)
	}

	l.mu.Lock()
	credit := l.credit
	l.mu.Unlock()
	// 只实际消费 100 字节：除时间累积带来的少量追加外，其余必须已退回。
	// 时间累积恒为非负，故用下界判定即可，不依赖具体等待时长。
	if credit < float64(readChunkSize-100) {
		t.Fatalf("短读后未退还配额: credit = %.0f, want >= %d", credit, readChunkSize-100)
	}
}

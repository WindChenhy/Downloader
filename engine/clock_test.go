package engine

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestScheduledTaskStartsWithFakeClock 用假时钟确定性地验证定时任务：
// 未到点保持排队；时钟推进后立即启动，无需真实等待。
func TestScheduledTaskStartsWithFakeClock(t *testing.T) {
	current := time.Now()
	clock := func() time.Time { return current } // 测试内可控的假时钟

	content := newContent(t, 100)
	srv := httptest.NewServer(&testServer{content: content, etag: `"z"`})
	defer srv.Close()

	m, err := NewManager(t.TempDir(), nopNotify, WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	// 假时钟会跳过统计的 30s 落盘节流，不停掉循环就会和 TempDir 清理抢同一个目录
	stopOnCleanup(t, m)
	saveDir := t.TempDir()

	future := current.Add(1 * time.Hour).Format(time.RFC3339)
	tk, err := m.AddTaskWithChecksum(AddTaskParams{URL: srv.URL, SaveDir: saveDir, StartAt: future})
	if err != nil {
		t.Fatal(err)
	}
	// 假时钟未到点：AddTask 内部已触发过调度，应保持 queued
	if got := m.GetTasks()[0].Status; got != StatusQueued {
		t.Fatalf("未到点应保持 queued, got %s", got)
	}

	// 推进假时钟并重触发调度（SetTaskStartAt 会重新 dispatch）→ 立即启动并跑完
	// （等到结束再退出用例：否则下载中的文件会和 TempDir 清理撞车）
	current = current.Add(2 * time.Hour)
	if err := m.SetTaskStartAt(tk.ID, future); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		return m.GetTasks()[0].Status == StatusCompleted
	}, 10*time.Second)
}

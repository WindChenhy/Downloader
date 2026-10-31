package engine

import (
	"net/http/httptest"
	"testing"
	"time"
)

// atTime 用本地时区构造时刻：时间窗按本地时钟判断，测试也必须按本地时钟造点。
func atTime(h, m int) time.Time {
	return time.Date(2026, 10, 11, h, m, 0, 0, time.Local)
}

func TestWindowContains(t *testing.T) {
	cases := []struct {
		start, end string
		Now        time.Time
		want       bool
	}{
		{"09:00", "18:00", atTime(9, 0), true},   // 左闭
		{"09:00", "18:00", atTime(17, 59), true}, // 右开前一刻
		{"09:00", "18:00", atTime(18, 0), false}, // 到点即出窗
		{"09:00", "18:00", atTime(8, 59), false},
		{"22:00", "06:00", atTime(23, 30), true}, // 跨零点：夜里
		{"22:00", "06:00", atTime(3, 0), true},   // 跨零点：凌晨
		{"22:00", "06:00", atTime(6, 0), false},
		{"22:00", "06:00", atTime(12, 0), false}, // 白天
		{"00:00", "00:00", atTime(12, 0), true},  // 起止相同 = 全天
		{"", "06:00", atTime(12, 0), true},       // 配置没填全 = 不拦
		{"25:00", "06:00", atTime(12, 0), true},  // 非法时刻 = 不拦
	}
	for _, c := range cases {
		if got := windowContains(c.start, c.end, c.Now); got != c.want {
			t.Errorf("windowContains(%q,%q,%s) = %v, want %v", c.start, c.end, c.Now.Format("15:04"), got, c.want)
		}
	}
}

func TestWindowOpensAt(t *testing.T) {
	// 白天且窗口在夜里：下一个开窗点是今天 22:00
	opens, ok := windowOpensAt("22:00", "06:00", atTime(12, 0))
	if !ok || opens.Format("15:04") != "22:00" || opens.Day() != 11 {
		t.Fatalf("今天 22:00 应开闸: %v %v", opens.Format(time.RFC3339), ok)
	}
	// 同一天内的窗口，傍晚已过结束点：下一个开窗点是明天 09:00
	opens, ok = windowOpensAt("09:00", "18:00", atTime(20, 0))
	if !ok || opens.Day() != 12 || opens.Format("15:04") != "09:00" {
		t.Fatalf("应落到明天 09:00: %v %v", opens.Format(time.RFC3339), ok)
	}
	// 窗内与全天都不给开闸时间
	if _, ok := windowOpensAt("22:00", "06:00", atTime(2, 0)); ok {
		t.Fatal("窗内不应返回开闸时间")
	}
	if _, ok := windowOpensAt("08:00", "08:00", atTime(12, 0)); ok {
		t.Fatal("全天放行不应返回开闸时间")
	}
}

// TestWindowBlocksDispatch 窗外不启动新任务；假时钟走进窗内后由进度循环的
// tick 自动放行，不需要用户再点一次。
func TestWindowBlocksDispatch(t *testing.T) {
	current := atTime(12, 0)
	clock := func() time.Time { return current }

	content := newContent(t, 1000)
	srv := httptest.NewServer(&testServer{content: content, etag: `"w"`})
	defer srv.Close()

	m, err := NewManager(t.TempDir(), nopNotify, WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	// 窗口设在凌晨 00:00-06:00，假时钟的中午 12:00 在窗外
	if err := m.SetQueueWindow(true, "00:00", "06:00"); err != nil {
		t.Fatal(err)
	}
	task, err := m.AddTask(srv.URL, t.TempDir(), 1, "")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond) // 让真实 tick 至少走一轮
	if got := statusOf(m, task.ID); got != StatusQueued {
		t.Fatalf("窗外不应启动任务, got %s", got)
	}
	st := m.QueueStatus()
	if st.Active || !st.WindowEnabled || st.WindowActive || st.OpensAt == "" {
		t.Fatalf("窗外状态 = %+v", st)
	}
	if opens, err := time.Parse(time.RFC3339, st.OpensAt); err != nil || !opens.After(current) {
		t.Fatalf("OpensAt 应为未来的开闸时刻: %q err=%v", st.OpensAt, err)
	}

	// 时钟走进窗内（推到次日凌晨，保证假时钟单调，不倒着走）：下一个 tick 自动起跑
	current = atTime(3, 0).Add(24 * time.Hour)
	waitFor(t, func() bool { return statusOf(m, task.ID) == StatusCompleted }, 15*time.Second)
	if st := m.QueueStatus(); !st.Active || !st.WindowActive {
		t.Fatalf("窗内应放行: %+v", st)
	}
}

// TestWindowSurvivesRestartAndRejectsGarbage 时间窗是持久状态（与总闸同为跨重启语义）；
// 非法时刻直接报错而不是静默改成全天。
func TestWindowSurvivesRestartAndRejectsGarbage(t *testing.T) {
	dataDir := t.TempDir()
	m, err := NewManager(dataDir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	if err := m.SetQueueWindow(true, "6:0", ""); err == nil {
		t.Fatal("非法时间应报错")
	}
	if err := m.SetQueueWindow(true, "22:00", "06:00"); err != nil {
		t.Fatal(err)
	}
	// 补齐到两位，界面回读时不会拿到 "6:0" 这类不一致的写法
	if st := m.QueueStatus(); st.WindowStart != "22:00" || st.WindowEnd != "06:00" {
		t.Fatalf("窗口配置回读 = %q-%q", st.WindowStart, st.WindowEnd)
	}

	m2, err := NewManager(dataDir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m2)
	st := m2.QueueStatus()
	if !st.WindowEnabled || st.WindowStart != "22:00" || st.WindowEnd != "06:00" {
		t.Fatalf("重启后时间窗配置丢失: %+v", st)
	}
}

// TestWindowSettingKeepsOtherFields 时间窗写盘只改自己那几项：
// 期间别的字段被 SaveSettings 改过也不能被回写覆盖掉。
func TestWindowSettingKeepsOtherFields(t *testing.T) {
	m, err := NewManager(t.TempDir(), nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	st := m.GetSettings()
	st.Connections = 9
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	if err := m.SetQueueWindow(true, "01:00", "05:00"); err != nil {
		t.Fatal(err)
	}
	got := m.GetSettings()
	if got.Connections != 9 || !got.WindowEnabled || got.WindowStart != "01:00" {
		t.Fatalf("窗口设置覆盖了其他字段: %+v", got)
	}
}

// TestWindowDoesNotIdleQueue 窗外且仍有排队任务时不得发队列空闲事件：
// 收到该事件上层会执行「全部完成」动作（可能是关机）。
func TestWindowDoesNotIdleQueue(t *testing.T) {
	content := newContent(t, 500)
	srv := httptest.NewServer(&testServer{content: content, etag: `"i"`})
	defer srv.Close()

	current := atTime(12, 0)
	log := newEventLog()
	m, err := NewManager(t.TempDir(), log.notify, WithClock(func() time.Time { return current }))
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	// 先让一个任务跑完（窗未启用），再闭窗并留一个排队任务
	if _, err := m.AddTask(srv.URL, t.TempDir(), 1, "a.bin"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.GetTasks()[0].Status == StatusCompleted }, 10*time.Second)
	if err := m.SetQueueWindow(true, "00:00", "06:00"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddTask(srv.URL, t.TempDir(), 1, "b.bin"); err != nil {
		t.Fatal(err)
	}
	if c := log.count(EventQueueIdle); c != 1 {
		t.Fatalf("第一个任务结束时该发一次空闲事件, got %d", c)
	}
	time.Sleep(600 * time.Millisecond) // 窗外 tick 反复空转，不该再发第二次
	if c := log.count(EventQueueIdle); c != 1 {
		t.Fatalf("窗外有排队任务时不应再发空闲事件, got %d", c)
	}
}

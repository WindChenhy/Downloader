package engine

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPriorityStoredAndDispatchOrder(t *testing.T) {
	content := newContent(t, 1500)
	ts := &testServer{content: content, etag: `"a"`}
	ts.gate = make(chan struct{})
	srv := httptest.NewServer(ts)
	defer srv.Close()

	m, err := NewManager(t.TempDir(), nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	st := m.GetSettings()
	st.ConcurrentTasks = 1
	if err := m.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	saveDir := t.TempDir()

	// 占位任务先占满并发槽并阻塞
	if _, err := m.AddTaskWithChecksum(AddTaskParams{URL: srv.URL + "/block", SaveDir: saveDir, Connections: 1}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.GetTasks()[0].Status == StatusRunning }, 5*time.Second)

	if _, err := m.AddTaskWithChecksum(AddTaskParams{URL: srv.URL + "/low", SaveDir: saveDir, Connections: 1, Priority: PriorityLow}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddTaskWithChecksum(AddTaskParams{URL: srv.URL + "/high", SaveDir: saveDir, Connections: 1, Priority: PriorityHigh}); err != nil {
		t.Fatal(err)
	}

	// 放行后，高优先应先于低优先进入 running
	ts.releaseGate()
	waitFor(t, func() bool {
		var highRun, lowRun bool
		for _, tk := range m.GetTasks() {
			if strings.Contains(tk.URL, "/high") && (tk.Status == StatusRunning || tk.Status == StatusCompleted) {
				highRun = true
			}
			if strings.Contains(tk.URL, "/low") && tk.Status == StatusRunning {
				lowRun = true
			}
		}
		return highRun && !lowRun
	}, 5*time.Second)
	// 等全部结束，避免测试目录清理失败
	waitFor(t, func() bool {
		for _, tk := range m.GetTasks() {
			if tk.Status == StatusRunning || tk.Status == StatusQueued {
				return false
			}
		}
		return true
	}, 15*time.Second)
}

func TestScheduledTaskNotStartedEarly(t *testing.T) {
	content := newContent(t, 800)
	srv := httptest.NewServer(&testServer{content: content, etag: `"b"`})
	defer srv.Close()

	m, saveDir := mustManager(t)
	future := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	tk, err := m.AddTaskWithChecksum(AddTaskParams{
		URL: srv.URL, SaveDir: saveDir, Connections: 1, StartAt: future,
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	got := m.GetTasks()[0]
	if got.Status != StatusQueued {
		t.Fatalf("定时未到点状态 = %s, want queued", got.Status)
	}
	if got.Downloaded != 0 {
		t.Fatal("定时任务不应已开始下载")
	}
	// 到点后应启动
	if err := m.SetTaskStartAt(tk.ID, time.Now().Add(-time.Second).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.GetTasks()[0].Status == StatusCompleted }, 10*time.Second)
}

func TestPerTaskSpeedLimitField(t *testing.T) {
	m, saveDir := mustManager(t)
	srv := httptest.NewServer(&testServer{content: newContent(t, 500), etag: `"c"`})
	defer srv.Close()
	tk, err := m.AddTaskWithChecksum(AddTaskParams{
		URL: srv.URL, SaveDir: saveDir, Connections: 1, SpeedLimit: 64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tk.SpeedLimit != 64*1024 {
		t.Fatalf("SpeedLimit = %d", tk.SpeedLimit)
	}
	if err := m.SetTaskSpeedLimit(tk.ID, 128*1024); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.GetTasks()[0].Status == StatusCompleted }, 10*time.Second)
	for _, cur := range m.GetTasks() {
		if cur.ID == tk.ID && cur.SpeedLimit != 128*1024 {
			t.Fatalf("更新后 SpeedLimit = %d", cur.SpeedLimit)
		}
	}
}

func TestMoveTaskOrder(t *testing.T) {
	content := newContent(t, 200)
	srv := httptest.NewServer(&testServer{content: content, etag: `"d"`})
	defer srv.Close()
	m, saveDir := mustManager(t)
	st := m.GetSettings()
	st.ConcurrentTasks = 1
	_ = m.SaveSettings(st)

	var ids []string
	for i := 0; i < 3; i++ {
		tk, err := m.AddTaskWithChecksum(AddTaskParams{
			URL: srv.URL + "/f" + string(rune('a'+i)), SaveDir: saveDir, Connections: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, tk.ID)
	}
	if err := m.MoveTask(ids[2], -1); err != nil {
		t.Fatal(err)
	}
	order := m.GetTasks()
	if order[2].ID != ids[1] || order[1].ID != ids[2] {
		t.Fatalf("移动后顺序不对: %v", []string{order[0].ID, order[1].ID, order[2].ID})
	}
	// 等到全部结束，避免 Windows TempDir 清理被占用文件
	waitFor(t, func() bool {
		for _, tk := range m.GetTasks() {
			if tk.Status == StatusRunning || tk.Status == StatusQueued {
				return false
			}
		}
		return true
	}, 15*time.Second)
}

func TestExportImportTasks(t *testing.T) {
	content := newContent(t, 400)
	srv := httptest.NewServer(&testServer{content: content, etag: `"e"`})
	defer srv.Close()
	m, saveDir := mustManager(t)
	if _, err := m.AddTaskWithChecksum(AddTaskParams{
		URL: srv.URL, SaveDir: saveDir, Connections: 2, Priority: PriorityHigh,
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.GetTasks()[0].Status == StatusCompleted }, 10*time.Second)

	data, err := m.ExportTasksJSON()
	if err != nil {
		t.Fatal(err)
	}
	var arr []Task
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 || arr[0].Priority != PriorityHigh {
		t.Fatalf("导出内容异常: %+v", arr)
	}

	// 新 Manager 导入（已完成任务应跳过）
	m2, err := NewManager(t.TempDir(), nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	res, err := m2.ImportTasksJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tasks) != 0 {
		t.Fatalf("已完成任务不应重新导入, got %d", len(res.Tasks))
	}

	// 构造未完成任务 JSON 再导入
	src := arr[0]
	src.Status = StatusPaused
	src.ID = "x"
	src.SpeedLimit = 1024
	raw, _ := json.Marshal([]Task{src})
	res2, err := m2.ImportTasksJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Tasks) != 1 {
		t.Fatalf("应导入 1 条, errors=%v", res2.Errors)
	}
	if res2.Tasks[0].Status != StatusPaused {
		t.Fatalf("导入后状态 = %s, want paused", res2.Tasks[0].Status)
	}
	if res2.Tasks[0].SpeedLimit != 1024 {
		t.Fatalf("导入后限速丢失: %d", res2.Tasks[0].SpeedLimit)
	}
	// 清理导入任务，避免目录占用
	for _, tk := range m2.GetTasks() {
		_ = m2.RemoveTask(tk.ID, false)
	}
}

func TestRunAfterCompleteActionUnknown(t *testing.T) {
	if err := RunAfterCompleteAction("nope", ""); err == nil || strings.Contains(err.Error(), "exit") {
		t.Fatalf("未知动作应报错, got %v", err)
	}
	if err := RunAfterCompleteAction(AfterCompleteNone, ""); err != nil {
		t.Fatal(err)
	}
}

package engine

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"downloader/sysaction"
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
	st.APIEnabled = false // 测试不真正监听端口
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
	st.APIEnabled = false // 测试不真正监听端口
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

	data, err := m.ExportTasksJSON(false)
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
	if err := sysaction.Run("nope", ""); err == nil || strings.Contains(err.Error(), "exit") {
		t.Fatalf("未知动作应报错, got %v", err)
	}
	if err := sysaction.Run(sysaction.None, ""); err != nil {
		t.Fatal(err)
	}
}

// taskOf 按 ID 取任务快照（用例内断言用）。
func taskOf(t *testing.T, m *Manager, id string) Task {
	t.Helper()
	for _, tk := range m.GetTasks() {
		if tk.ID == id {
			return tk
		}
	}
	t.Fatalf("任务不存在: %s", id)
	return Task{}
}

// TestSetTaskPriorityValidatesAndPersists 优先级 setter：非法值拒绝、不存在的任务
// 报 ErrTaskNotFound、合法值写入并跨重启保留。
// 已有 TestPriorityStoredAndDispatchOrder 只覆盖了「新建时给优先级」的调度效果。
func TestSetTaskPriorityValidatesAndPersists(t *testing.T) {
	dataDir := t.TempDir()
	m, err := NewManager(dataDir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	// 闭闸：任务停在 queued，不会去碰网络
	if err := m.SetQueueSuspended(true); err != nil {
		t.Fatal(err)
	}
	task, err := m.AddTask("http://127.0.0.1:1/a.zip", t.TempDir(), 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := task.Priority; got != PriorityLow {
		// 引擎侧缺省值是 0（低）：REST/批量新建不传 priority 时即落为低优先。
		// 前端「新建下载」对话框始终显式发送 1（普通），两条入口的缺省并不同源。
		t.Fatalf("引擎缺省优先级应为低（0）, got %d", got)
	}

	for _, bad := range []int{-1, PriorityHigh + 1} {
		if err := m.SetTaskPriority(task.ID, bad); err == nil {
			t.Fatalf("非法优先级 %d 应被拒绝", bad)
		}
	}
	if err := m.SetTaskPriority("no-such-id", PriorityLow); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("不存在的任务应 ErrTaskNotFound, got %v", err)
	}

	if err := m.SetTaskPriority(task.ID, PriorityLow); err != nil {
		t.Fatal(err)
	}
	if got := taskOf(t, m, task.ID).Priority; got != PriorityLow {
		t.Fatalf("优先级未生效, got %d", got)
	}

	// 落盘：重启后仍是低优先
	m.Stop()
	m2, err := NewManager(dataDir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m2)
	if got := taskOf(t, m2, task.ID).Priority; got != PriorityLow {
		t.Fatalf("重启后优先级丢失, got %d", got)
	}
}

// TestHasTaskWithURLAndTaskSaveDir 覆盖剪贴板去重与 API 连文件删除各自依赖的查询。
func TestHasTaskWithURLAndTaskSaveDir(t *testing.T) {
	m, saveDir := mustManager(t)
	if err := m.SetQueueSuspended(true); err != nil {
		t.Fatal(err)
	}
	const url = "http://127.0.0.1:1/dup.zip"
	task, err := m.AddTask(url, saveDir, 1, "")
	if err != nil {
		t.Fatal(err)
	}

	if !m.HasTaskWithURL(url) {
		t.Fatal("相同 URL 应被识别为已存在（剪贴板去重）")
	}
	if m.HasTaskWithURL(url + "?v=2") {
		t.Fatal("不同 URL 不应被误判为重复")
	}

	dir, ok := m.TaskSaveDir(task.ID)
	if !ok || dir == "" {
		t.Fatalf("TaskSaveDir = (%q, %v), want 有值", dir, ok)
	}
	if _, ok := m.TaskSaveDir("no-such-id"); ok {
		t.Fatal("不存在的任务 TaskSaveDir 应 ok=false")
	}
}

// TestMoveTaskRejectsBadDeltaAndMissingTask 上下移的入参校验与边界。
// 已有 TestMoveTaskOrder 覆盖正常换位，这里补错误分支。
func TestMoveTaskRejectsBadDeltaAndMissingTask(t *testing.T) {
	m, saveDir := mustManager(t)
	if err := m.SetQueueSuspended(true); err != nil {
		t.Fatal(err)
	}
	task, err := m.AddTask("http://127.0.0.1:1/a.zip", saveDir, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.MoveTask(task.ID, 0); err == nil {
		t.Fatal("delta 不是 -1/+1 应报错")
	}
	if err := m.MoveTask("no-such-id", -1); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("不存在的任务应 ErrTaskNotFound, got %v", err)
	}
	// 只有一个任务时上移已在边界：静默成功，不报错
	if err := m.MoveTask(task.ID, -1); err != nil {
		t.Fatalf("边界移动应静默成功, got %v", err)
	}
}

// TestSetTaskSpeedLimitClampsNegative 负数被夹到 0（跟随全局），不写入非法值。
func TestSetTaskSpeedLimitClampsNegative(t *testing.T) {
	m, saveDir := mustManager(t)
	if err := m.SetQueueSuspended(true); err != nil {
		t.Fatal(err)
	}
	task, err := m.AddTask("http://127.0.0.1:1/a.zip", saveDir, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetTaskSpeedLimit(task.ID, -5); err != nil {
		t.Fatal(err)
	}
	if got := taskOf(t, m, task.ID).SpeedLimit; got != 0 {
		t.Fatalf("负数限速应夹到 0（不限）, got %d", got)
	}
	if err := m.SetTaskSpeedLimit("no-such-id", 1024); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("不存在的任务应 ErrTaskNotFound, got %v", err)
	}
}

// TestSetTaskStartAtValidatesAndApplies 定时开始：非法时间串报错且不改任务，
// 空串表示立即（零值），合法 RFC3339 落到 StartAt。
func TestSetTaskStartAtValidatesAndApplies(t *testing.T) {
	m, saveDir := mustManager(t)
	if err := m.SetQueueSuspended(true); err != nil {
		t.Fatal(err)
	}
	task, err := m.AddTask("http://127.0.0.1:1/a.zip", saveDir, 1, "")
	if err != nil {
		t.Fatal(err)
	}

	if err := m.SetTaskStartAt(task.ID, "not-a-time"); err == nil {
		t.Fatal("非法时间串应报错")
	}
	if err := m.SetTaskStartAt("no-such-id", ""); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("不存在的任务应 ErrTaskNotFound, got %v", err)
	}

	when := time.Now().Add(time.Hour).Truncate(time.Second)
	if err := m.SetTaskStartAt(task.ID, when.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if got := taskOf(t, m, task.ID).StartAt; !got.Equal(when) {
		t.Fatalf("定时未生效: got %v want %v", got, when)
	}
	// 清空 → 回到立即
	if err := m.SetTaskStartAt(task.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got := taskOf(t, m, task.ID).StartAt; !got.IsZero() {
		t.Fatalf("清空定时应回到零值, got %v", got)
	}
}

package main

import (
	"errors"
	"testing"
	"time"

	"downloader/engine"
)

// 根包（应用层）此前没有任何用例。这里覆盖三类纯逻辑且不触碰 Wails/托盘运行时的行为：
//   1. 引擎未初始化时所有绑定方法的 nil 守卫（startup 里 NewManager 失败后的那段窗口）；
//   2. 通知开关与语言文案的分派；
//   3. 剪贴板监听的停机通道。
// 注意：Quit / OpenFolder / shutdown 会分别调用托盘、文件管理器与 systray，
// 不在单测里触发。

// TestBindingsReturnNotReadyWithoutManager startup 中 NewManager 失败时 a.mgr 保持 nil，
// 随后的 runtime.Quit 生效前，前端任何一次调用都必须安全返回而不是 nil 解引用 panic。
func TestBindingsReturnNotReadyWithoutManager(t *testing.T) {
	a := NewApp() // ctx / mgr 均为 nil，等价于初始化失败后的状态

	if got := a.GetTasks(); got != nil {
		t.Fatalf("GetTasks 应返回 nil, got %v", got)
	}
	if _, err := a.AddTask("https://example.com/a.zip", "", 0, ""); !errors.Is(err, errNotReady) {
		t.Fatalf("AddTask 应返回 errNotReady, got %v", err)
	}
	if got := a.AddTasks(nil); got.Tasks != nil || got.Errors != nil {
		t.Fatalf("AddTasks 应返回零值, got %+v", got)
	}
	for name, err := range map[string]error{
		"PauseTask":         a.PauseTask("x"),
		"ResumeTask":        a.ResumeTask("x"),
		"RemoveTask":        a.RemoveTask("x", true),
		"SetQueueSuspended": a.SetQueueSuspended(true),
		"SetQueueWindow":    a.SetQueueWindow(true, "22:00", "06:00"),
		"SetTaskPriority":   a.SetTaskPriority("x", 2),
		"MoveTask":          a.MoveTask("x", -1),
		"SetTaskSpeedLimit": a.SetTaskSpeedLimit("x", 1024),
		"SetTaskStartAt":    a.SetTaskStartAt("x", ""),
		"SaveSettings":      a.SaveSettings(engine.Settings{}),
		"ApplyCloseAction":  a.ApplyCloseAction("exit", false),
	} {
		if !errors.Is(err, errNotReady) {
			t.Fatalf("%s 应返回 errNotReady, got %v", name, err)
		}
	}
	if got := a.PauseAll(); got != 0 {
		t.Fatalf("PauseAll = %d, want 0", got)
	}
	if got := a.ResumeAll(); got != 0 {
		t.Fatalf("ResumeAll = %d, want 0", got)
	}
	if got := a.RetryAllFailed(); got != 0 {
		t.Fatalf("RetryAllFailed = %d, want 0", got)
	}
	if got := a.ClearCompleted(); got != 0 {
		t.Fatalf("ClearCompleted = %d, want 0", got)
	}
	if got := a.QueueStatus(); got.Active || got.Suspended {
		t.Fatalf("QueueStatus 应为零值, got %+v", got)
	}
	if got := a.GetStats(); got.TaskCount != 0 || got.TodayBytes != 0 {
		t.Fatalf("GetStats 应为零值, got %+v", got)
	}
	if _, ok := a.GetTaskStats("x"); ok {
		t.Fatal("GetTaskStats 应返回 ok=false")
	}
	if _, err := a.ExportTasksJSON(true); !errors.Is(err, errNotReady) {
		t.Fatalf("ExportTasksJSON 应返回 errNotReady, got %v", err)
	}
	if got := a.ImportTasksJSON("[]"); len(got.Errors) != 1 {
		t.Fatalf("ImportTasksJSON 应带一条 errNotReady 说明, got %+v", got)
	}
	if got := a.GetSettings(); got.APIPort != 0 {
		t.Fatalf("GetSettings 应为零值, got %+v", got)
	}
	// ctx 为 nil：不碰 Wails 运行时，直接返回
	a.ShowWindow()
}

// TestNotifyGatingByKind 通知按事件类别分派：默认全关、未知类别返回 false（不静默弹窗）、
// 各类开关互不串台。对应的评审点是「用字符串字面量做分支、拼错静默失效」。
func TestNotifyGatingByKind(t *testing.T) {
	a := NewApp()

	for _, kind := range []string{notifyKindCreate, notifyKindPause, notifyKindComplete, notifyKindFail} {
		if a.notifyEnabled(kind) {
			t.Fatalf("默认不应开启 %s", kind)
		}
	}
	if a.notifyEnabled("completed") { // 近似但拼错的类别
		t.Fatal("未知类别必须返回 false，而不是落到某个默认开启的分支")
	}

	a.notifyComplete.Store(true)
	if !a.notifyEnabled(notifyKindComplete) {
		t.Fatal("开启后对应类别应为 true")
	}
	if a.notifyEnabled(notifyKindFail) {
		t.Fatal("开关不应串台")
	}
}

// TestLanguageTextDefaults 语言与文案缓存：默认中文，切到 en-US 后取英文文案。
func TestLanguageTextDefaults(t *testing.T) {
	a := NewApp()
	if got := a.uiLanguage(); got != "zh-CN" {
		t.Fatalf("默认语言 = %q, want zh-CN", got)
	}
	if got := a.text("中文文案", "english text"); got != "中文文案" {
		t.Fatalf("默认应取中文文案, got %q", got)
	}
	a.uiLang.Store("en-US")
	if got := a.text("中文文案", "english text"); got != "english text" {
		t.Fatalf("切英文后应取英文文案, got %q", got)
	}
	// mgr 为 nil 时同步开关直接返回
	a.syncNotifyFlags()
}

// TestNotifyTaskFinishedBranches 完成/失败通知按状态与开关分流。
// ctx 为 nil 时 sendSysNotification 会短路，因此用例不会真的弹系统通知。
func TestNotifyTaskFinishedBranches(t *testing.T) {
	a := NewApp()
	a.mgr = nil
	done := engine.Task{ID: "t1", FileName: "f.bin", Status: engine.StatusCompleted}
	failed := engine.Task{ID: "t2", FileName: "g.bin", Status: engine.StatusFailed, Error: "boom"}
	other := engine.Task{ID: "t3", FileName: "h.bin", Status: engine.StatusPaused}

	// 开关全关：不应弹任何通知（不 panic 即通过）
	a.notifyTaskFinished(done)
	a.notifyTaskFinished(failed)
	a.notifyTaskFinished(other) // 既非完成也非失败：直接 return
	a.notifyTaskCreated(done)
	a.notifyTaskPaused(done)

	// 打开开关走完发送分支（ctx=nil 使其短路）
	a.notifyComplete.Store(true)
	a.notifyFail.Store(true)
	a.notifyCreate.Store(true)
	a.notifyPause.Store(true)
	a.notifyTaskFinished(done)
	a.notifyTaskFinished(failed)
	a.notifyTaskFinished(other)
	a.notifyTaskCreated(done)
	a.notifyTaskPaused(done)
}

// TestClipboardStopIsIdempotentAndStopsLoop 停机通道：关闭后监听循环必须退出，
// 且重复调用不 panic（sync.Once 保证只关一次）。
func TestClipboardStopIsIdempotentAndStopsLoop(t *testing.T) {
	a := NewApp()
	done := make(chan struct{})
	go func() {
		a.watchClipboard() // mgr 为 nil：每轮 continue，收到停机信号即返回
		close(done)
	}()

	a.stopClipboard()
	select {
	case <-a.clipboardStop:
	default:
		t.Fatal("stopClipboard 应关闭停机通道")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("停机后剪贴板监听循环应退出")
	}
	// 幂等：再次调用不 panic
	a.stopClipboard()
}

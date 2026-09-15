package main

import (
	"downloader/engine"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// syncNotifyFlags 将设置中的通知开关同步到原子缓存。
// 必须在 Manager 锁外调用；通知回调里只读原子值，禁止再进 GetSettings。
func (a *App) syncNotifyFlags() {
	if a.mgr == nil {
		return
	}
	st := a.mgr.GetSettings()
	a.notifyCreate.Store(st.NotifyOnCreate)
	a.notifyPause.Store(st.NotifyOnPause)
	a.notifyComplete.Store(st.NotifyOnComplete)
	a.notifyFail.Store(st.NotifyOnFail)
}

// notifyEnabled 读取缓存的通知开关（可在 notify 回调中安全调用）。
func (a *App) notifyEnabled(kind string) bool {
	switch kind {
	case "create":
		return a.notifyCreate.Load()
	case "pause":
		return a.notifyPause.Load()
	case "complete":
		return a.notifyComplete.Load()
	case "fail":
		return a.notifyFail.Load()
	default:
		return false
	}
}

// sendSysNotification 发送 Windows 系统通知；不可用时静默忽略。
func (a *App) sendSysNotification(id, title, body string) {
	if a.ctx == nil || !runtime.IsNotificationAvailable(a.ctx) {
		return
	}
	_ = runtime.SendNotification(a.ctx, runtime.NotificationOptions{
		ID:    id,
		Title: title,
		Body:  body,
	})
}

// notifyTaskCreated 创建下载任务时弹系统通知（默认关闭）。
func (a *App) notifyTaskCreated(t engine.Task) {
	if !a.notifyEnabled("create") {
		return
	}
	a.sendSysNotification("task-created-"+t.ID, "已创建下载任务", t.FileName)
}

// notifyTaskPaused 下载任务暂停时弹系统通知（默认关闭）。
func (a *App) notifyTaskPaused(t engine.Task) {
	if !a.notifyEnabled("pause") {
		return
	}
	a.sendSysNotification("task-paused-"+t.ID, "下载已暂停", t.FileName)
}

// notifyTaskFinished 任务完成/失败时弹系统通知（默认开启）。
func (a *App) notifyTaskFinished(t engine.Task) {
	if t.Status == engine.StatusFailed {
		if !a.notifyEnabled("fail") {
			return
		}
		body := t.FileName
		if t.Error != "" {
			body += "：" + t.Error
		}
		a.sendSysNotification("task-finished-"+t.ID, "下载失败", body)
		return
	}
	if t.Status != engine.StatusCompleted {
		return
	}
	if !a.notifyEnabled("complete") {
		return
	}
	a.sendSysNotification("task-finished-"+t.ID, "下载完成", t.FileName)
}

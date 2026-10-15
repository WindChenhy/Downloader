package main

import (
	"downloader/engine"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// syncNotifyFlags 将设置中的通知开关与界面语言同步到原子缓存。
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
	a.uiLang.Store(st.Language)
}

// uiLanguage 返回缓存的界面语言（zh-CN / en-US）；通知回调内可安全调用。
func (a *App) uiLanguage() string {
	if v, ok := a.uiLang.Load().(string); ok && v != "" {
		return v
	}
	return "zh-CN"
}

// text 按当前界面语言取文案：en 为英文文案，zh 为默认中文文案。
func (a *App) text(zh, en string) string {
	if a.uiLanguage() == "en-US" {
		return en
	}
	return zh
}

// 通知类别。用常量而不是字符串字面量：拼错时编译期就报错，
// 而不是静默落进 default 分支，让某类通知永远弹不出来还查不到原因。
const (
	notifyKindCreate   = "create"
	notifyKindPause    = "pause"
	notifyKindComplete = "complete"
	notifyKindFail     = "fail"
)

// notifyEnabled 读取缓存的通知开关（可在 notify 回调中安全调用）。
func (a *App) notifyEnabled(kind string) bool {
	switch kind {
	case notifyKindCreate:
		return a.notifyCreate.Load()
	case notifyKindPause:
		return a.notifyPause.Load()
	case notifyKindComplete:
		return a.notifyComplete.Load()
	case notifyKindFail:
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
	if !a.notifyEnabled(notifyKindCreate) {
		return
	}
	a.sendSysNotification("task-created-"+t.ID, a.text("已创建下载任务", "Download task created"), t.FileName)
}

// notifyTaskPaused 下载任务暂停时弹系统通知（默认关闭）。
func (a *App) notifyTaskPaused(t engine.Task) {
	if !a.notifyEnabled(notifyKindPause) {
		return
	}
	a.sendSysNotification("task-paused-"+t.ID, a.text("下载已暂停", "Download paused"), t.FileName)
}

// notifyTaskFinished 任务完成/失败时弹系统通知（默认开启）。
func (a *App) notifyTaskFinished(t engine.Task) {
	if t.Status == engine.StatusFailed {
		if !a.notifyEnabled(notifyKindFail) {
			return
		}
		body := t.FileName
		if t.Error != "" {
			body += ": " + t.Error
		}
		a.sendSysNotification("task-finished-"+t.ID, a.text("下载失败", "Download failed"), body)
		return
	}
	if t.Status != engine.StatusCompleted {
		return
	}
	if !a.notifyEnabled(notifyKindComplete) {
		return
	}
	a.sendSysNotification("task-finished-"+t.ID, a.text("下载完成", "Download completed"), t.FileName)
}

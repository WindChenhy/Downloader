package main

import (
	"downloader/engine"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

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

// notifyTaskCreated 创建下载任务时弹系统通知。
func (a *App) notifyTaskCreated(t engine.Task) {
	a.sendSysNotification("task-created-"+t.ID, "已创建下载任务", t.FileName)
}

// notifyTaskPaused 下载任务暂停时弹系统通知。
func (a *App) notifyTaskPaused(t engine.Task) {
	a.sendSysNotification("task-paused-"+t.ID, "下载已暂停", t.FileName)
}

// notifyTaskFinished 任务完成/失败时弹系统通知。
func (a *App) notifyTaskFinished(t engine.Task) {
	title := "下载完成"
	body := t.FileName
	if t.Status == engine.StatusFailed {
		title = "下载失败"
		if t.Error != "" {
			body += "：" + t.Error
		}
	}
	a.sendSysNotification("task-finished-"+t.ID, title, body)
}

package main

import (
	"downloader/engine"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// notifyTaskFinished 任务完成/失败时弹系统通知。
func (a *App) notifyTaskFinished(t engine.Task) {
	if !runtime.IsNotificationAvailable(a.ctx) {
		return
	}
	opts := runtime.NotificationOptions{
		ID:    "task-" + t.ID,
		Title: "下载完成",
		Body:  t.FileName,
	}
	if t.Status == engine.StatusFailed {
		opts.Title = "下载失败"
		opts.Body = t.FileName
		if t.Error != "" {
			opts.Body += "：" + t.Error
		}
	}
	_ = runtime.SendNotification(a.ctx, opts)
}

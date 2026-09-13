package main

import (
	"strings"
	"time"

	"downloader/engine"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// watchClipboard 轮询剪贴板：发现新的可下载链接时向前端推送
// clipboard:url 事件，由前端弹预填好的新建下载对话框。
func (a *App) watchClipboard() {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	var last string
	for range ticker.C {
		if a.mgr == nil {
			continue
		}
		if !a.mgr.GetSettings().ClipboardWatch {
			continue
		}
		text, err := runtime.ClipboardGetText(a.ctx)
		if err != nil {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" || text == last {
			continue
		}
		last = text
		if !engine.IsDownloadableURL(text) {
			continue
		}
		if a.mgr.HasTaskWithURL(text) {
			continue
		}
		runtime.EventsEmit(a.ctx, "clipboard:url", text)
	}
}

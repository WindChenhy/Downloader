package main

import (
	"downloader/engine"
)

// syncNotifyFlags 将设置中的通知开关与界面语言同步到原子缓存。
// 必须在 Manager 锁外调用；通知回调里只读原子值，禁止再进 GetSettings。
func (s *AppService) syncNotifyFlags() {
	if s.mgr == nil {
		return
	}
	st := s.mgr.GetSettings()
	s.notifyCreate.Store(st.NotifyOnCreate)
	s.notifyPause.Store(st.NotifyOnPause)
	s.notifyComplete.Store(st.NotifyOnComplete)
	s.notifyFail.Store(st.NotifyOnFail)
	s.uiLang.Store(st.Language)
}

// uiLanguage 返回缓存的界面语言（zh-CN / en-US）；通知回调内可安全调用。
func (s *AppService) uiLanguage() string {
	if v, ok := s.uiLang.Load().(string); ok && v != "" {
		return v
	}
	return "zh-CN"
}

// text 按当前界面语言取文案：en 为英文文案，zh 为默认中文文案。
func (s *AppService) text(zh, en string) string {
	if s.uiLanguage() == "en-US" {
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
func (s *AppService) notifyEnabled(kind string) bool {
	switch kind {
	case notifyKindCreate:
		return s.notifyCreate.Load()
	case notifyKindPause:
		return s.notifyPause.Load()
	case notifyKindComplete:
		return s.notifyComplete.Load()
	case notifyKindFail:
		return s.notifyFail.Load()
	default:
		return false
	}
}

// sendSysNotification 发送系统通知；通知后端不可用时静默忽略。
func (s *AppService) sendSysNotification(id, title, body string) {
	if s.notifier == nil {
		return
	}
	s.notifier.send(id, title, body)
}

// notifyTaskCreated 创建下载任务时弹系统通知（默认关闭）。
func (s *AppService) notifyTaskCreated(t engine.Task) {
	if !s.notifyEnabled(notifyKindCreate) {
		return
	}
	s.sendSysNotification("task-created-"+t.ID, s.text("已创建下载任务", "Download task created"), t.FileName)
}

// notifyTaskPaused 下载任务暂停时弹系统通知（默认关闭）。
func (s *AppService) notifyTaskPaused(t engine.Task) {
	if !s.notifyEnabled(notifyKindPause) {
		return
	}
	s.sendSysNotification("task-paused-"+t.ID, s.text("下载已暂停", "Download paused"), t.FileName)
}

// notifyTaskFinished 任务完成/失败时弹系统通知（默认开启）。
func (s *AppService) notifyTaskFinished(t engine.Task) {
	if t.Status == engine.StatusFailed {
		if !s.notifyEnabled(notifyKindFail) {
			return
		}
		body := t.FileName
		if t.Error != "" {
			body += ": " + t.Error
		}
		s.sendSysNotification("task-finished-"+t.ID, s.text("下载失败", "Download failed"), body)
		return
	}
	if t.Status != engine.StatusCompleted {
		return
	}
	if !s.notifyEnabled(notifyKindComplete) {
		return
	}
	s.sendSysNotification("task-finished-"+t.ID, s.text("下载完成", "Download completed"), t.FileName)
}

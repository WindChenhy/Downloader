package engine

import (
	"fmt"
	"time"
)

// 队列级批量操作。
// 与逐个调用单任务方法的区别只有一处：一次加锁、一次写 tasks.json、一次推送列表。
// 单任务路径每次都全量快照并 emitTasks，50 个任务循环调用会产生 50 次事件风暴
// 和 50 次磁盘写，因此这里必须自带批量骨架，不能在 UI 层循环调用 PauseTask。

// PauseAll 暂停全部未结束的任务：排队中的立即置为 paused，运行中的发出取消信号
// （其状态在该轮下载退出时落定）。返回发出暂停动作的活跃任务数（运行中的任务
// 可能恰好下载完成而非暂停，计数含它们是「已请求暂停」的口径）。
func (m *Manager) PauseAll() int {
	m.mu.Lock()
	n := 0
	for _, id := range m.order {
		h := m.handles[id]
		switch h.task.Status {
		case StatusQueued, StatusRunning:
			n++
		}
		m.pauseLocked(h)
	}
	if n == 0 {
		m.mu.Unlock()
		return n // 没有可暂停的任务：不落盘也不推送
	}
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.emitTasks(snap)
	return n
}

// resumeAllMatching 把匹配到的任务重新入队；onlyFailed 为 true 时只处理失败任务。
// 返回被重新入队的任务数。
func (m *Manager) resumeAllMatching(onlyFailed bool) int {
	m.mu.Lock()
	n := 0
	for _, id := range m.order {
		h := m.handles[id]
		match := h.task.Status == StatusFailed || (!onlyFailed && h.task.Status == StatusPaused)
		if !match {
			continue
		}
		h.task.Status = StatusQueued
		h.task.Error = ""
		n++
	}
	if n > 0 {
		// 重新入队视为新的一轮：队列此前可能已空过（空闲事件发过、标志置位），
		// 不重置的话这批任务跑完不会再触发「全部完成动作」，与重新添加任务不一致
		m.queueIdleFired = false
		m.dispatchLocked() // 总闸关闭时这里不会启动任何任务，任务停在 queued
		m.persistTasksLocked()
		snap := m.snapshotLocked()
		m.mu.Unlock()
		m.emitTasks(snap)
		return n
	}
	m.mu.Unlock()
	return n
}

// ResumeAll 恢复全部已暂停与已失败的任务。
// 含失败任务：界面按钮语义是「全部开始」，逐个再点重试是原行为。
// 需要只处理失败项时用 RetryAllFailed。
func (m *Manager) ResumeAll() int { return m.resumeAllMatching(false) }

// RetryAllFailed 重试全部失败的任务（不影响已暂停的任务）。
func (m *Manager) RetryAllFailed() int { return m.resumeAllMatching(true) }

// ClearCompleted 移除全部已完成任务的记录。
// 磁盘上的文件保留（与单任务「仅删除记录」一致）；记录摘除与残留文件清理
// 复用 RemoveTask 的同一条路径（dropLocked + cleanupTaskFiles），区别只是
// 批量汇总成一次落盘与一次推送。返回被移除的记录数。
func (m *Manager) ClearCompleted() int {
	m.mu.Lock()
	var done []Task
	for _, id := range m.order {
		if h := m.handles[id]; h.task.Status == StatusCompleted {
			done = append(done, h.task)
		}
	}
	for _, t := range done {
		m.dropLocked(t.ID)
	}
	n := len(done)
	if n == 0 {
		m.mu.Unlock()
		return n // 没有已完成记录：不落盘也不推送
	}
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.emitTasks(snap)
	for _, t := range done {
		// 已完成任务在收尾时已删掉 sidecar 与暂存文件，这里只是与单任务
		// 删除保持同一兜底处理（异常遗留的孤儿 part 不会因清理记录而驻留）
		m.cleanupTaskFiles(t)
	}
	return n
}

// SetQueueSuspended 开关队列总闸。
// 闭闸只阻止后续启动，不暂停正在下载的任务——要连正在下的一起停请再调 PauseAll。
// 开闸时立即把已排队任务按优先级放出去。状态持久化，重启后保持。
func (m *Manager) SetQueueSuspended(v bool) error {
	m.mu.Lock()
	if m.settings.QueueSuspended == v {
		m.mu.Unlock()
		return nil
	}
	next := m.settings
	next.QueueSuspended = v
	m.mu.Unlock()
	// 写盘不持锁：与 SaveSettings 一致，避免磁盘 IO 期间阻塞进度循环
	if err := m.store.SaveSettings(next); err != nil {
		return err
	}
	m.mu.Lock()
	// 只回写闸门这一位：期间可能有 SaveSettings 改了其他字段
	inmem := m.settings
	inmem.QueueSuspended = v
	m.settings = inmem
	if !v {
		m.dispatchLocked()
	}
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.emitTasks(snap)
	return nil
}

// QueueStatus 返回队列此刻的放行状态：用户总闸、带宽时间窗、被空间拦住的任务数。
// 三者都会造成「有排队任务却不跑」，界面必须说清是哪一种，否则只能看到卡在队列里。
func (m *Manager) QueueStatus() QueueStatus {
	m.mu.Lock()
	now := m.now()
	st := m.settings
	s := QueueStatus{
		Active:        m.queueActiveLocked(now),
		Suspended:     st.QueueSuspended,
		WindowEnabled: st.WindowEnabled,
		WindowActive:  m.windowActiveLocked(now),
		WindowStart:   st.WindowStart,
		WindowEnd:     st.WindowEnd,
	}
	for _, id := range m.order {
		h := m.handles[id]
		if h.task.Status == StatusQueued && h.task.BlockReason != "" {
			s.BlockedTasks++
		}
	}
	m.mu.Unlock()
	if s.WindowEnabled && !s.WindowActive {
		if opens, ok := windowOpensAt(s.WindowStart, s.WindowEnd, now); ok {
			s.OpensAt = opens.Format(time.RFC3339)
		}
	}
	return s
}

// SetQueueWindow 配置带宽时间窗。起止时间必须是 HH:MM，非法直接报错（表单路径，
// 不像 normalize 那样静默回落）；窗外只阻止新任务启动，正在下载的不受影响。
func (m *Manager) SetQueueWindow(enabled bool, start, end string) error {
	s, ok := parseHHMM(start)
	if !ok {
		return fmt.Errorf("开始时间格式应为 HH:MM，当前为 %q", start)
	}
	e, ok := parseHHMM(end)
	if !ok {
		return fmt.Errorf("结束时间格式应为 HH:MM，当前为 %q", end)
	}
	m.mu.Lock()
	next := m.settings
	next.WindowEnabled = enabled
	next.WindowStart = hhmm(s)
	next.WindowEnd = hhmm(e)
	m.mu.Unlock()
	// 与 SetQueueSuspended 一致：写盘不持锁，回来只合并时间窗这几项
	if err := m.store.SaveSettings(next); err != nil {
		return err
	}
	m.mu.Lock()
	cur := m.settings
	cur.WindowEnabled = next.WindowEnabled
	cur.WindowStart = next.WindowStart
	cur.WindowEnd = next.WindowEnd
	m.settings = cur
	// 可能正好把窗口开到当下：立刻把排队的放出去
	m.dispatchLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.emitTasks(snap)
	return nil
}

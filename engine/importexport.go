package engine

import (
	"encoding/json"
	"fmt"
	"time"
)

// ExportTasksJSON 导出当前任务列表为 JSON（用于备份/迁移）。
// 会清零瞬时 Speed，避免导入端误读。
// withAuth 为 false 时剥离每任务的 Cookie/Referer：这两项常常就是会话凭据，
// 默认不随任务包外流（导出的 JSON 可能被贴进聊天窗口或提交到仓库）。
func (m *Manager) ExportTasksJSON(withAuth bool) ([]byte, error) {
	tasks := m.GetTasks()
	for i := range tasks {
		tasks[i].Speed = 0
		if !withAuth {
			tasks[i].Cookie = ""
			tasks[i].Referer = ""
		}
	}
	return json.MarshalIndent(tasks, "", "  ")
}

// ImportTasksJSON 从 JSON 导入任务。
// - 已完成：跳过，不重新下载
// - 暂停/失败/排队：按源意图重建；暂停态建成就停在暂停，不会先跑起来
// 返回新建的任务与逐条错误说明。
//
// 与旧实现的两处差别：
//  1. 一次加锁、一次写 tasks.json、一次推列表快照。原来逐条调
//     AddTaskWithChecksum，导入上百条就是上百次全量写盘加上百次列表事件。
//  2. 暂停态在建任务时就置为 paused（PauseImmediately），不再「建完再暂停」。
//     原来是先被 dispatch 拉起来跑一段、取消后再忙轮询最多 3 秒等状态落定，
//     导入几十条暂停任务能把界面卡住上百秒。
func (m *Manager) ImportTasksJSON(data []byte) (BatchAddResult, error) {
	var items []Task
	if err := json.Unmarshal(data, &items); err != nil {
		return BatchAddResult{}, fmt.Errorf("解析任务列表失败: %w", err)
	}

	// 待新建条目。idx 保留原始下标，让错误说明里的「第 N 条」与输入对得上。
	type pending struct {
		idx    int
		params AddTaskParams
	}
	var (
		out      BatchAddResult
		pendings []pending
	)
	// 先做不持锁的准备工作：SaveDirAllowed 内部要加锁，
	// 不能在批量新建的临界区里调用（Go 的 Mutex 不可重入）
	for i, src := range items {
		if src.URL == "" {
			out.Errors = append(out.Errors, fmt.Sprintf("第 %d 条缺少 url", i+1))
			continue
		}
		if src.Status == StatusCompleted {
			continue // 已完成：跳过，不重新下载
		}
		// 导入文件内容不可信：目录不在白名单内时收敛到默认下载目录，
		// 防止借"任务包"向任意目录写文件
		saveDir := src.SaveDir
		if saveDir != "" && !m.SaveDirAllowed(saveDir) {
			saveDir = ""
		}
		startAt := ""
		if !src.StartAt.IsZero() {
			startAt = src.StartAt.Format(time.RFC3339)
		}
		pendings = append(pendings, pending{idx: i, params: AddTaskParams{
			URL:              src.URL,
			SaveDir:          saveDir,
			Connections:      src.Connections,
			CustomName:       src.CustomName,
			ChecksumAlgo:     src.ChecksumAlgo,
			ChecksumExpected: src.ChecksumExpected,
			Priority:         src.Priority,
			StartAt:          startAt,
			SpeedLimit:       src.SpeedLimit,
			Cookie:           src.Cookie,
			Referer:          src.Referer,
			// 源为暂停：建成就停在暂停，不进调度
			PauseImmediately: src.Status == StatusPaused,
		}})
	}

	m.mu.Lock()
	for _, pd := range pendings {
		u, err := validateURL(pd.params.URL)
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("第 %d 条（%s）: %v",
				pd.idx+1, truncateURL(pd.params.URL), err))
			continue
		}
		t, err := m.addTaskLocked(u, pd.params)
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("第 %d 条（%s）: %v",
				pd.idx+1, truncateURL(pd.params.URL), err))
			continue
		}
		out.Tasks = append(out.Tasks, t)
	}
	if len(out.Tasks) == 0 {
		m.mu.Unlock()
		return out, nil // 一条都没建成：不落盘也不推送
	}
	m.persistTasksLocked()
	snap := m.snapshotLocked()
	m.mu.Unlock()
	m.emitTasks(snap)
	for _, t := range out.Tasks {
		m.emitTask(EventTaskCreated, t)
	}
	return out, nil
}

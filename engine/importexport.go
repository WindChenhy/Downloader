package engine

import (
	"encoding/json"
	"fmt"
	"time"
)

// ExportTasksJSON 导出当前任务列表为 JSON（用于备份/迁移）。
// 会清零瞬时 Speed，避免导入端误读。
func (m *Manager) ExportTasksJSON() ([]byte, error) {
	tasks := m.GetTasks()
	for i := range tasks {
		tasks[i].Speed = 0
	}
	return json.MarshalIndent(tasks, "", "  ")
}

// ImportTasksJSON 从 JSON 导入任务。
// - 已完成：跳过，不重新下载
// - 暂停/失败/排队：按源意图重建（暂停态会再暂停一次）
// 返回新建的任务与逐条错误说明。
func (m *Manager) ImportTasksJSON(data []byte) (BatchAddResult, error) {
	var items []Task
	if err := json.Unmarshal(data, &items); err != nil {
		return BatchAddResult{}, fmt.Errorf("解析任务列表失败: %w", err)
	}
	var out BatchAddResult
	for i, src := range items {
		if src.URL == "" {
			out.Errors = append(out.Errors, fmt.Sprintf("第 %d 条缺少 url", i+1))
			continue
		}
		if src.Status == StatusCompleted {
			continue
		}
		startAt := ""
		if !src.StartAt.IsZero() {
			startAt = src.StartAt.Format(time.RFC3339)
		}
		t, err := m.AddTaskWithChecksum(AddTaskParams{
			URL:              src.URL,
			SaveDir:          src.SaveDir,
			Connections:      src.Connections,
			CustomName:       src.CustomName,
			ChecksumAlgo:     src.ChecksumAlgo,
			ChecksumExpected: src.ChecksumExpected,
			Priority:         src.Priority,
			StartAt:          startAt,
			SpeedLimit:       src.SpeedLimit,
		})
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("第 %d 条（%s）: %v", i+1, truncateURL(src.URL), err))
			continue
		}
		// 源为暂停：导入后立刻再暂停，避免抢跑
		if src.Status == StatusPaused {
			_ = m.PauseTask(t.ID)
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				for _, cur := range m.GetTasks() {
					if cur.ID == t.ID && cur.Status == StatusPaused {
						t = cur
						break
					}
				}
				if t.Status == StatusPaused {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
		out.Tasks = append(out.Tasks, t)
	}
	return out, nil
}

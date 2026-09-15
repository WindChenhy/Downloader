package engine

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// TaskExportItem 导出用的任务快照（不含实时 Speed）。
type TaskExportItem struct {
	Task
}

// ExportTasksJSON 导出当前任务列表为 JSON（用于备份/迁移）。
func (m *Manager) ExportTasksJSON() ([]byte, error) {
	tasks := m.GetTasks()
	// 导出时清零瞬时速度，避免误导
	for i := range tasks {
		tasks[i].Speed = 0
	}
	return json.MarshalIndent(tasks, "", "  ")
}

// ImportTasksJSON 从 JSON 导入任务：只恢复未完成（queued/paused/failed）任务的下载意图，
// 已完成任务仅作为记录导入（不重新下载）。返回新建的任务与错误说明。
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
		// 已完成任务不重新入队，仅跳过；失败/暂停/排队按源状态意图重放
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
		// 源为暂停则导入后保持暂停，避免立刻开跑
		if src.Status == StatusPaused {
			_ = m.PauseTask(t.ID)
			// 等待 running → paused 收尾（PauseTask 对 running 是异步 cancel）
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

// RunAfterCompleteAction 执行「全部下载结束」动作。
// openDir 为空时 open_dir 动作退化为不打开。
func RunAfterCompleteAction(action, openDir string) error {
	switch action {
	case AfterCompleteNone, "":
		return nil
	case AfterCompleteOpenDir:
		if openDir == "" {
			return nil
		}
		return openFolder(openDir)
	case AfterCompleteShutdown:
		return shutdownOS()
	case AfterCompleteSleep:
		return sleepOS()
	case AfterCompleteExitApp:
		// 由 App 层处理（需要退出 Wails 进程），引擎只返回提示
		return errExitApp
	default:
		return fmt.Errorf("未知完成后动作: %s", action)
	}
}

// errExitApp 信号：App 收到后调用 requestQuit。
var errExitApp = fmt.Errorf("exit_app")

// IsExitAppAction 判断错误是否为退出应用信号。
func IsExitAppAction(err error) bool { return err == errExitApp }

func openFolder(dir string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", dir).Start()
	case "darwin":
		return exec.Command("open", dir).Start()
	default:
		return exec.Command("xdg-open", dir).Start()
	}
}

func shutdownOS() error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("shutdown", "/s", "/t", "60").Start()
	case "darwin":
		return exec.Command("osascript", "-e", `tell application "System Events" to shut down`).Start()
	default:
		return exec.Command("systemctl", "poweroff").Start()
	}
}

func sleepOS() error {
	switch runtime.GOOS {
	case "windows":
		// rundll32 睡眠
		return exec.Command("rundll32.exe", "powrprof.dll,SetSuspendState", "0,1,0").Start()
	case "darwin":
		return exec.Command("pmset", "sleepnow").Start()
	default:
		return exec.Command("systemctl", "suspend").Start()
	}
}

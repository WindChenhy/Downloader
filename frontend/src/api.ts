import * as App from '../wailsjs/go/main/App';

import type {AddTaskParams, BatchAddResult, Settings, Task} from './types';

// 绑定由 wails build 自动生成；这里统一收口并转换为本项目的类型。
export const api = {
  getTasks: (): Promise<Task[]> => App.GetTasks() as unknown as Promise<Task[]>,
  addTask: (url: string, saveDir: string, connections: number, customName: string): Promise<Task> =>
    App.AddTask(url, saveDir, connections, customName) as unknown as Promise<Task>,
  addTasks: (items: AddTaskParams[]): Promise<BatchAddResult> =>
    App.AddTasks(items as unknown as Parameters<typeof App.AddTasks>[0]) as unknown as Promise<BatchAddResult>,
  pauseTask: (id: string): Promise<void> => App.PauseTask(id),
  resumeTask: (id: string): Promise<void> => App.ResumeTask(id),
  removeTask: (id: string, deleteFiles: boolean): Promise<void> => App.RemoveTask(id, deleteFiles),
  openFolder: (saveDir: string, fileName: string): Promise<void> => App.OpenFolder(saveDir, fileName),
  setTaskPriority: (id: string, priority: number): Promise<void> => App.SetTaskPriority(id, priority),
  moveTask: (id: string, delta: number): Promise<void> => App.MoveTask(id, delta),
  setTaskSpeedLimit: (id: string, limit: number): Promise<void> => App.SetTaskSpeedLimit(id, limit),
  setTaskStartAt: (id: string, startAt: string): Promise<void> => App.SetTaskStartAt(id, startAt),
  exportTasksJson: (): Promise<string> => App.ExportTasksJSON() as unknown as Promise<string>,
  importTasksJson: (data: string): Promise<BatchAddResult> =>
    App.ImportTasksJSON(data) as unknown as Promise<BatchAddResult>,
  getSettings: (): Promise<Settings> => App.GetSettings() as unknown as Promise<Settings>,
  saveSettings: (s: Settings): Promise<void> =>
    App.SaveSettings(s as unknown as Parameters<typeof App.SaveSettings>[0]),
  applyCloseAction: (action: 'exit' | 'minimize', remember: boolean): Promise<void> =>
    App.ApplyCloseAction(action, remember),
};

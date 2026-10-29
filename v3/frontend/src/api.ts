import {AppService} from '../bindings/downloader/index.js';

import type {AddTaskParams, BatchAddResult, QueueStatus, Settings, Stats, Task, TaskStat} from './types';

// 绑定由 `wails3 generate bindings` 自动生成（frontend/bindings，JSDoc 类型）；
// 这里统一收口并转换为本项目的类型。方法名与 v2 保持一致。
export const api = {
  getTasks: (): Promise<Task[]> => AppService.GetTasks() as unknown as Promise<Task[]>,
  addTask: (url: string, saveDir: string, connections: number, customName: string): Promise<Task> =>
    AppService.AddTask(url, saveDir, connections, customName) as unknown as Promise<Task>,
  addTasks: (items: AddTaskParams[]): Promise<BatchAddResult> =>
    AppService.AddTasks(items as unknown as Parameters<typeof AppService.AddTasks>[0]) as unknown as Promise<BatchAddResult>,
  pauseTask: (id: string): Promise<void> => AppService.PauseTask(id),
  resumeTask: (id: string): Promise<void> => AppService.ResumeTask(id),
  pauseAll: (): Promise<number> => AppService.PauseAll(),
  resumeAll: (): Promise<number> => AppService.ResumeAll(),
  retryAllFailed: (): Promise<number> => AppService.RetryAllFailed(),
  clearCompleted: (): Promise<number> => AppService.ClearCompleted(),
  setQueueSuspended: (v: boolean): Promise<void> => AppService.SetQueueSuspended(v),
  queueStatus: (): Promise<QueueStatus> => AppService.QueueStatus() as unknown as Promise<QueueStatus>,
  setQueueWindow: (enabled: boolean, start: string, end: string): Promise<void> =>
    AppService.SetQueueWindow(enabled, start, end),
  getStats: (): Promise<Stats> => AppService.GetStats() as unknown as Promise<Stats>,
  // 单任务曲线按需拉取：GetStats 的任务汇总不再带 samples，
  // 避免每次轮询全量序列化；任务无统计记录时 reject（404）
  getTaskStats: (id: string): Promise<TaskStat> =>
    AppService.GetTaskStats(id) as unknown as Promise<TaskStat>,
  removeTask: (id: string, deleteFiles: boolean): Promise<void> => AppService.RemoveTask(id, deleteFiles),
  openFolder: (saveDir: string, fileName: string): Promise<void> => AppService.OpenFolder(saveDir, fileName),
  setTaskPriority: (id: string, priority: number): Promise<void> => AppService.SetTaskPriority(id, priority),
  moveTask: (id: string, delta: number): Promise<void> => AppService.MoveTask(id, delta),
  setTaskSpeedLimit: (id: string, limit: number): Promise<void> => AppService.SetTaskSpeedLimit(id, limit),
  setTaskStartAt: (id: string, startAt: string): Promise<void> => AppService.SetTaskStartAt(id, startAt),
  exportTasksJson: (withAuth: boolean): Promise<string> =>
    AppService.ExportTasksJSON(withAuth) as unknown as Promise<string>,
  importTasksJson: (data: string): Promise<BatchAddResult> =>
    AppService.ImportTasksJSON(data) as unknown as Promise<BatchAddResult>,
  getSettings: (): Promise<Settings> => AppService.GetSettings() as unknown as Promise<Settings>,
  saveSettings: (s: Settings): Promise<void> =>
    AppService.SaveSettings(s as unknown as Parameters<typeof AppService.SaveSettings>[0]),
  applyCloseAction: (action: 'exit' | 'minimize', remember: boolean): Promise<void> =>
    AppService.ApplyCloseAction(action, remember),
};

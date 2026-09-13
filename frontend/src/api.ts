import {
  AddTask,
  GetSettings,
  GetTasks,
  OpenFolder,
  PauseTask,
  RemoveTask,
  ResumeTask,
  SaveSettings,
} from '../wailsjs/go/main/App';

import type {Settings, Task} from './types';

// 绑定由 wails build 自动生成；这里统一收口并转换为本项目的类型。
export const api = {
  getTasks: (): Promise<Task[]> => GetTasks() as unknown as Promise<Task[]>,
  addTask: (url: string, saveDir: string, connections: number): Promise<Task> =>
    AddTask(url, saveDir, connections) as unknown as Promise<Task>,
  pauseTask: (id: string): Promise<void> => PauseTask(id),
  resumeTask: (id: string): Promise<void> => ResumeTask(id),
  removeTask: (id: string, deleteFiles: boolean): Promise<void> => RemoveTask(id, deleteFiles),
  openFolder: (saveDir: string, fileName: string): Promise<void> => OpenFolder(saveDir, fileName),
  getSettings: (): Promise<Settings> => GetSettings() as unknown as Promise<Settings>,
  saveSettings: (s: Settings): Promise<void> => SaveSettings(s),
};

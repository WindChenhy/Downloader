export type Status = 'queued' | 'running' | 'paused' | 'completed' | 'failed';

export interface Task {
  id: string;
  url: string;
  fileName: string;
  saveDir: string;
  totalSize: number; // <=0 表示未知
  downloaded: number;
  speed: number; // 字节/秒
  status: Status;
  connections: number;
  error?: string;
  createdAt: string;
}

export interface Settings {
  saveDir: string;
  connections: number;
  concurrentTasks: number;
}

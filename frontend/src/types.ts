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

export type ProxyMode = 'none' | 'system' | 'custom';
export type CloseAction = 'ask' | 'exit' | 'minimize';

export interface Settings {
  saveDir: string;
  connections: number;
  concurrentTasks: number;
  speedLimit: number; // 字节/秒，0 不限
  userAgent: string;
  extraHeaders: string;
  proxyMode: ProxyMode;
  proxyUrl: string;
  githubMirror: boolean;
  mirrorTemplate: string;
  clipboardWatch: boolean;
  apiEnabled: boolean;
  apiPort: number;
  closeAction: CloseAction;
}

export type ThemeMode = 'light' | 'dark' | 'system';

export const ACCENTS: {key: string; label: string; color: string}[] = [
  {key: 'blue', label: '蓝', color: '#4f8cff'},
  {key: 'violet', label: '紫', color: '#8b5cf6'},
  {key: 'green', label: '绿', color: '#3ecf8e'},
  {key: 'teal', label: '青', color: '#14b8a6'},
  {key: 'orange', label: '橙', color: '#f59e0b'},
  {key: 'red', label: '红', color: '#ef4444'},
  {key: 'pink', label: '粉', color: '#ec4899'},
  {key: 'slate', label: '灰', color: '#64748b'},
];

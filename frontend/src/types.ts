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
  /** 纯下载耗时（毫秒），暂停不计 */
  activeMs: number;
  /** 结束时刻；零值/空表示尚未完成或失败 */
  finishedAt?: string;
  /** 平均下载速度（字节/秒），按活跃时长 */
  avgSpeed: number;
  /** 优先级 0低/1普通/2高 */
  priority: number;
  /** 定时开始；零值/空表示立即 */
  startAt?: string;
  /** 每任务限速（字节/秒），0 跟随全局 */
  speedLimit: number;
  /** 校验和（可选） */
  checksumAlgo?: string;
  checksumExpected?: string;
  checksumActual?: string;
  checksumStatus?: 'ok' | 'mismatch' | 'error' | 'skipped' | '';
}

export interface AddTaskParams {
  url: string;
  saveDir: string;
  connections: number;
  customName?: string;
  checksumAlgo?: string;
  checksumExpected?: string;
  priority?: number;
  startAt?: string;
  speedLimit?: number;
}

export interface BatchAddResult {
  tasks: Task[];
  errors: string[];
}

export type ProxyMode = 'none' | 'system' | 'custom';
export type CloseAction = 'ask' | 'exit' | 'minimize';
export type AfterCompleteAction = 'none' | 'open_dir' | 'shutdown' | 'sleep' | 'exit_app';

export interface DirCategory {
  name: string;
  path: string;
}

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
  dirCategories: DirCategory[];
  autoExtract: boolean;
  /** 系统通知：完成/失败默认开，创建/暂停默认关 */
  notifyOnCreate: boolean;
  notifyOnPause: boolean;
  notifyOnComplete: boolean;
  notifyOnFail: boolean;
  /** 全部下载结束后的动作 */
  afterComplete: AfterCompleteAction;
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

export const PRIORITY_META: {key: number; label: string}[] = [
  {key: 0, label: '低'},
  {key: 1, label: '普通'},
  {key: 2, label: '高'},
];

export function isScheduled(task: Task, now = Date.now()): boolean {
  if (!task.startAt) return false;
  const t = Date.parse(task.startAt);
  if (Number.isNaN(t) || t <= now) return false;
  // Go 零值
  if (task.startAt.startsWith('0001-01-01')) return false;
  return task.status === 'queued' || task.status === 'paused';
}

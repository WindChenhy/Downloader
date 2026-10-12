export type Status = 'queued' | 'running' | 'paused' | 'completed' | 'failed';

export interface Task {
  id: string;
  url: string;
  fileName: string;
  /** 自定义文件名；空 = 自动从链接/响应头获取 */
  customName?: string;
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
  /** 每任务认证与防盗链头（可选）；空 = 沿用全局自定义请求头 */
  cookie?: string;
  referer?: string;
  /** 停在队列里却没被启动的原因（磁盘空间不足）；空 = 无阻塞 */
  blockReason?: string;
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
  cookie?: string;
  referer?: string;
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
  /** REST API 访问令牌；由后端生成，保存时空值表示保留现有 */
  apiToken?: string;
  /** API/导入允许的保存目录白名单；空 = 仅默认下载目录 */
  allowSaveDirs?: string[];
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
  /** 界面语言（解析后的具体值，供系统通知/托盘使用）；前端偏好见 i18n 的 LangPref */
  language?: 'zh-CN' | 'en-US';
  /** 队列总闸：true = 不启动任何任务，已排队的停在排队中；跨重启保持 */
  queueSuspended?: boolean;
  /** 带宽时间窗：启用后只在 [windowStart, windowEnd) 内启动新任务 */
  windowEnabled?: boolean;
  windowStart?: string;
  windowEnd?: string;
  /** 磁盘空间预检查开关（字段取反：老设置没这个键 = 开启） */
  diskGuardOff?: boolean;
  /** 预检查在文件大小之外还要保留的余量（字节），0 = 不留 */
  diskMinFree?: number;
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

/** SpeedPoint 速度采样点：t = 毫秒时间戳，v = 字节/秒（窗口平均速率）。 */
export interface SpeedPoint {
  t: number;
  v: number;
}

/** DailyStat 单日聚合；completed/failed 计的是「结束轮次」。 */
export interface DailyStat {
  date: string;
  bytes: number;
  completed: number;
  failed: number;
  peakSpeed: number;
  sumSpeed: number;
  samples: number;
}

/** TaskStat 单任务统计汇总；samples 只为最近 15 个任务落盘（更早的只有汇总）。 */
export interface TaskStat {
  taskId: string;
  fileName: string;
  url: string;
  status: Status;
  bytes: number;
  totalSize: number;
  avgSpeed: number;
  peakSpeed: number;
  activeMs: number;
  updatedAt: number;
  samples?: SpeedPoint[];
}

/** Stats 统计页一次读取的全部数据（引擎 engine.Stats）。 */
export interface Stats {
  generatedAt: number;
  todayBytes: number;
  weekBytes: number;
  totalBytes: number;
  currentSpeed: number;
  peakSpeed: number;
  avgSpeed: number;
  windowMs: number;
  globalFrom: number;
  globalTo: number;
  todayCompleted: number;
  todayFailed: number;
  taskCount: number;
  running: number;
  queued: number;
  completed: number;
  failed: number;
  paused: number;
  daily: DailyStat[];
  global: SpeedPoint[];
  tasks: TaskStat[];
}

/** QueueStatus 队列为何不动：用户总闸 / 带宽时间窗外 / 磁盘空间不足。 */
export interface QueueStatus {
  active: boolean;
  suspended: boolean;
  windowEnabled: boolean;
  windowActive: boolean;
  windowStart: string;
  windowEnd: string;
  /** 窗外时下一次放行的时刻（RFC3339）；窗内为空串 */
  opensAt: string;
  /** 因磁盘空间不足停在队列里的任务数 */
  blockedTasks: number;
}

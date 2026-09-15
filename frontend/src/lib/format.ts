import type {Status, Task} from '../types';

export function formatBytes(n: number): string {
  if (!n || n < 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`;
}

export function formatSpeed(bytesPerSec: number): string {
  if (!bytesPerSec || bytesPerSec <= 0) return '';
  return `${formatBytes(bytesPerSec)}/s`;
}

export function formatETA(seconds: number): string {
  if (!seconds || seconds <= 0 || !isFinite(seconds)) return '';
  const s = Math.round(seconds);
  if (s < 60) return `${s} 秒`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} 分 ${s % 60} 秒`;
  const h = Math.floor(m / 60);
  return `${h} 小时 ${m % 60} 分`;
}

/** formatDuration 毫秒 → 人读时长，如 `1分23秒` / `2小时5分`。 */
export function formatDuration(ms: number): string {
  if (!ms || ms < 0 || !isFinite(ms)) return '—';
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s} 秒`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} 分 ${s % 60} 秒`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} 小时 ${m % 60} 分`;
  const d = Math.floor(h / 24);
  return `${d} 天 ${h % 24} 小时`;
}

/** isEndedStatus 任务是否已停止计时（完成/失败）。 */
function isEndedStatus(status: Status): boolean {
  return status === 'completed' || status === 'failed';
}

/**
 * totalElapsedMs 创建到结束的总耗时（含暂停）。
 * - 已完成/失败：只用冻结的 finishedAt，不再随当前时间增长；无有效结束时间时返回 0。
 * - 进行中/暂停：用 now - createdAt。
 */
export function totalElapsedMs(task: Task, now = Date.now()): number {
  const created = Date.parse(task.createdAt);
  if (Number.isNaN(created)) return 0;

  if (isEndedStatus(task.status)) {
    if (!task.finishedAt || task.finishedAt.startsWith('0001-01-01')) return 0;
    const end = Date.parse(task.finishedAt);
    if (Number.isNaN(end) || end <= created) return 0;
    return end - created;
  }
  return Math.max(0, now - created);
}

/** activeElapsedMs 活跃下载耗时；未结束任务的 running 部分由后端并入 activeMs。 */
export function activeElapsedMs(task: Task): number {
  return Math.max(0, task.activeMs || 0);
}

/** parseDownloadUrls 从粘贴文本提取 http(s) 链接（支持多行/空白分隔/引号）。 */
export function parseDownloadUrls(text: string): string[] {
  return text
    .split(/[\r\n\s]+/)
    .map((s) => s.trim().replace(/^["']|["']$/g, ''))
    .filter((s) => /^https?:\/\//i.test(s));
}

/**
 * expandSequenceToken 展开 `{1..10}` / `{001..010:2}` 序列占位。
 * 宽度取较大端点的十进制位数（可补零）。
 */
export function expandSequenceToken(token: string, pad: number, step: number): string[] {
  const [a, b] = token.split('..');
  const start = Number(a);
  const end = Number(b);
  if (!Number.isFinite(start) || !Number.isFinite(end)) return [];
  const [lo, hi] = start <= end ? [start, end] : [end, start];
  const s = Math.max(1, step | 0);
  const out: string[] = [];
  const width = Math.max(String(lo).length, String(hi).length, pad);
  const dir = start <= end ? 1 : -1;
  if (dir > 0) {
    for (let i = lo; i <= hi; i += s) out.push(String(i).padStart(width, '0'));
  } else {
    for (let i = lo; i >= hi; i -= s) out.push(String(i).padStart(width, '0'));
  }
  return out;
}

/**
 * expandSequenceUrls 展开含 `{a..b}` / `{a..b:step}` 的链接。
 * 例：`https://x/f_{1..3}.zip` → 三条链接。无序列时原样返回。
 */
export function expandSequenceUrls(text: string): string[] {
  const lines = text.split(/[\r\n]+/).map((l) => l.trim()).filter(Boolean);
  const out: string[] = [];
  const re = /\{(\d+)\.\.(\d+)(?::(\d+))?\}/;
  for (const line of lines) {
    const m = line.match(re);
    if (!m) {
      out.push(line);
      continue;
    }
    const [, a, b, stepRaw] = m;
    const step = stepRaw ? Number(stepRaw) : 1;
    const nums = expandSequenceToken(`${a}..${b}`, 0, step);
    for (const n of nums) {
      out.push(line.replace(m[0], n));
    }
  }
  return out;
}

/** 串联：先展开序列号，再过滤为合法下载链接。 */
export function parseAndExpandUrls(text: string): string[] {
  return parseDownloadUrls(expandSequenceUrls(text).join('\n'));
}

export const checksumStatusMeta: Record<string, {label: string; cls: string}> = {
  ok: {label: '校验通过', cls: 'completed'},
  mismatch: {label: '校验失败', cls: 'failed'},
  error: {label: '校验错误', cls: 'failed'},
  skipped: {label: '已计算', cls: 'paused'},
};

export function percent(task: {downloaded: number; totalSize: number; status: Status}): number {
  if (task.status === 'completed') return 100;
  if (task.totalSize <= 0) return 0;
  return Math.min(100, Math.round((task.downloaded / task.totalSize) * 100));
}

export const statusMeta: Record<Status, {label: string; cls: string}> = {
  queued: {label: '排队中', cls: 'queued'},
  running: {label: '下载中', cls: 'running'},
  paused: {label: '已暂停', cls: 'paused'},
  completed: {label: '已完成', cls: 'completed'},
  failed: {label: '失败', cls: 'failed'},
};

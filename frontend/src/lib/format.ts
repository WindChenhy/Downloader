import type {Status} from '../types';

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

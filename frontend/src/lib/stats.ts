import type {DailyStat, SpeedPoint} from '../types';

/**
 * 统计页的纯函数：坐标换算、刻度选择、抽稀与补洞。
 * 与 React 无关，方便单测（项目里没有组件测试基础，逻辑都压到这层来）。
 */

/** 图表 viewBox 尺寸与内边距：纵轴标签要留位置。 */
export const CHART_BOX = {w: 640, h: 180, padL: 46, padR: 10, padT: 12, padB: 22};

export interface ChartBox {
  w: number;
  h: number;
  padL: number;
  padR: number;
  padT: number;
  padB: number;
}

export interface ChartSeries {
  /** SVG 折线路径；点数不足 2 个时为空串 */
  line: string;
  /** 折线下的填充路径（闭合到基线） */
  area: string;
  /** 最后一个点，用于画当前值标记 */
  last?: {x: number; y: number};
}

const round1 = (v: number): number => Math.round(v * 10) / 10;

/** 绘图区基线（v=0 所在的 y），网格线与面积填充都用它。 */
export const CHART_BASELINE = CHART_BOX.padT + (CHART_BOX.h - CHART_BOX.padT - CHART_BOX.padB);

/** yForValue 把速度值换算成 SVG 的 y；超出 vmax 的点夹在顶部。 */
export function yForValue(v: number, vmax: number, box: ChartBox = CHART_BOX): number {
  const innerH = box.h - box.padT - box.padB;
  const top = Math.max(vmax, 1);
  return box.padT + innerH - (Math.max(0, Math.min(v, top)) / top) * innerH;
}

/**
 * niceMax 纵轴上界：取不小于 v 的「1/2/5 × 10^k」，刻度标签才不会是 1372.4 KB/s 这种数。
 * 0 或负数返回 0（调用方按「无数据」处理）。
 */
export function niceMax(v: number): number {
  if (!Number.isFinite(v) || v <= 0) return 0;
  const exp = Math.floor(Math.log10(v));
  const base = Math.pow(10, exp);
  for (const m of [1, 2, 5, 10]) {
    const cand = m * base;
    if (cand >= v) return cand;
  }
  return 10 * base;
}

/** tickValues 从 0 到 vmax 的等分刻度（含两端）。 */
export function tickValues(vmax: number, count = 4): number[] {
  if (vmax <= 0 || count <= 0) return [0];
  const out: number[] = [];
  for (let i = 0; i <= count; i++) out.push(Math.round((vmax * i) / count));
  return out;
}

/**
 * buildSeries 采样点 → SVG 路径。
 * 横轴按时间线性映射：采样在有速度时才记点，空闲段是空洞，
 * 按索引等分绘制会把空洞压缩成「一直在全速」的假象。
 */
export function buildSeries(points: SpeedPoint[], t0: number, t1: number, vmax: number, box: ChartBox = CHART_BOX): ChartSeries {
  const innerW = box.w - box.padL - box.padR;
  const span = Math.max(1, t1 - t0);
  const px = (t: number) => box.padL + ((t - t0) / span) * innerW;

  const pts = (points ?? []).filter((p) => p && Number.isFinite(p.t) && Number.isFinite(p.v) && p.t >= t0 && p.t <= t1);
  if (pts.length === 0) return {line: '', area: ''};
  const coords = pts.map((p) => ({x: round1(px(p.t)), y: round1(yForValue(p.v, vmax, box))}));
  if (coords.length === 1) return {line: '', area: '', last: coords[0]};

  const line = coords.map((c, i) => `${i === 0 ? 'M' : 'L'}${c.x} ${c.y}`).join(' ');
  const baseline = round1(CHART_BASELINE);
  const area = `${line} L${coords[coords.length - 1].x} ${baseline} L${coords[0].x} ${baseline} Z`;
  return {line, area, last: coords[coords.length - 1]};
}

/**
 * decimate 等间隔抽稀到 max 个以内，始终保留首尾。
 * 上千点在 SVG 里既拖慢重绘又看不出差别。
 */
export function decimate<T>(items: T[], max: number): T[] {
  if (!Number.isFinite(max) || max <= 0 || items.length <= max) return items.slice();
  if (max === 1) return [items[items.length - 1]];
  const out: number[] = [];
  const step = (items.length - 1) / (max - 1);
  for (let i = 0; i < max; i++) {
    const idx = Math.round(i * step);
    if (out.length === 0 || idx !== out[out.length - 1]) out.push(idx);
  }
  return out.map((idx) => items[idx]);
}

/** chartWindow 全局曲线的时间范围：右端跟随当前时刻，左端至少覆盖满窗口。 */
export function chartWindow(from: number, to: number, windowMs: number, now: number): {t0: number; t1: number} {
  const t1 = Math.max(to || 0, now);
  const t0 = from && from < t1 ? from : t1 - Math.max(windowMs, 60_000);
  return {t0, t1};
}

export interface DayBar {
  date: string;
  /** MM-DD 短标签 */
  label: string;
  bytes: number;
  today: boolean;
}

/**
 * fillDays 补齐最近 days 天（没有记录的日子填 0）。
 * 引擎只在有下载的那一天写聚合，直接画图会跳日期。
 */
export function fillDays(daily: DailyStat[], days: number, now = new Date()): DayBar[] {
  const byDate = new Map((daily ?? []).map((d) => [d.date, d.bytes]));
  const key = (d: Date) =>
    `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  const out: DayBar[] = [];
  for (let i = days - 1; i >= 0; i--) {
    const d = new Date(now);
    d.setDate(d.getDate() - i);
    const date = key(d);
    out.push({date, label: date.slice(5), bytes: byDate.get(date) ?? 0, today: i === 0});
  }
  return out;
}

/** windowLabel 曲线窗口的人读时长，如 `30 分钟` / `5 分钟`。 */
export function windowLabel(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '';
  const min = Math.round(ms / 60000);
  if (min < 60) return `${min} 分钟`;
  const h = Math.floor(min / 60);
  return min % 60 === 0 ? `${h} 小时` : `${h} 小时 ${min % 60} 分`;
}

/** clockLabel 毫秒时间戳 → HH:MM（曲线横轴端点）。 */
export function clockLabel(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '';
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return '';
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

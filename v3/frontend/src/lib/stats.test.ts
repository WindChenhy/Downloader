import {describe, expect, it} from 'vitest';

import type {DailyStat, SpeedPoint} from '../types';
import {
  buildSeries,
  chartWindow,
  clockLabel,
  decimate,
  fillDays,
  niceMax,
  tickValues,
  windowLabel,
} from './stats';

const pts = (pairs: [number, number][]): SpeedPoint[] => pairs.map(([t, v]) => ({t, v}));

describe('niceMax', () => {
  it('取 1/2/5×10^k 的上界', () => {
    expect(niceMax(1372)).toBe(2000);
    expect(niceMax(900)).toBe(1000);
    expect(niceMax(5001)).toBe(10000);
    expect(niceMax(10)).toBe(10);
  });
  it('非正数与非法值返回 0', () => {
    expect(niceMax(0)).toBe(0);
    expect(niceMax(-5)).toBe(0);
    expect(niceMax(Number.NaN)).toBe(0);
  });
});

describe('tickValues', () => {
  it('等分含两端', () => {
    expect(tickValues(2000)).toEqual([0, 500, 1000, 1500, 2000]);
    expect(tickValues(0)).toEqual([0]);
  });
});

describe('buildSeries', () => {
  // 绘图区：x 从 padL=46 到 w-padR=630，y 从顶部 12 到基线 158（h-padT-padB）
  const BASELINE = 158;
  const coordsOf = (line: string): [number, number][] => {
    const nums = line.match(/-?\d+(?:\.\d+)?/g)?.map(Number) ?? [];
    const out: [number, number][] = [];
    for (let i = 0; i + 1 < nums.length; i += 2) out.push([nums[i], nums[i + 1]]);
    return out;
  };

  it('按时间线性映射横轴，首尾落在绘图区两端', () => {
    const s = buildSeries(pts([[0, 0], [1000, 100]]), 0, 1000, 100);
    expect(coordsOf(s.line)).toEqual([
      [46, BASELINE],
      [630, 12],
    ]);
  });

  it('超出上界的值被夹住，空洞按真实时间留白', () => {
    const s = buildSeries(pts([[0, 500], [500, 50], [1000, 100]]), 0, 1000, 100);
    const coords = coordsOf(s.line);
    expect(coords[0][1]).toBe(12); // 500 > vmax → 夹到顶
    expect(coords[1][0]).toBeCloseTo(338, 0); // 中间时刻在中间位置
    expect(coords[1][1]).toBeCloseTo((12 + BASELINE) / 2, 0); // v=50 在中点
  });

  it('范围外的点被丢掉', () => {
    const s = buildSeries(pts([[0, 10], [2000, 20]]), 100, 500, 100);
    expect(s.line).toBe('');
    expect(s.last).toBeUndefined();
  });

  it('单点只给标记不画线', () => {
    const s = buildSeries(pts([[500, 50]]), 0, 1000, 100);
    expect(s.line).toBe('');
    expect(s.last).toBeDefined();
  });

  it('面积路径闭合到基线', () => {
    const s = buildSeries(pts([[0, 50], [1000, 50]]), 0, 1000, 100);
    expect(s.area.endsWith('Z')).toBe(true);
    expect(s.area).toContain(`L${s.last?.x} ${BASELINE} L46 ${BASELINE} Z`);
  });
});

describe('decimate', () => {
  it('不超上限时原样返回（副本）', () => {
    const src = [1, 2, 3];
    const out = decimate(src, 10);
    expect(out).toEqual(src);
    expect(out).not.toBe(src);
  });
  it('保留首尾且长度不超上限', () => {
    const src = Array.from({length: 1000}, (_, i) => i);
    const out = decimate(src, 60);
    expect(out.length).toBeLessThanOrEqual(60);
    expect(out[0]).toBe(0);
    expect(out[out.length - 1]).toBe(999);
  });
  it('上限为 1 时只留最后一个', () => {
    expect(decimate([1, 2, 3], 1)).toEqual([3]);
  });
});

describe('chartWindow', () => {
  it('右端跟随当前时刻，左端用采样起点', () => {
    expect(chartWindow(1000, 2000, 30000, 5000)).toEqual({t0: 1000, t1: 5000});
  });
  it('没有采样时按窗口长度回推', () => {
    const {t0, t1} = chartWindow(0, 0, 120000, 500000);
    expect(t1).toBe(500000);
    expect(t0).toBe(380000);
  });
  it('窗口为 0 时至少留一分钟', () => {
    expect(chartWindow(0, 0, 0, 50000).t0).toBe(50000 - 60000);
  });
});

describe('fillDays', () => {
  const daily = (date: string, bytes: number): DailyStat => ({
    date,
    bytes,
    completed: 0,
    failed: 0,
    peakSpeed: 0,
    sumSpeed: 0,
    samples: 0,
  });

  it('缺失日期补 0，最后一天标记为今天', () => {
    const now = new Date('2026-10-11T12:00:00');
    const out = fillDays([daily('2026-10-11', 100), daily('2026-10-09', 7)], 4, now);
    expect(out.map((d) => d.date)).toEqual(['2026-10-08', '2026-10-09', '2026-10-10', '2026-10-11']);
    expect(out.map((d) => d.bytes)).toEqual([0, 7, 0, 100]);
    expect(out[3].today).toBe(true);
    expect(out[0].today).toBe(false);
    expect(out[0].label).toBe('10-08');
  });

  it('跨月边界按本地日期回推', () => {
    const now = new Date('2026-03-01T08:00:00');
    const out = fillDays([], 3, now);
    expect(out.map((d) => d.date)).toEqual(['2026-02-27', '2026-02-28', '2026-03-01']);
  });

  it('空输入不炸', () => {
    expect(fillDays(undefined as unknown as DailyStat[], 2)).toHaveLength(2);
  });
});

describe('windowLabel', () => {
  it('分钟与小时口径', () => {
    expect(windowLabel(2 * 60000)).toBe('2 分钟');
    expect(windowLabel(30 * 60000)).toBe('30 分钟');
    expect(windowLabel(2 * 3600000)).toBe('2 小时');
    expect(windowLabel(60 * 60000 + 30 * 60000)).toBe('1 小时 30 分');
    expect(windowLabel(0)).toBe('');
  });
});

describe('clockLabel', () => {
  it('输出本地 HH:MM，非法值空串', () => {
    const d = new Date(2026, 9, 11, 9, 5, 0);
    expect(clockLabel(d.getTime())).toBe('09:05');
    expect(clockLabel(0)).toBe('');
  });
});

import {describe, expect, it} from 'vitest';

import {
  expandSequenceUrls,
  formatDuration,
  parseAndExpandUrls,
  parseDownloadUrls,
  totalElapsedMs,
} from './format';
import type {Task} from '../types';

function baseTask(patch: Partial<Task> = {}): Task {
  return {
    id: '1',
    url: 'https://example.com/a.bin',
    fileName: 'a.bin',
    saveDir: 'C:/dl',
    totalSize: 100,
    downloaded: 100,
    speed: 0,
    status: 'completed',
    connections: 8,
    createdAt: '2026-01-01T00:00:00.000Z',
    activeMs: 0,
    avgSpeed: 0,
    priority: 1,
    speedLimit: 0,
    ...patch,
  };
}

describe('parseDownloadUrls', () => {
  it('按行拆分并过滤非法项', () => {
    const text = [
      'https://example.com/a.zip',
      'not a url',
      '  http://example.com/b.zip  ',
      '"https://example.com/c.zip"',
      'ftp://example.com/x',
      '',
    ].join('\n');
    expect(parseDownloadUrls(text)).toEqual([
      'https://example.com/a.zip',
      'http://example.com/b.zip',
      'https://example.com/c.zip',
    ]);
  });

  it('空白分隔也算多条', () => {
    expect(parseDownloadUrls('https://a.com/1 https://a.com/2')).toHaveLength(2);
  });
});

describe('expandSequenceUrls', () => {
  it('展开 {1..3}', () => {
    expect(expandSequenceUrls('https://x.com/f_{1..3}.zip')).toEqual([
      'https://x.com/f_1.zip',
      'https://x.com/f_2.zip',
      'https://x.com/f_3.zip',
    ]);
  });

  it('支持步长', () => {
    expect(expandSequenceUrls('https://x.com/f_{1..5:2}.zip')).toEqual([
      'https://x.com/f_1.zip',
      'https://x.com/f_3.zip',
      'https://x.com/f_5.zip',
    ]);
  });

  it('无序列原样返回', () => {
    expect(expandSequenceUrls('https://x.com/a.zip')).toEqual(['https://x.com/a.zip']);
  });
});

describe('parseAndExpandUrls', () => {
  it('序列展开后过滤合法链接', () => {
    const urls = parseAndExpandUrls('https://x.com/{1..2}.zip\nnope');
    expect(urls).toEqual(['https://x.com/1.zip', 'https://x.com/2.zip']);
  });
});

describe('formatDuration', () => {
  it('毫秒转人读时长', () => {
    expect(formatDuration(0)).toBe('—');
    expect(formatDuration(1500)).toBe('1 秒');
    expect(formatDuration(65_000)).toBe('1 分 5 秒');
    expect(formatDuration(3_725_000)).toBe('1 小时 2 分');
  });
});

describe('totalElapsedMs', () => {
  it('进行中用当前时间', () => {
    const created = Date.parse('2026-01-01T00:00:00.000Z');
    const now = created + 10_000;
    const t = baseTask({status: 'running', finishedAt: undefined});
    expect(totalElapsedMs(t, now)).toBe(10_000);
  });

  it('已完成后冻结在 finishedAt', () => {
    const t = baseTask({
      status: 'completed',
      createdAt: '2026-01-01T00:00:00.000Z',
      finishedAt: '2026-01-01T00:05:00.000Z',
    });
    expect(totalElapsedMs(t, Date.parse('2026-01-01T01:00:00.000Z'))).toBe(5 * 60 * 1000);
  });

  it('Go 零值 finishedAt 不参与计算', () => {
    const t = baseTask({
      status: 'completed',
      finishedAt: '0001-01-01T00:00:00Z',
    });
    expect(totalElapsedMs(t)).toBe(0);
  });
});

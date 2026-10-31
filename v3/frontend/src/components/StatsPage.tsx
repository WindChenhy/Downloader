import {useEffect, useMemo, useState} from 'react';

import type {SpeedPoint, Stats} from '../types';
import {t, useI18n} from '../i18n';
import {api} from '../api';
import {formatBytes, formatDuration, formatSpeed} from '../lib/format';
import {registerBackHandler} from '../lib/backHandler';
import {chartWindow, fillDays, niceMax, windowLabel} from '../lib/stats';
import SpeedChart from './SpeedChart';

interface Props {
  onBack: () => void;
}

const DAY_BARS = 14;

function Card({label, value, hint}: {label: string; value: string; hint?: string}) {
  return (
    <div className="stats-card">
      <div className="stats-card-label">{label}</div>
      <div className="stats-card-value">{value}</div>
      {hint && <div className="stats-card-hint">{hint}</div>}
    </div>
  );
}

export default function StatsPage({onBack}: Props) {
  const {t: tr} = useI18n();
  const [stats, setStats] = useState<Stats | null>(null);
  const [error, setError] = useState('');
  const [picked, setPicked] = useState('');
  // 任务选择弹层：原生 select 的系统弹窗无法跟随主题（亮底系统 radio），
  // 换成与主题一致的自定义面板
  const [pickerOpen, setPickerOpen] = useState(false);
  // Android 返回键：选择弹层打开时先关弹层
  useEffect(() => {
    if (!pickerOpen) return;
    return registerBackHandler(() => {
      setPickerOpen(false);
      return true;
    });
  }, [pickerOpen]);
  // 所选任务的曲线：GetStats 汇总不再带 samples，单独按需拉取
  const [taskSamples, setTaskSamples] = useState<SpeedPoint[]>([]);
  // 引擎 500ms 推一次进度，但统计接口是拉取的：曲线本身 2s 一个采样点，
  // 汇总 2s 一轮足够跟住
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    let alive = true;
    const pull = () => {
      api
        .getStats()
        .then((s) => {
          if (alive) {
            setStats(s);
            setNow(Date.now());
            setError('');
          }
        })
        .catch((e) => {
          if (alive) setError(String(e));
        });
    };
    pull();
    const timer = window.setInterval(pull, 2000);
    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, []);

  const days = useMemo(() => fillDays(stats?.daily ?? [], DAY_BARS, new Date(now)), [stats, now]);
  const maxDay = Math.max(...days.map((d) => d.bytes), 1);

  // 全局曲线与所选任务曲线共用同一纵轴刻度：两张图的高度才可以直接比
  const vmax = niceMax(Math.max(stats?.peakSpeed ?? 0, stats?.currentSpeed ?? 0, 1));
  const win = chartWindow(stats?.globalFrom ?? 0, stats?.globalTo ?? 0, stats?.windowMs ?? 0, now);

  const taskStat = useMemo(() => {
    const list = stats?.tasks ?? [];
    if (!list.length) return null;
    return list.find((x) => x.taskId === picked) ?? list[0];
  }, [stats, picked]);

  const taskId = taskStat?.taskId ?? '';

  // 单任务曲线按需拉取（与汇总同节奏 2s；切任务时立即重拉）。
  // 任务没有统计记录（404，如被裁剪的老任务）时静默置空
  useEffect(() => {
    if (!taskId) {
      setTaskSamples([]);
      return;
    }
    let alive = true;
    const pull = () => {
      api
        .getTaskStats(taskId)
        .then((ts) => {
          if (alive) setTaskSamples(ts.samples ?? []);
        })
        .catch(() => {
          if (alive) setTaskSamples([]);
        });
    };
    pull();
    const timer = window.setInterval(pull, 2000);
    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, [taskId]);

  const taskWin = useMemo(() => {
    if (!taskSamples.length) return {t0: win.t0, t1: win.t1};
    return {t0: taskSamples[0].t, t1: Math.max(taskSamples[taskSamples.length - 1].t, now)};
  }, [taskSamples, win.t0, win.t1, now]);

  const windowText = windowLabel(stats?.windowMs ?? 0);

  return (
    <div className="settings">
      <h2>{tr('下载统计')}</h2>
      {error && <div className="settings-note">{tr('读取统计失败：{e}', {e: error})}</div>}

      <div className="stats-cards">
        <Card label={tr('今日下载量')} value={formatBytes(stats?.todayBytes ?? 0)} hint={tr('按天累计，跨重启保留')} />
        <Card label={tr('近 7 天')} value={formatBytes(stats?.weekBytes ?? 0)} />
        <Card label={tr('累计下载量')} value={formatBytes(stats?.totalBytes ?? 0)} hint={tr('最多保留 {n} 天', {n: 90})} />
        <Card
          label={tr('当前速度')}
          value={stats?.currentSpeed ? formatSpeed(stats.currentSpeed) : tr('空闲')}
          hint={tr('最近一次采样（{n}）', {n: windowText})}
        />
        <Card label={tr('窗口内峰值')} value={stats?.peakSpeed ? formatSpeed(stats.peakSpeed) : '—'} hint={windowText} />
        <Card label={tr('窗口内均速')} value={stats?.avgSpeed ? formatSpeed(stats.avgSpeed) : '—'} hint={windowText} />
      </div>

      <h3>{tr('速度曲线')}</h3>
      <SpeedChart
        points={stats?.global ?? []}
        t0={win.t0}
        t1={win.t1}
        vmax={vmax}
        caption={tr('全部任务合计，只记有下载在跑的时段')}
      />

      <h3>{tr('每日下载量')}</h3>
      <div className="stats-bars">
        {days.map((d) => (
          <div className={`stats-bar ${d.today ? 'today' : ''}`} key={d.date}>
            <div className="stats-bar-value">{d.bytes ? formatBytes(d.bytes) : ''}</div>
            <div
              className="stats-bar-fill"
              style={{height: `${Math.max(2, Math.round((d.bytes / maxDay) * 72))}px`}}
            />
            <div className="stats-bar-label">{d.label}</div>
          </div>
        ))}
      </div>

      <h3>{tr('单个任务')}</h3>
      {(stats?.tasks.length ?? 0) === 0 ? (
        <div className="empty small">
          <p>{tr('还没有下载记录')}</p>
        </div>
      ) : (
        <>
          <div className="field-grid">
            <label className="field">
              <span>{tr('任务')}</span>
              <button
                type="button"
                className="picker-trigger"
                onClick={() => setPickerOpen(true)}
              >
                <span className="picker-trigger-name">
                  {taskStat ? taskStat.fileName || taskStat.url : tr('选择任务')}
                </span>
                <em className="collapse-caret">▾</em>
              </button>
            </label>
          </div>
          {pickerOpen && (
            <div
              className="overlay picker-overlay"
              onMouseDown={(e) => e.target === e.currentTarget && setPickerOpen(false)}
            >
              <div className="picker-panel">
                <h3>{tr('选择任务')}</h3>
                <div className="picker-list">
                  {(stats?.tasks ?? []).map((x) => {
                    const active = x.taskId === (taskStat?.taskId ?? '');
                    return (
                      <button
                        key={x.taskId}
                        type="button"
                        className={`picker-item${active ? ' active' : ''}`}
                        onClick={() => {
                          setPicked(x.taskId);
                          setPickerOpen(false);
                        }}
                      >
                        <span className="picker-item-name">{x.fileName || x.url}</span>
                        <span className="picker-item-meta">
                          {active ? '✓ ' : ''}
                          {formatBytes(x.bytes)}
                        </span>
                      </button>
                    );
                  })}
                </div>
                <div className="picker-actions">
                  <button className="btn ghost" onClick={() => setPickerOpen(false)}>
                    {tr('取消')}
                  </button>
                </div>
              </div>
            </div>
          )}
          {taskStat && (
            <>
              <div className="stats-task-meta">
                <span>
                  {tr('下载量')}：{formatBytes(taskStat.bytes)}
                </span>
                <span>
                  {tr('平均速度')}：{taskStat.avgSpeed ? formatSpeed(taskStat.avgSpeed) : '—'}
                </span>
                <span>
                  {tr('峰值')}：{taskStat.peakSpeed ? formatSpeed(taskStat.peakSpeed) : '—'}
                </span>
                <span>
                  {tr('下载耗时')}：{formatDuration(taskStat.activeMs)}
                </span>
              </div>
              <SpeedChart
                points={taskSamples}
                t0={taskWin.t0}
                t1={taskWin.t1}
                vmax={vmax}
                caption={tr('曲线保留最近 15 个任务，更早的只留汇总')}
              />
            </>
          )}
        </>
      )}

      <h3>{tr('今日结束的任务')}</h3>
      <div className="stats-task-meta">
        <span>
          {tr('已完成')}：{stats?.todayCompleted ?? 0}
        </span>
        <span>
          {tr('失败')}：{stats?.todayFailed ?? 0}
        </span>
        <span>
          {tr('当前队列')}：{stats?.running ?? 0} {tr('下载中')} / {stats?.queued ?? 0} {tr('排队中')} /{' '}
          {stats?.paused ?? 0} {tr('已暂停')}
        </span>
      </div>

      <div className="dialog-actions">
        <button className="btn ghost" onClick={onBack}>
          {tr('返回')}
        </button>
      </div>
    </div>
  );
}

import {useCallback, useEffect, useMemo, useRef, useState} from 'react';

import type {QueueStatus, Status, Task} from '../types';
import {isScheduled} from '../types';
import {t, useI18n} from '../i18n';
import {formatBytes, formatDateTime, formatDuration, formatETA, formatSpeed, percent, statusLabel, checksumStatusLabel, totalElapsedMs} from '../lib/format';
import {clockLabel} from '../lib/stats';
import {api} from '../api';
import {FolderIcon, PauseIcon, PlayIcon, RetryIcon, TrashIcon} from './icons';
import DeleteTaskDialog from './DeleteTaskDialog';
import TaskDetailDialog from './TaskDetailDialog';

interface Props {
  tasks: Task[];
  onChanged: () => void;
  /** 队列动作后让父组件回读设置：闸门与时间窗都可能被 REST 路径改过 */
  onSettingsChanged: () => void;
}

interface RowActions {
  onChanged: () => void;
  onRequestDelete: (t: Task) => void;
  onRequestDetail: (t: Task) => void;
}

const FILTERS: {key: 'all' | Status; label: string}[] = [
  {key: 'all', label: '全部'},
  {key: 'running', label: '下载中'},
  {key: 'queued', label: '排队中'},
  {key: 'paused', label: '已暂停'},
  {key: 'completed', label: '已完成'},
  {key: 'failed', label: '失败'},
];

function TaskRow({
  task,
  onChanged,
  onRequestDelete,
  onRequestDetail,
}: RowActions & {task: Task}) {
  const {t} = useI18n();
  const pct = percent(task);
  const running = task.status === 'running';
  // 剩余时间 = 未下载字节 / 当前速度
  const eta =
    running && task.totalSize > 0 && task.speed > 0
      ? Math.round((task.totalSize - task.downloaded) / task.speed)
      : 0;
  const [localErr, setLocalErr] = useState('');
  const errTimer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(errTimer.current), []);

  const showError = (msg: string) => {
    setLocalErr(msg);
    window.clearTimeout(errTimer.current);
    errTimer.current = window.setTimeout(() => setLocalErr(''), 5000);
  };

  const toggle = async () => {
    try {
      if (task.status === 'running') {
        await api.pauseTask(task.id);
      } else if (task.status === 'paused' || task.status === 'failed') {
        await api.resumeTask(task.id);
      }
      onChanged();
    } catch (e) {
      showError(t('操作失败：{e}', {e: String(e)}));
    }
  };

  const move = async (delta: -1 | 1) => {
    try {
      await api.moveTask(task.id, delta);
      onChanged();
    } catch (e) {
      showError(t('排序失败：{e}', {e: String(e)}));
    }
  };

  const openFolder = async () => {
    try {
      await api.openFolder(task.saveDir, task.fileName);
    } catch (e) {
      showError(t('打开目录失败：{e}', {e: String(e)}));
    }
  };

  const scheduled = isScheduled(task);
  const prioLabel =
    task.priority === 2 ? t('高优') : task.priority === 0 ? t('低优') : '';
  // 上移/下移只在排队中有意义：出队时同优先级按列表顺序取，移动会改变
  // 启动顺序；其余状态下移动纯属装饰，常驻显示徒增按钮拥挤
  const canMove = task.status === 'queued';

  return (
    <div className={`task-row status-${task.status}`} onClick={() => onRequestDetail(task)}>
      <div className="task-main">
        <div className="task-title">
          <span className="task-name" title={task.url}>
            {task.fileName}
          </span>
          <span className={`chip ${scheduled ? 'queued' : task.status}`}>
            {scheduled ? t('定时') : statusLabel(task.status)}
          </span>
          {prioLabel && <span className="chip running">{prioLabel}</span>}
        </div>
        <div className="task-sub">
          <span>
            {formatBytes(task.downloaded)}
            {task.totalSize > 0 ? ` / ${formatBytes(task.totalSize)}` : ''}
          </span>
          {task.totalSize > 0 && <span className="task-pct">{pct}%</span>}
          <span>{t('{n} 连接', {n: task.connections})}</span>
          {task.speedLimit > 0 && <span>{t('限 {v}', {v: formatSpeed(task.speedLimit)})}</span>}
          {scheduled && (
            <span>
              {t('{v} 开始', {v: formatDateTime(task.startAt)})}
            </span>
          )}
          {running && <span className="task-speed">{formatSpeed(task.speed) || '—'}</span>}
          {running && eta > 0 && <span>{t('剩余 {v}', {v: formatETA(eta)})}</span>}
          {/* 写不下时任务依旧停在队列里：这里只说明原因，不把它当成失败 */}
          {task.status === 'queued' && task.blockReason && (
            <span className="task-block" title={t('腾出空间后会自动继续下载')}>
              {task.blockReason}
            </span>
          )}
          {(task.status === 'completed' || task.status === 'failed') && (
            <span title={t('纯下载耗时 / 总耗时（含暂停，已冻结）')}>
              {t('用时')} {formatDuration(task.activeMs || 0)} · {t('总')} {formatDuration(totalElapsedMs(task))}
              {task.avgSpeed > 0 && ` · ${t('均')} ${formatSpeed(task.avgSpeed)}`}
            </span>
          )}
          {task.checksumStatus === 'ok' && <span className="chip completed">{t('校验通过')}</span>}
          {task.checksumStatus === 'mismatch' && <span className="chip failed">{t('校验失败')}</span>}
          {(localErr || (task.status === 'failed' && task.error)) && (
            <span className="task-error" title={localErr || task.error}>
              {localErr || task.error}
            </span>
          )}
        </div>
        <div className="progress">
          <div
            className={`progress-fill ${pct >= 100 ? 'done' : ''}`}
            style={{width: `${pct}%`}}
          />
        </div>
      </div>
      <div className="task-actions" onClick={(e) => e.stopPropagation()}>
        {canMove && (
          <>
            <button className="btn icon" title={t('上移')} onClick={() => move(-1)}>
              ↑
            </button>
            <button className="btn icon" title={t('下移')} onClick={() => move(1)}>
              ↓
            </button>
          </>
        )}
        <button className="btn icon" title={t('打开所在目录')} onClick={openFolder}>
          <FolderIcon />
        </button>
        {(task.status === 'running' || task.status === 'paused' || task.status === 'failed') && (
          <button
            className="btn icon"
            title={running ? t('暂停') : task.status === 'failed' ? t('重试') : t('继续')}
            onClick={toggle}
          >
            {running ? <PauseIcon /> : task.status === 'failed' ? <RetryIcon /> : <PlayIcon />}
          </button>
        )}
        <button className="btn icon danger" title={t('删除任务')} onClick={() => onRequestDelete(task)}>
          <TrashIcon />
        </button>
      </div>
    </div>
  );
}

export default function TaskList({tasks, onChanged, onSettingsChanged}: Props) {
  const {t} = useI18n();
  const [filter, setFilter] = useState<'all' | Status>('all');
  const [query, setQuery] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<Task | null>(null);
  // 只存 ID：详情弹窗始终用列表里的实时任务，下载耗时才能跟着 progressLoop 刷新
  const [detailId, setDetailId] = useState<string | null>(null);
  const detailTask = detailId ? (tasks.find((t) => t.id === detailId) ?? null) : null;

  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [confirmClear, setConfirmClear] = useState(false);
  const noticeTimer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(noticeTimer.current), []);

  const flash = (msg: string) => {
    setNotice(msg);
    window.clearTimeout(noticeTimer.current);
    noticeTimer.current = window.setTimeout(() => setNotice(''), 6000);
  };

  const counts = useMemo(() => {
    const c = new Map<string, number>();
    for (const t of tasks) c.set(t.status, (c.get(t.status) ?? 0) + 1);
    return c;
  }, [tasks]);

  const failedCount = counts.get('failed') ?? 0;
  const completedCount = counts.get('completed') ?? 0;
  // 「暂停全部」对未结束的任务有意义；「恢复全部」对暂停与失败有意义
  const activeCount = (counts.get('running') ?? 0) + (counts.get('queued') ?? 0);
  const resumableCount = (counts.get('paused') ?? 0) + failedCount;
  // 定时未到点的任务也停在队列里（dispatch 会跳过 StartAt 在未来的任务）
  const scheduledWaiting = tasks.filter((x) => x.status === 'queued' && isScheduled(x)).length;

  // 闸门/时间窗/空间不足三件事都会让队列停摆，以引擎为准（本地时间推不出服务
  // 端的窗口判定）；轮询间隔 2s，与队列动作后的主动刷新共用一套逻辑
  const [status, setStatus] = useState<QueueStatus | null>(null);
  const refreshStatus = useCallback(() => {
    api
      .queueStatus()
      .then(setStatus)
      .catch(() => {});
  }, []);
  useEffect(() => {
    refreshStatus();
    const timer = window.setInterval(refreshStatus, 2000);
    return () => window.clearInterval(timer);
  }, [refreshStatus]);

  const gateClosed = !!status?.suspended;
  const windowBlocked = !!status?.windowEnabled && !!status && !status.windowActive;
  const opensAt = status?.opensAt ? clockLabel(Date.parse(status.opensAt)) : status?.windowStart ?? '';

  // 横幅只说一件事，按严重程度排：操作反馈 > 用户闭闸 > 时间窗外 > 定时未到点 > 空间不足
  const queueNote = gateClosed
    ? t('队列已暂停，任务不会开始')
    : windowBlocked
      ? t('带宽时间窗外，{v} 自动放行', {v: opensAt})
      : scheduledWaiting > 0
        ? t('{n} 个任务定时未到点', {n: scheduledWaiting})
        : (status?.blockedTasks ?? 0) > 0
          ? t('{n} 个任务因磁盘空间不足停在队列里', {n: status?.blockedTasks ?? 0})
          : '';

  const runBatch = async (fn: () => Promise<number>, message: (n: number) => string) => {
    if (busy) return;
    setBusy(true);
    try {
      flash(message(await fn()));
      onChanged();
    } catch (e) {
      flash(t('操作失败：{e}', {e: String(e)}));
    } finally {
      setBusy(false);
    }
  };

  const toggleGate = async () => {
    try {
      await api.setQueueSuspended(!gateClosed);
      onChanged();
      // 闸门状态存在设置里：回读引擎与父组件的设置，保证按钮与横幅一致
      refreshStatus();
      onSettingsChanged();
    } catch (e) {
      flash(t('操作失败：{e}', {e: String(e)}));
    }
  };

  const clearDone = async () => {
    if (!confirmClear) {
      setConfirmClear(true);
      flash(t('再次点击确认：仅移除记录，文件保留在磁盘'));
      return;
    }
    setConfirmClear(false);
    await runBatch(api.clearCompleted, (n) => t('已清理 {n} 条已完成记录', {n}));
  };

  // 完成数一变（新增完成或刚清理完）就解除待确认状态，避免按钮停在危险态
  useEffect(() => {
    setConfirmClear(false);
  }, [completedCount]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return tasks.filter(
      (t) =>
        (filter === 'all' || t.status === filter) &&
        (q === '' || t.fileName.toLowerCase().includes(q) || t.url.toLowerCase().includes(q)),
    );
  }, [tasks, filter, query]);

  if (tasks.length === 0) {
    return (
      <div className="empty">
        <div className="empty-icon">⇣</div>
        <p>{t('还没有下载任务')}</p>
        <p className="empty-hint">{t('点击右上角「新建下载」，粘贴链接开始')}</p>
      </div>
    );
  }

  return (
    <div className="task-list">
      <div className="toolbar">
        <div className="filter-chips">
          {FILTERS.map((f) => (
            <button
              key={f.key}
              className={`filter-chip ${filter === f.key ? 'active' : ''}`}
              onClick={() => setFilter(f.key)}
            >
              {t(f.label)}
              <em>{f.key === 'all' ? tasks.length : counts.get(f.key) ?? 0}</em>
            </button>
          ))}
        </div>
        <input
          className="search-input"
          type="text"
          placeholder={t('搜索文件名或链接…')}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="queue-actions">
          <button
            className={`btn small ghost ${gateClosed ? 'active' : ''}`}
            onClick={toggleGate}
            disabled={busy}
            title={
              gateClosed
                ? t('开闸后已排队的任务会按优先级继续启动')
                : t('闭闸后不再启动任何任务，正在下载的不受影响')
            }
          >
            {gateClosed ? t('恢复队列') : t('暂停队列')}
          </button>
          {/* 暂停全部与恢复全部按队列聚合状态二选一：有未结束的任务就给「暂停全部」，
              否则给「恢复全部」，两个动作不会互相踩踏 */}
          {activeCount > 0 ? (
            <button
              className="btn small ghost"
              disabled={busy}
              title={t('暂停所有排队中与下载中的任务')}
              onClick={() => runBatch(api.pauseAll, (n) => t('已暂停 {n} 个任务，运行中的已停止', {n}))}
            >
              {t('暂停全部')}
            </button>
          ) : (
            <button
              className="btn small ghost"
              disabled={busy || resumableCount === 0}
              title={t('恢复所有已暂停与失败的任务')}
              onClick={() => runBatch(api.resumeAll, (n) => t('已恢复 {n} 个任务', {n}))}
            >
              {t('恢复全部')}
            </button>
          )}
          {failedCount > 0 && (
            <button
              className="btn small ghost"
              disabled={busy}
              onClick={() => runBatch(api.retryAllFailed, (n) => t('已重试 {n} 个失败任务', {n}))}
            >
              {t('重试 {n} 个失败', {n: failedCount})}
            </button>
          )}
          {completedCount > 0 && (
            <button
              className={`btn small ghost ${confirmClear ? 'danger' : ''}`}
              disabled={busy}
              onClick={clearDone}
            >
              {t('清理 {n} 个已完成', {n: completedCount})}
            </button>
          )}
        </div>
      </div>
      {/* 横幅只说需要说明的事：操作反馈与队列停摆原因；批量动作常驻工具栏 */}
      {(notice || queueNote) && (
        <div className={`queue-banner ${gateClosed || windowBlocked ? 'warn' : ''}`}>
          <span className="queue-banner-text">{notice || queueNote}</span>
        </div>
      )}
      {filtered.length === 0 ? (
        <div className="empty small">
          <p>{t('没有符合条件的项目')}</p>
        </div>
      ) : (
        filtered.map((t) => (
          <TaskRow
            key={t.id}
            task={t}
            onChanged={onChanged}
            onRequestDelete={setDeleteTarget}
            onRequestDetail={(t) => setDetailId(t.id)}
          />
        ))
      )}
      {deleteTarget && (
        <DeleteTaskDialog
          task={deleteTarget}
          onClose={() => setDeleteTarget(null)}
          onDeleted={onChanged}
        />
      )}
      {detailTask && (
        <TaskDetailDialog task={detailTask} onClose={() => setDetailId(null)} />
      )}
    </div>
  );
}

import {useEffect, useMemo, useRef, useState} from 'react';

import type {Status, Task} from '../types';
import {isScheduled} from '../types';
import {t, useI18n} from '../i18n';
import {formatBytes, formatDateTime, formatDuration, formatETA, formatSpeed, percent, statusLabel, checksumStatusLabel, totalElapsedMs} from '../lib/format';
import {api} from '../api';
import {FolderIcon, PauseIcon, PlayIcon, RetryIcon, TrashIcon} from './icons';
import DeleteTaskDialog from './DeleteTaskDialog';
import TaskDetailDialog from './TaskDetailDialog';

interface Props {
  tasks: Task[];
  onChanged: () => void;
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

export default function TaskList({tasks, onChanged}: Props) {
  const {t} = useI18n();
  const [filter, setFilter] = useState<'all' | Status>('all');
  const [query, setQuery] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<Task | null>(null);
  // 只存 ID：详情弹窗始终用列表里的实时任务，下载耗时才能跟着 progressLoop 刷新
  const [detailId, setDetailId] = useState<string | null>(null);
  const detailTask = detailId ? (tasks.find((t) => t.id === detailId) ?? null) : null;

  const counts = useMemo(() => {
    const c = new Map<string, number>();
    for (const t of tasks) c.set(t.status, (c.get(t.status) ?? 0) + 1);
    return c;
  }, [tasks]);

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
      </div>
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

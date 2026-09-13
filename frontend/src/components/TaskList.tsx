import {useEffect, useRef, useState} from 'react';

import type {Task} from '../types';
import {formatBytes, formatSpeed, percent, statusMeta} from '../lib/format';
import {api} from '../api';

interface Props {
  tasks: Task[];
  onChanged: () => void;
}

// 平滑速度显示：引擎每 500ms 推一次原始速率，做轻量指数平滑避免数字跳动
function useSmoothedSpeed(tasks: Task[]): Map<string, number> {
  const prev = useRef(new Map<string, number>());
  const out = new Map<string, number>();
  for (const t of tasks) {
    let s = t.speed;
    if (t.status !== 'running') {
      s = 0;
    } else {
      const p = prev.current.get(t.id);
      if (p !== undefined) s = Math.round(p * 0.4 + s * 0.6);
    }
    prev.current.set(t.id, s);
    out.set(t.id, s);
  }
  return out;
}

function TaskRow({task, onChanged}: {task: Task; onChanged: () => void}) {
  const meta = statusMeta[task.status];
  const pct = percent(task);
  const running = task.status === 'running';
  // Wails WebView 中 window.confirm/alert 不可靠，删除采用两段式确认，错误行内展示
  const [confirming, setConfirming] = useState(false);
  const [localErr, setLocalErr] = useState('');
  const confirmTimer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(confirmTimer.current), []);

  const showError = (msg: string) => {
    setLocalErr(msg);
    window.setTimeout(() => setLocalErr(''), 5000);
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
      showError(`操作失败：${e}`);
    }
  };

  const remove = async () => {
    if (!confirming) {
      setConfirming(true);
      confirmTimer.current = window.setTimeout(() => setConfirming(false), 2500);
      return;
    }
    window.clearTimeout(confirmTimer.current);
    setConfirming(false);
    try {
      await api.removeTask(task.id);
      onChanged();
    } catch (e) {
      showError(`删除失败：${e}`);
    }
  };

  return (
    <div className={`task-row status-${task.status}`}>
      <div className="task-main">
        <div className="task-title">
          <span className="task-name" title={task.url}>
            {task.fileName}
          </span>
          <span className={`chip ${meta.cls}`}>{meta.label}</span>
        </div>
        <div className="task-sub">
          <span>
            {formatBytes(task.downloaded)}
            {task.totalSize > 0 ? ` / ${formatBytes(task.totalSize)}` : ''}
          </span>
          <span>{task.connections} 连接</span>
          <span className="task-speed">{formatSpeed(task.speed)}</span>
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
      <div className="task-actions">
        {(task.status === 'running' || task.status === 'paused' || task.status === 'failed') && (
          <button className="btn ghost small" onClick={toggle}>
            {running ? '暂停' : task.status === 'failed' ? '重试' : '继续'}
          </button>
        )}
        <button
          className={`btn small ${confirming ? 'confirm-delete' : 'ghost danger'}`}
          onClick={remove}
        >
          {confirming ? '确认删除？' : '删除'}
        </button>
      </div>
    </div>
  );
}

export default function TaskList({tasks, onChanged}: Props) {
  useSmoothedSpeed(tasks); // 保持平滑缓存更新

  if (tasks.length === 0) {
    return (
      <div className="empty">
        <div className="empty-icon">⇣</div>
        <p>还没有下载任务</p>
        <p className="empty-hint">点击右上角「新建下载」，粘贴链接开始</p>
      </div>
    );
  }
  return (
    <div className="task-list">
      {tasks.map((t) => (
        <TaskRow key={t.id} task={t} onChanged={onChanged} />
      ))}
    </div>
  );
}

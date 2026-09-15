import {useEffect, useState} from 'react';

import {ClipboardSetText} from '../../wailsjs/runtime/runtime';

import type {Task} from '../types';
import {
  activeElapsedMs,
  checksumStatusMeta,
  formatBytes,
  formatDuration,
  formatSpeed,
  percent,
  statusMeta,
  totalElapsedMs,
} from '../lib/format';
import {CopyIcon} from './icons';

interface Props {
  task: Task;
  onClose: () => void;
}

// DetailRow 一行详情 + 复制按钮。
function DetailRow({label, value, mono, onCopy, copied}: {
  label: string;
  value: string;
  mono?: boolean;
  onCopy?: () => void;
  copied?: boolean;
}) {
  return (
    <div className="detail-row">
      <span className="detail-label">{label}</span>
      <span className={`detail-value ${mono ? 'mono' : ''}`} title={value}>
        {value}
      </span>
      {onCopy && (
        <button
          className="btn icon copy-btn"
          title="复制"
          onClick={() => {
            onCopy();
          }}
        >
          {copied ? <span className="copied-text">已复制</span> : <CopyIcon size={13} />}
        </button>
      )}
    </div>
  );
}

export default function TaskDetailDialog({task, onClose}: Props) {
  const [copied, setCopied] = useState('');
  // 暂停/排队时后端不再推进度，总耗时靠本地秒表继续走；完成后 totalElapsedMs 已冻结
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  const meta = statusMeta[task.status];
  const fullPath = `${task.saveDir.replace(/\//g, '\\')}\\${task.fileName}`;

  const copy = (text: string, key: string) => {
    ClipboardSetText(text).finally(() => {
      setCopied(key);
      window.setTimeout(() => setCopied(''), 1500);
    });
  };

  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog dialog-compact">
        <h2>任务详情</h2>
        <DetailRow
          label="文件名"
          value={task.fileName}
          onCopy={() => copy(task.fileName, 'name')}
          copied={copied === 'name'}
        />
        <DetailRow
          label="链接"
          value={task.url}
          mono
          onCopy={() => copy(task.url, 'url')}
          copied={copied === 'url'}
        />
        <DetailRow
          label="下载路径"
          value={fullPath}
          mono
          onCopy={() => copy(fullPath, 'path')}
          copied={copied === 'path'}
        />
        <DetailRow
          label="大小"
          value={
            task.totalSize > 0
              ? `${formatBytes(task.downloaded)} / ${formatBytes(task.totalSize)}（${percent(task)}%）`
              : formatBytes(task.downloaded)
          }
        />
        <DetailRow label="状态" value={meta.label} />
        <DetailRow label="连接数" value={String(task.connections)} />
        <DetailRow
          label="下载耗时"
          value={formatDuration(activeElapsedMs(task))}
          // 纯下载：暂停期间不累计；完成后冻结
        />
        <DetailRow
          label="总耗时"
          value={formatDuration(totalElapsedMs(task, now))}
          // 创建到结束（含暂停）；已完成/失败后不再随时间增长
        />
        <DetailRow
          label="平均速度"
          value={formatSpeed(task.avgSpeed || 0) || '—'}
        />
        {(task.checksumStatus || task.checksumActual || task.checksumExpected) && (
          <DetailRow
            label="校验和"
            value={
              [
                task.checksumAlgo ? task.checksumAlgo.toUpperCase() : '',
                task.checksumActual ? `实际 ${task.checksumActual.slice(0, 16)}…` : '',
                task.checksumStatus ? (checksumStatusMeta[task.checksumStatus]?.label ?? task.checksumStatus) : '',
              ]
                .filter(Boolean)
                .join(' · ') || '—'
            }
            mono
          />
        )}
        {task.checksumExpected && (
          <DetailRow
            label="期望校验值"
            value={task.checksumExpected}
            mono
            onCopy={() => copy(task.checksumExpected ?? '', 'checksum')}
            copied={copied === 'checksum'}
          />
        )}
        <DetailRow
          label="创建时间"
          value={new Date(task.createdAt).toLocaleString('zh-CN', {hour12: false})}
        />
        {task.finishedAt &&
          !task.finishedAt.startsWith('0001-01-01') &&
          (task.status === 'completed' || task.status === 'failed') && (
            <DetailRow
              label="结束时间"
              value={new Date(task.finishedAt).toLocaleString('zh-CN', {hour12: false})}
            />
          )}
        <div className="dialog-actions">
          <button className="btn primary" onClick={onClose}>
            关闭
          </button>
        </div>
      </div>
    </div>
  );
}

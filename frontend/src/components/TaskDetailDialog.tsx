import {useState} from 'react';

import {ClipboardSetText} from '../../wailsjs/runtime/runtime';

import type {Task} from '../types';
import {formatBytes, percent, statusMeta} from '../lib/format';
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
          label="创建时间"
          value={new Date(task.createdAt).toLocaleString('zh-CN', {hour12: false})}
        />
        <div className="dialog-actions">
          <button className="btn primary" onClick={onClose}>
            关闭
          </button>
        </div>
      </div>
    </div>
  );
}

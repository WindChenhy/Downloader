import {useEffect, useState} from 'react';

import type {Task} from '../types';
import {
  activeElapsedMs,
  checksumStatusLabel,
  formatBytes,
  formatDateTime,
  formatDuration,
  formatSpeed,
  percent,
  statusLabel,
  totalElapsedMs,
} from '../lib/format';
import {t, useI18n} from '../i18n';
import {CopyIcon} from './icons';

interface Props {
  task: Task;
  onClose: () => void;
}

// maskSecret 认证头常含会话令牌：详情里只露长度，原文经复制取用。
function maskSecret(v: string): string {
  return `${'•'.repeat(Math.min(v.length, 12))}（${v.length}）`;
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
          title={t('复制')}
          onClick={() => {
            onCopy();
          }}
        >
          {copied ? <span className="copied-text">{t('已复制')}</span> : <CopyIcon size={13} />}
        </button>
      )}
    </div>
  );
}

export default function TaskDetailDialog({task, onClose}: Props) {
  const {t} = useI18n();
  const [copied, setCopied] = useState('');
  // 暂停/排队时后端不再推进度，总耗时靠本地秒表继续走；完成后 totalElapsedMs 已冻结
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  const fullPath = `${task.saveDir.replace(/\//g, '\\')}\\${task.fileName}`;

  // WebView2 / Android WebView 均支持 navigator.clipboard；
  // 文档未聚焦时 writeText 可能被拒，退回 execCommand 兜底
  const copy = (text: string, key: string) => {
    const markCopied = () => {
      setCopied(key);
      window.setTimeout(() => setCopied(''), 1500);
    };
    navigator.clipboard?.writeText(text).then(markCopied).catch(() => {
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      ta.remove();
      markCopied();
    });
  };

  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog dialog-compact">
        <h2>{t('任务详情')}</h2>
        <div className="dialog-body">
          <DetailRow
            label={t('文件名')}
            value={task.fileName}
            onCopy={() => copy(task.fileName, 'name')}
            copied={copied === 'name'}
          />
          <DetailRow
            label={t('链接')}
            value={task.url}
            mono
            onCopy={() => copy(task.url, 'url')}
            copied={copied === 'url'}
          />
          <DetailRow
            label={t('下载路径')}
            value={fullPath}
            mono
            onCopy={() => copy(fullPath, 'path')}
            copied={copied === 'path'}
          />
          <DetailRow
            label={t('大小')}
            value={
              task.totalSize > 0
                ? `${formatBytes(task.downloaded)} / ${formatBytes(task.totalSize)}（${percent(task)}%）`
                : formatBytes(task.downloaded)
            }
          />
          <DetailRow label={t('状态')} value={statusLabel(task.status)} />
          {/* 磁盘空间不足时任务依旧停在队列里，这里说清等什么，不让人误以为卡住了 */}
          {task.blockReason && <DetailRow label={t('为何未开始')} value={task.blockReason} />}
          <DetailRow label={t('连接数')} value={String(task.connections)} />
          <DetailRow
            label={t('优先级')}
            value={task.priority === 2 ? t('高') : task.priority === 0 ? t('低') : t('普通')}
          />
          <DetailRow
            label={t('限速')}
            value={task.speedLimit > 0 ? formatSpeed(task.speedLimit) : t('跟随全局')}
          />
          {task.cookie && (
            <DetailRow
              label={t('Cookie')}
              value={maskSecret(task.cookie)}
              mono
              onCopy={() => copy(task.cookie ?? '', 'cookie')}
              copied={copied === 'cookie'}
            />
          )}
          {task.referer && (
            <DetailRow
              label={t('Referer')}
              value={task.referer}
              mono
              onCopy={() => copy(task.referer ?? '', 'referer')}
              copied={copied === 'referer'}
            />
          )}
          {formatDateTime(task.startAt) && (
            <DetailRow label={t('定时开始')} value={formatDateTime(task.startAt)} />
          )}
          <DetailRow
            label={t('下载耗时')}
            value={formatDuration(activeElapsedMs(task))}
            // 纯下载：暂停期间不累计；完成后冻结
          />
          <DetailRow
            label={t('总耗时')}
            value={formatDuration(totalElapsedMs(task, now))}
            // 创建到结束（含暂停）；已完成/失败后不再随时间增长
          />
          <DetailRow
            label={t('平均速度')}
            value={formatSpeed(task.avgSpeed || 0) || '—'}
          />
          {(task.checksumStatus || task.checksumActual || task.checksumExpected) && (
            <DetailRow
              label={t('校验和')}
              value={
                [
                  task.checksumAlgo ? task.checksumAlgo.toUpperCase() : '',
                  task.checksumActual ? `${t('实际')} ${task.checksumActual.slice(0, 16)}…` : '',
                  task.checksumStatus ? checksumStatusLabel(task.checksumStatus) : '',
                ]
                  .filter(Boolean)
                  .join(' · ') || '—'
              }
              mono
            />
          )}
          {task.checksumExpected && (
            <DetailRow
              label={t('期望校验值')}
              value={task.checksumExpected}
              mono
              onCopy={() => copy(task.checksumExpected ?? '', 'checksum')}
              copied={copied === 'checksum'}
            />
          )}
          <DetailRow label={t('创建时间')} value={formatDateTime(task.createdAt) || '—'} />
          {(task.status === 'completed' || task.status === 'failed') &&
            formatDateTime(task.finishedAt) && (
              <DetailRow label={t('结束时间')} value={formatDateTime(task.finishedAt)} />
            )}
        </div>
        <div className="dialog-actions">
          <button className="btn primary" onClick={onClose}>
            {t('关闭')}
          </button>
        </div>
      </div>
    </div>
  );
}

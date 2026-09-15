import {useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent} from 'react';

import type {Settings} from '../types';
import {api} from '../api';
import {parseAndExpandUrls} from '../lib/format';

interface Props {
  settings: Settings;
  initialUrl?: string;
  onClose: () => void;
  onAdded: () => void;
}

/** 按内容高度自适应：先重置再量 scrollHeight，避免越撑越高。 */
function autoGrow(el: HTMLTextAreaElement | null) {
  if (!el) return;
  el.style.height = 'auto';
  el.style.height = `${el.scrollHeight}px`;
}

function toLocalInputValue(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export default function AddTaskDialog({settings, initialUrl, onClose, onAdded}: Props) {
  const [url, setUrl] = useState(initialUrl ?? '');
  const [customName, setCustomName] = useState('');
  const [saveDir, setSaveDir] = useState(settings.saveDir);
  const [connections, setConnections] = useState(settings.connections);
  const [categoryIdx, setCategoryIdx] = useState(-1);
  const [checksumAlgo, setChecksumAlgo] = useState('');
  const [checksumExpected, setChecksumExpected] = useState('');
  const [priority, setPriority] = useState(1);
  const [startAtLocal, setStartAtLocal] = useState('');
  const [speedLimitKb, setSpeedLimitKb] = useState(0);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const urlRef = useRef<HTMLTextAreaElement>(null);

  const urls = useMemo(() => parseAndExpandUrls(url), [url]);
  const multi = urls.length > 1;

  useEffect(() => {
    if (initialUrl) setUrl(initialUrl);
  }, [initialUrl]);

  useEffect(() => {
    autoGrow(urlRef.current);
  }, [url]);

  const onUrlChange = useCallback((e: ChangeEvent<HTMLTextAreaElement>) => {
    setUrl(e.target.value);
    autoGrow(e.target);
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const submit = async () => {
    if (urls.length === 0) {
      setError('请输入至少一条 http/https 下载链接（支持多行批量与 {1..10} 序列）');
      return;
    }
    setBusy(true);
    setError('');
    try {
      let startAt = '';
      if (startAtLocal) {
        const ts = new Date(startAtLocal);
        if (!Number.isNaN(ts.getTime())) startAt = ts.toISOString();
      }
      const items = urls.map((u) => ({
        url: u,
        saveDir: saveDir.trim(),
        connections,
        customName: multi ? '' : customName.trim(),
        checksumAlgo: multi ? '' : checksumAlgo,
        checksumExpected: multi ? '' : checksumExpected.trim(),
        priority,
        startAt,
        speedLimit: Math.max(0, speedLimitKb) * 1024,
      }));
      const result = await api.addTasks(items);
      onAdded();
      if (result.errors?.length) {
        setError(
          `已创建 ${result.tasks?.length ?? 0} 个任务；${result.errors.length} 条失败：\n` +
            result.errors.join('\n'),
        );
        setBusy(false);
      } else {
        onClose();
      }
    } catch (e) {
      setError(String(e).replace(/^.*:\s*/, ''));
      setBusy(false);
    }
  };

  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog">
        <h2>新建下载</h2>
        <div className="dialog-body">
        <label className="field">
          <span>
            下载链接
            {urls.length > 0 && (
              <em style={{fontStyle: 'normal', opacity: 0.65, marginLeft: 8}}>
                已识别 {urls.length} 条{multi ? '（批量）' : ''}
              </em>
            )}
          </span>
          <textarea
            ref={urlRef}
            autoFocus
            rows={1}
            className="field-textarea"
            placeholder="粘贴链接；多行批量；支持序列如 https://x.com/f_{1..10}.zip"
            value={url}
            onChange={onUrlChange}
          />
        </label>
        <label className="field">
          <span>
            重命名（留空自动从链接获取）
            {multi && <em style={{fontStyle: 'normal', opacity: 0.65, marginLeft: 8}}>批量时忽略</em>}
          </span>
          <input
            type="text"
            value={customName}
            placeholder="可选，如： ubuntu-24.04.iso"
            disabled={multi}
            onChange={(e) => setCustomName(e.target.value)}
          />
        </label>
        <label className="field">
          <span>目录分类</span>
          <select
            value={categoryIdx}
            onChange={(e) => {
              const idx = Number(e.target.value);
              setCategoryIdx(idx);
              if (idx >= 0) {
                const c = settings.dirCategories?.[idx];
                if (c?.path) setSaveDir(c.path);
              }
            }}
          >
            <option value={-1}>默认 / 自定义</option>
            {(settings.dirCategories ?? []).map((c, i) => (
              <option key={i} value={i}>
                {c.name || '未命名'}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          <span>保存到</span>
          <input
            type="text"
            value={saveDir}
            placeholder="下载目录"
            onChange={(e) => {
              setSaveDir(e.target.value);
              setCategoryIdx(-1);
            }}
          />
        </label>
        <div className="field-grid">
          <label className="field">
            <span>连接数（1–128）</span>
            <input
              type="number"
              min={1}
              max={128}
              value={connections}
              onChange={(e) => setConnections(Number(e.target.value))}
            />
          </label>
          <label className="field">
            <span>优先级</span>
            <select value={priority} onChange={(e) => setPriority(Number(e.target.value))}>
              <option value={0}>低</option>
              <option value={1}>普通</option>
              <option value={2}>高</option>
            </select>
          </label>
          <label className="field">
            <span>限速（KB/s，0 不限）</span>
            <input
              type="number"
              min={0}
              value={speedLimitKb}
              onChange={(e) => setSpeedLimitKb(Math.max(0, Number(e.target.value)))}
            />
          </label>
          <label className="field">
            <span>定时开始（可选）</span>
            <input
              type="datetime-local"
              value={startAtLocal}
              onChange={(e) => setStartAtLocal(e.target.value)}
            />
          </label>
        </div>
        {!multi && (
          <div className="field-grid">
            <label className="field">
              <span>校验算法（可选）</span>
              <select value={checksumAlgo} onChange={(e) => setChecksumAlgo(e.target.value)}>
                <option value="">不校验</option>
                <option value="md5">MD5</option>
                <option value="sha1">SHA1</option>
                <option value="sha256">SHA256</option>
              </select>
            </label>
            <label className="field">
              <span>期望校验值（可选）</span>
              <input
                type="text"
                value={checksumExpected}
                placeholder="留空则仅计算摘要"
                onChange={(e) => setChecksumExpected(e.target.value)}
              />
            </label>
          </div>
        )}
        </div>
        {error && <div className="dialog-error" style={{whiteSpace: 'pre-wrap'}}>{error}</div>}
        <div className="dialog-actions">
          <button className="btn ghost" onClick={onClose} disabled={busy}>
            取消
          </button>
          <button className="btn primary" onClick={submit} disabled={busy}>
            {busy ? '添加中…' : multi ? `批量创建 ${urls.length} 个任务` : '开始下载'}
          </button>
        </div>
      </div>
    </div>
  );
}

// 避免未使用告警（保留导出便于测试本地时间格式）
export {toLocalInputValue};

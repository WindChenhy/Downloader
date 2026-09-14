import {useCallback, useEffect, useRef, useState, type ChangeEvent} from 'react';

import type {Settings} from '../types';
import {api} from '../api';

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

export default function AddTaskDialog({settings, initialUrl, onClose, onAdded}: Props) {
  const [url, setUrl] = useState(initialUrl ?? '');
  const [customName, setCustomName] = useState('');
  const [saveDir, setSaveDir] = useState(settings.saveDir);
  const [connections, setConnections] = useState(settings.connections);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const urlRef = useRef<HTMLTextAreaElement>(null);

  // 剪贴板监听推来新链接时更新预填内容
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
    if (!url.trim()) {
      setError('请输入下载链接');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await api.addTask(url.trim(), saveDir.trim(), connections, customName.trim());
      onAdded();
      onClose();
    } catch (e) {
      setError(String(e).replace(/^.*:\s*/, ''));
      setBusy(false);
    }
  };

  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog">
        <h2>新建下载</h2>
        <label className="field">
          <span>下载链接</span>
          <textarea
            ref={urlRef}
            autoFocus
            rows={1}
            className="field-textarea"
            placeholder="粘贴 http/https 链接"
            value={url}
            onChange={onUrlChange}
          />
        </label>
        <label className="field">
          <span>重命名（留空自动从链接获取）</span>
          <input
            type="text"
            value={customName}
            placeholder="可选，如： ubuntu-24.04.iso"
            onChange={(e) => setCustomName(e.target.value)}
          />
        </label>
        <label className="field">
          <span>保存到</span>
          <input
            type="text"
            value={saveDir}
            placeholder="下载目录"
            onChange={(e) => setSaveDir(e.target.value)}
          />
        </label>
        <label className="field">
          <span>连接数（1–128，多连接可提速）</span>
          <input
            type="number"
            min={1}
            max={128}
            value={connections}
            onChange={(e) => setConnections(Number(e.target.value))}
          />
        </label>
        {error && <div className="dialog-error">{error}</div>}
        <div className="dialog-actions">
          <button className="btn ghost" onClick={onClose} disabled={busy}>
            取消
          </button>
          <button className="btn primary" onClick={submit} disabled={busy}>
            {busy ? '添加中…' : '开始下载'}
          </button>
        </div>
      </div>
    </div>
  );
}

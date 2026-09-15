import {useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent} from 'react';

import type {Settings} from '../types';
import {api} from '../api';
import {parseDownloadUrls} from '../lib/format';

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
  const [categoryIdx, setCategoryIdx] = useState(-1); // -1 = 默认/自定义目录
  const [checksumAlgo, setChecksumAlgo] = useState('');
  const [checksumExpected, setChecksumExpected] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const urlRef = useRef<HTMLTextAreaElement>(null);

  const urls = useMemo(() => parseDownloadUrls(url), [url]);
  const multi = urls.length > 1;

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
    if (urls.length === 0) {
      setError('请输入至少一条 http/https 下载链接（可多行批量）');
      return;
    }
    setBusy(true);
    setError('');
    try {
      // 批量时不套用自定义文件名/校验和（避免多文件共用同一校验值）
      const items = urls.map((u) => ({
        url: u,
        saveDir: saveDir.trim(),
        connections,
        customName: multi ? '' : customName.trim(),
        checksumAlgo: multi ? '' : checksumAlgo,
        checksumExpected: multi ? '' : checksumExpected.trim(),
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
            placeholder="粘贴 http/https 链接；多行可批量创建"
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
                disabled={!checksumAlgo && !checksumExpected}
                onChange={(e) => setChecksumExpected(e.target.value)}
              />
            </label>
          </div>
        )}
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

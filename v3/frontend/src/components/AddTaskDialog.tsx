import {useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent} from 'react';

import type {Settings} from '../types';
import {api} from '../api';
import {parseAndExpandUrls} from '../lib/format';
import {registerBackHandler} from '../lib/backHandler';
import {t, useI18n} from '../i18n';

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
  const {t} = useI18n();
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
  const [cookie, setCookie] = useState('');
  const [referer, setReferer] = useState('');
  // 高级设置折叠区：默认收起，未展开时全部按默认值执行
  const [advanced, setAdvanced] = useState(false);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const urlRef = useRef<HTMLTextAreaElement>(null);

  const urls = useMemo(() => parseAndExpandUrls(url), [url]);
  const multi = urls.length > 1;

  // 高级区是否被改过：给折叠开关一个「已自定义」提示，收起也不丢已改的值
  const advancedTouched =
    categoryIdx >= 0 ||
    saveDir.trim() !== settings.saveDir ||
    priority !== 1 ||
    speedLimitKb > 0 ||
    startAtLocal !== '' ||
    checksumAlgo !== '' ||
    checksumExpected.trim() !== '' ||
    cookie.trim() !== '' ||
    referer.trim() !== '';

  useEffect(() => {
    if (initialUrl) setUrl(initialUrl);
  }, [initialUrl]);

  // Android 返回键：先关本弹窗
  useEffect(() => {
    return registerBackHandler(() => {
      onClose();
      return true;
    });
  }, [onClose]);

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
      setError(t('请输入至少一条 http/https 下载链接（支持多行批量与 {1..10} 序列）'));
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
        // 批量链接多来自同一站点，认证头对每条都生效
        cookie: cookie.trim(),
        referer: referer.trim(),
      }));
      const result = await api.addTasks(items);
      onAdded();
      if (result.errors?.length) {
        setError(
          t('已创建 {n} 个任务；{m} 条失败：\n', {n: result.tasks?.length ?? 0, m: result.errors.length}) +
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
        <h2>{t('新建下载')}</h2>
        <div className="dialog-body">
        <label className="field">
          <span>
            {t('下载链接')}
            {urls.length > 0 && (
              <em style={{fontStyle: 'normal', opacity: 0.65, marginLeft: 8}}>
                {t('已识别 {n} 条', {n: urls.length})}{multi ? t('（批量）') : ''}
              </em>
            )}
          </span>
          <textarea
            ref={urlRef}
            autoFocus
            rows={1}
            className="field-textarea"
            placeholder={t('粘贴链接；多行批量；支持序列如 https://x.com/f_{1..10}.zip')}
            value={url}
            onChange={onUrlChange}
          />
        </label>
        <label className="field">
          <span>
            {t('重命名（留空自动从链接获取）')}
            {multi && <em style={{fontStyle: 'normal', opacity: 0.65, marginLeft: 8}}>{t('批量时忽略')}</em>}
          </span>
          <input
            type="text"
            value={customName}
            placeholder={t('可选，如： ubuntu-24.04.iso')}
            disabled={multi}
            onChange={(e) => setCustomName(e.target.value)}
          />
        </label>
        <label className="field">
          <span>{t('连接数（1–128）')}</span>
          <input
            type="number"
            min={1}
            max={128}
            value={connections}
            onChange={(e) => setConnections(Number(e.target.value))}
          />
        </label>
        <button type="button" className="btn small ghost collapse-toggle" onClick={() => setAdvanced(!advanced)}>
          {t('高级设置')}{advancedTouched ? t(' · 已自定义') : ''}
          <em className="collapse-caret">{advanced ? '▴' : '▾'}</em>
        </button>
        {advanced && (
          <>
            <div className="field-grid">
              <label className="field">
                <span>{t('目录分类')}</span>
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
                  <option value={-1}>{t('默认 / 自定义')}</option>
                  {(settings.dirCategories ?? []).map((c, i) => (
                    <option key={i} value={i}>
                      {c.name || t('未命名')}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                <span>{t('保存到')}</span>
                <input
                  type="text"
                  value={saveDir}
                  placeholder={t('下载目录')}
                  onChange={(e) => {
                    setSaveDir(e.target.value);
                    setCategoryIdx(-1);
                  }}
                />
              </label>
              <label className="field">
                <span>{t('优先级')}</span>
                <select value={priority} onChange={(e) => setPriority(Number(e.target.value))}>
                  <option value={0}>{t('低')}</option>
                  <option value={1}>{t('普通')}</option>
                  <option value={2}>{t('高')}</option>
                </select>
              </label>
              <label className="field">
                <span>{t('限速（KB/s，0 不限）')}</span>
                <input
                  type="number"
                  min={0}
                  value={speedLimitKb}
                  onChange={(e) => setSpeedLimitKb(Math.max(0, Number(e.target.value)))}
                />
              </label>
              <label className="field">
                <span>{t('定时开始（可选）')}</span>
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
                  <span>{t('校验算法（可选）')}</span>
                  <select value={checksumAlgo} onChange={(e) => setChecksumAlgo(e.target.value)}>
                    <option value="">{t('不校验')}</option>
                    <option value="md5">MD5</option>
                    <option value="sha1">SHA1</option>
                    <option value="sha256">SHA256</option>
                  </select>
                </label>
                <label className="field">
                  <span>{t('期望校验值（可选）')}</span>
                  <input
                    type="text"
                    value={checksumExpected}
                    placeholder={t('留空则仅计算摘要')}
                    onChange={(e) => setChecksumExpected(e.target.value)}
                  />
                </label>
              </div>
            )}
            <div className="field-grid">
              <label className="field">
                <span>{t('Cookie（可选）')}</span>
                <input
                  type="text"
                  value={cookie}
                  spellCheck={false}
                  placeholder={t('从浏览器复制，如 sid=abc; token=def')}
                  onChange={(e) => setCookie(e.target.value)}
                />
              </label>
              <label className="field">
                <span>{t('Referer（可选）')}</span>
                <input
                  type="text"
                  value={referer}
                  spellCheck={false}
                  placeholder={t('防盗链站点常需，如 https://site.example/page')}
                  onChange={(e) => setReferer(e.target.value)}
                />
              </label>
            </div>
          </>
        )}
        </div>
        {error && <div className="dialog-error" style={{whiteSpace: 'pre-wrap'}}>{error}</div>}
        <div className="dialog-actions">
          <button className="btn ghost" onClick={onClose} disabled={busy}>
            {t('取消')}
          </button>
          <button className="btn primary" onClick={submit} disabled={busy}>
            {busy ? t('添加中…') : multi ? t('批量创建 {n} 个任务', {n: urls.length}) : t('开始下载')}
          </button>
        </div>
      </div>
    </div>
  );
}

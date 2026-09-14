import {useEffect, useState, type ReactNode} from 'react';

import type {DirCategory, Settings, ThemeMode} from '../types';
import {ACCENTS} from '../types';
import {api} from '../api';
import {MonitorIcon, MoonIcon, PencilIcon, PlusIcon, SunIcon, TrashIcon} from './icons';

interface Props {
  settings: Settings;
  theme: ThemeMode;
  accent: string;
  onTheme: (t: ThemeMode) => void;
  onAccent: (a: string) => void;
  onSaved: (s: Settings) => void;
  onBack: () => void;
}

function Switch({
  checked,
  onChange,
  label,
  hint,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
  hint?: string;
}) {
  return (
    <div className="switch-row">
      <div>
        <div className="switch-label">{label}</div>
        {hint && <div className="switch-hint">{hint}</div>}
      </div>
      <label className="switch">
        <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
        <span />
      </label>
    </div>
  );
}

const THEME_OPTIONS: {key: ThemeMode; label: string; icon: ReactNode}[] = [
  {key: 'light', label: '亮色', icon: <SunIcon size={14} />},
  {key: 'dark', label: '暗色', icon: <MoonIcon size={14} />},
  {key: 'system', label: '跟随系统', icon: <MonitorIcon size={14} />},
];

export default function SettingsPage({
  settings,
  theme,
  accent,
  onTheme,
  onAccent,
  onSaved,
  onBack,
}: Props) {
  const [form, setForm] = useState<Settings>({
    ...settings,
    closeAction: settings.closeAction ?? 'ask',
    dirCategories: settings.dirCategories ?? [],
    autoExtract: settings.autoExtract ?? false,
  });
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');
  // 应用内重命名弹窗（替代原生 prompt）
  const [renameTarget, setRenameTarget] = useState<{index: number; name: string} | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const set = (patch: Partial<Settings>) => setForm((f) => ({...f, ...patch}));

  const updateCategory = (i: number, patch: Partial<DirCategory>) => {
    setForm((f) => {
      const list = f.dirCategories.map((c, idx) => (idx === i ? {...c, ...patch} : c));
      return {...f, dirCategories: list};
    });
  };
  const removeCategory = (i: number) => {
    setForm((f) => ({...f, dirCategories: f.dirCategories.filter((_, idx) => idx !== i)}));
  };
  const addCategory = () => {
    setForm((f) => ({
      ...f,
      dirCategories: [...(f.dirCategories ?? []), {name: '', path: f.saveDir}],
    }));
  };
  const openRename = (i: number) => {
    const c = form.dirCategories?.[i];
    if (!c) return;
    setRenameTarget({index: i, name: c.name});
    setRenameValue(c.name);
  };
  const confirmRename = () => {
    if (renameTarget == null) return;
    const name = renameValue.trim();
    if (name) updateCategory(renameTarget.index, {name});
    setRenameTarget(null);
  };

  const save = async () => {
    try {
      await api.saveSettings(form);
      setError('');
      onSaved(form);
      setSaved(true);
      setTimeout(() => setSaved(false), 1500);
    } catch (e) {
      setError(`保存失败：${e}`);
    }
  };

  const speedLimitKb = form.speedLimit > 0 ? Math.round(form.speedLimit / 1024) : 0;

  useEffect(() => {
    if (renameTarget == null) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setRenameTarget(null);
      if (e.key === 'Enter') confirmRename();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [renameTarget, renameValue]);

  return (
    <>
    <div className="settings">
      <h2>设置</h2>

      <h3>外观</h3>
      <div className="field">
        <span>主题</span>
        <div className="segmented">
          {THEME_OPTIONS.map((o) => (
            <button
              key={o.key}
              className={theme === o.key ? 'active' : ''}
              onClick={() => onTheme(o.key)}
            >
              {o.icon}
              <span>{o.label}</span>
            </button>
          ))}
        </div>
      </div>
      <div className="field">
        <span>强调色</span>
        <div className="swatches">
          {ACCENTS.map((a) => (
            <button
              key={a.key}
              className={`swatch ${accent === a.key ? 'active' : ''}`}
              style={{background: a.color}}
              title={a.label}
              onClick={() => onAccent(a.key)}
            />
          ))}
        </div>
      </div>

      <h3>下载</h3>
      <label className="field">
        <span>默认保存目录</span>
        <input
          type="text"
          value={form.saveDir}
          onChange={(e) => set({saveDir: e.target.value})}
        />
      </label>
      <div className="field">
        <span>下载目录分类</span>
        <div className="dir-cats">
          {(form.dirCategories ?? []).map((c, i) => (
            <div className="dir-cat-row" key={i}>
              <div className="dir-cat-info">
                <input
                  type="text"
                  className="dir-cat-name"
                  value={c.name}
                  placeholder="分类名称，如：音乐"
                  onChange={(e) => updateCategory(i, {name: e.target.value})}
                />
                <input
                  type="text"
                  className="dir-cat-path"
                  value={c.path}
                  placeholder="保存路径"
                  onChange={(e) => updateCategory(i, {path: e.target.value})}
                />
              </div>
              <div className="dir-cat-actions">
                <button
                  type="button"
                  className="btn icon ghost"
                  title="重命名分类"
                  onClick={() => openRename(i)}
                >
                  <PencilIcon size={14} />
                </button>
                <button
                  type="button"
                  className="btn icon ghost danger"
                  title="删除分类"
                  onClick={() => removeCategory(i)}
                >
                  <TrashIcon size={14} />
                </button>
              </div>
            </div>
          ))}
          <button type="button" className="btn ghost small cat-add" onClick={addCategory}>
            <PlusIcon size={14} />
            添加
          </button>
        </div>
      </div>
      <div className="field-grid">
        <label className="field">
          <span>每任务连接数（1–128）</span>
          <input
            type="number"
            min={1}
            max={128}
            value={form.connections}
            onChange={(e) => set({connections: Number(e.target.value)})}
          />
        </label>
        <label className="field">
          <span>同时下载任务数（1–10）</span>
          <input
            type="number"
            min={1}
            max={10}
            value={form.concurrentTasks}
            onChange={(e) => set({concurrentTasks: Number(e.target.value)})}
          />
        </label>
        <label className="field">
          <span>限速（KB/s，0 不限速）</span>
          <input
            type="number"
            min={0}
            value={speedLimitKb}
            onChange={(e) => set({speedLimit: Math.max(0, Number(e.target.value)) * 1024})}
          />
        </label>
      </div>

      <h3>网络</h3>
      <label className="field">
        <span>User-Agent（留空使用内置默认）</span>
        <input
          type="text"
          value={form.userAgent}
          placeholder="Mozilla/5.0 …"
          onChange={(e) => set({userAgent: e.target.value})}
        />
      </label>
      <label className="field">
        <span>自定义请求头（每行一条 Key: Value）</span>
        <textarea
          rows={3}
          value={form.extraHeaders}
          placeholder={'Referer: https://example.com\nCookie: a=1'}
          onChange={(e) => set({extraHeaders: e.target.value})}
        />
      </label>
      <div className="field-grid">
        <label className="field">
          <span>代理</span>
          <select
            value={form.proxyMode}
            onChange={(e) => set({proxyMode: e.target.value as Settings['proxyMode']})}
          >
            <option value="none">不使用</option>
            <option value="system">系统代理</option>
            <option value="custom">自定义</option>
          </select>
        </label>
        <label className="field">
          <span>代理地址（http/socks5）</span>
          <input
            type="text"
            value={form.proxyUrl}
            placeholder="http://127.0.0.1:7890"
            disabled={form.proxyMode !== 'custom'}
            onChange={(e) => set({proxyUrl: e.target.value})}
          />
        </label>
      </div>
      <Switch
        checked={form.githubMirror}
        onChange={(v) => set({githubMirror: v})}
        label="GitHub 镜像加速"
        hint="下载 github.com 相关链接时自动改写为镜像地址"
      />
      {form.githubMirror && (
        <label className="field">
          <span>镜像模板（{'{url}'} 为原始链接）</span>
          <input
            type="text"
            value={form.mirrorTemplate}
            onChange={(e) => set({mirrorTemplate: e.target.value})}
          />
        </label>
      )}

      <h3>压缩包</h3>
      <div className="archive-panel">
        <Switch
          checked={form.autoExtract}
          onChange={(v) => set({autoExtract: v})}
          label="自动解压压缩包"
          hint="下载完成后解压到同目录下与压缩包同名的文件夹（zip / tar / tar.gz 等）"
        />
        <div className="archive-state">{form.autoExtract ? '开启' : '关闭'}</div>
      </div>

      <h3>窗口</h3>
      <div className="field">
        <span>点击关闭按钮时</span>
        <div className="segmented">
          {(
            [
              {key: 'ask', label: '每次询问'},
              {key: 'exit', label: '直接退出'},
              {key: 'minimize', label: '最小化到托盘'},
            ] as const
          ).map((o) => (
            <button
              key={o.key}
              type="button"
              className={(form.closeAction ?? 'ask') === o.key ? 'active' : ''}
              onClick={() => set({closeAction: o.key})}
            >
              <span>{o.label}</span>
            </button>
          ))}
        </div>
      </div>

      <h3>集成</h3>
      <Switch
        checked={form.clipboardWatch}
        onChange={(v) => set({clipboardWatch: v})}
        label="剪贴板监听"
        hint="复制下载链接时自动弹出新建下载"
      />
      <Switch
        checked={form.apiEnabled}
        onChange={(v) => set({apiEnabled: v})}
        label="本地 REST API"
        hint="仅供本机访问的自动化接口"
      />
      {form.apiEnabled && (
        <label className="field">
          <span>API 端口（修改后重启应用生效）</span>
          <input
            type="number"
            min={1}
            max={65535}
            value={form.apiPort}
            onChange={(e) => set({apiPort: Number(e.target.value)})}
          />
        </label>
      )}

      {error && <div className="dialog-error">{error}</div>}
      <div className="dialog-actions">
        <button className="btn ghost" onClick={onBack}>
          返回
        </button>
        <button className="btn primary" onClick={save}>
          {saved ? '已保存 ✓' : '保存'}
        </button>
      </div>
    </div>

    {renameTarget != null && (
      <div
        className="overlay"
        onMouseDown={(e) => e.target === e.currentTarget && setRenameTarget(null)}
      >
        <div className="dialog dialog-compact">
          <h2>重命名分类</h2>
          <label className="field">
            <span>分类名称</span>
            <input
              autoFocus
              type="text"
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') confirmRename();
              }}
            />
          </label>
          <div className="dialog-actions">
            <button className="btn ghost" onClick={() => setRenameTarget(null)}>
              取消
            </button>
            <button className="btn primary" onClick={confirmRename}>
              确定
            </button>
          </div>
        </div>
      </div>
    )}
    </>
  );
}

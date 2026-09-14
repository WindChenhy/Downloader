import {useState, type ReactNode} from 'react';

import type {Settings, ThemeMode} from '../types';
import {ACCENTS} from '../types';
import {api} from '../api';
import {MonitorIcon, MoonIcon, SunIcon} from './icons';

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
  });
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');
  const set = (patch: Partial<Settings>) => setForm((f) => ({...f, ...patch}));

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

  return (
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
  );
}

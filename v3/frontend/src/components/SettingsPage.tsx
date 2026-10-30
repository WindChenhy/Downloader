import {useEffect, useState, type ReactNode} from 'react';

import type {DirCategory, Settings, ThemeMode} from '../types';
import {ACCENTS} from '../types';
import type {LangPref} from '../i18n';
import {resolveLang, t, useI18n} from '../i18n';
import {api} from '../api';
import {MonitorIcon, MoonIcon, PencilIcon, PlusIcon, SunIcon, TrashIcon} from './icons';

interface Props {
  settings: Settings;
  theme: ThemeMode;
  accent: string;
  langPref: LangPref;
  onLangPref: (l: LangPref) => void;
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
  langPref,
  onLangPref,
  onTheme,
  onAccent,
  onSaved,
  onBack,
}: Props) {
  const {t} = useI18n();
  const [form, setForm] = useState<Settings>({
    ...settings,
    closeAction: settings.closeAction ?? 'ask',
    minimizeToTray: settings.minimizeToTray ?? false,
    dirCategories: settings.dirCategories ?? [],
    autoExtract: settings.autoExtract ?? false,
    afterComplete: settings.afterComplete ?? 'none',
    queueSuspended: settings.queueSuspended ?? false,
    windowEnabled: settings.windowEnabled ?? false,
    windowStart: settings.windowStart ?? '00:00',
    windowEnd: settings.windowEnd ?? '00:00',
    diskGuardOff: settings.diskGuardOff ?? false,
    diskMinFree: settings.diskMinFree ?? 0,
  });
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');
  // 导出任务包时是否带出每任务的 Cookie/Referer——常含会话凭据，默认不带
  const [exportAuth, setExportAuth] = useState(false);
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
      await api.saveSettings({...form, language: resolveLang(langPref)});
      setError('');
      onSaved(form);
      setSaved(true);
      setTimeout(() => setSaved(false), 1500);
    } catch (e) {
      setError(t('保存失败：{e}', {e: String(e)}));
    }
  };

  const speedLimitKb = form.speedLimit > 0 ? Math.round(form.speedLimit / 1024) : 0;
  const diskMinFreeMb = (form.diskMinFree ?? 0) > 0 ? Math.round((form.diskMinFree ?? 0) / (1024 * 1024)) : 0;

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
      <h2>{t('设置')}</h2>

      <h3>{t('外观')}</h3>
      <div className="field">
        <span>{t('主题')}</span>
        <div className="segmented">
          {THEME_OPTIONS.map((o) => (
            <button
              key={o.key}
              className={theme === o.key ? 'active' : ''}
              onClick={() => onTheme(o.key)}
            >
              {o.icon}
              <span>{t(o.label)}</span>
            </button>
          ))}
        </div>
      </div>
      <div className="field">
        <span>{t('强调色')}</span>
        <div className="swatches">
          {ACCENTS.map((a) => (
            <button
              key={a.key}
              className={`swatch ${accent === a.key ? 'active' : ''}`}
              style={{background: a.color}}
              title={t(a.label)}
              onClick={() => onAccent(a.key)}
            />
          ))}
        </div>
      </div>

      <div className="field">
        <span>{t('语言')}</span>
        <div className="segmented">
          {(['system', 'zh-CN', 'en-US'] as const).map((l) => (
            <button
              key={l}
              className={langPref === l ? 'active' : ''}
              onClick={() => onLangPref(l)}
            >
              <span>{l === 'system' ? t('跟随系统') : l === 'zh-CN' ? '简体中文' : 'English'}</span>
            </button>
          ))}
        </div>
      </div>

      <h3>{t('下载')}</h3>
      <label className="field">
        <span>{t('默认保存目录')}</span>
        <input
          type="text"
          value={form.saveDir}
          onChange={(e) => set({saveDir: e.target.value})}
        />
      </label>
      <div className="field">
        <span>{t('下载目录分类')}</span>
        <div className="dir-cats">
          {(form.dirCategories ?? []).map((c, i) => (
            <div className="dir-cat-row" key={i}>
              <div className="dir-cat-info">
                <input
                  type="text"
                  className="dir-cat-name"
                  value={c.name}
                  placeholder={t('分类名称，如：音乐')}
                  onChange={(e) => updateCategory(i, {name: e.target.value})}
                />
                <input
                  type="text"
                  className="dir-cat-path"
                  value={c.path}
                  placeholder={t('保存路径')}
                  onChange={(e) => updateCategory(i, {path: e.target.value})}
                />
              </div>
              <div className="dir-cat-actions">
                <button
                  type="button"
                  className="btn icon ghost"
                  title={t('重命名分类')}
                  onClick={() => openRename(i)}
                >
                  <PencilIcon size={14} />
                </button>
                <button
                  type="button"
                  className="btn icon ghost danger"
                  title={t('删除分类')}
                  onClick={() => removeCategory(i)}
                >
                  <TrashIcon size={14} />
                </button>
              </div>
            </div>
          ))}
          <button type="button" className="btn ghost small cat-add" onClick={addCategory}>
            <PlusIcon size={14} />
            {t('添加')}
          </button>
        </div>
      </div>
      <div className="field-grid">
        <label className="field">
          <span>{t('每任务连接数（1–128）')}</span>
          <input
            type="number"
            min={1}
            max={128}
            value={form.connections}
            onChange={(e) => set({connections: Number(e.target.value)})}
          />
        </label>
        <label className="field">
          <span>{t('同时下载任务数（1–10）')}</span>
          <input
            type="number"
            min={1}
            max={10}
            value={form.concurrentTasks}
            onChange={(e) => set({concurrentTasks: Number(e.target.value)})}
          />
        </label>
        <label className="field">
          <span>{t('限速（KB/s，0 不限速）')}</span>
          <input
            type="number"
            min={0}
            value={speedLimitKb}
            onChange={(e) => set({speedLimit: Math.max(0, Number(e.target.value)) * 1024})}
          />
        </label>
      </div>

      <h3>{t('网络')}</h3>
      <label className="field">
        <span>{t('User-Agent（留空使用内置默认）')}</span>
        <input
          type="text"
          value={form.userAgent}
          placeholder="Mozilla/5.0 …"
          onChange={(e) => set({userAgent: e.target.value})}
        />
      </label>
      <label className="field">
        <span>{t('自定义请求头（每行一条 Key: Value）')}</span>
        <textarea
          rows={3}
          value={form.extraHeaders}
          placeholder={'Referer: https://example.com\nCookie: a=1'}
          onChange={(e) => set({extraHeaders: e.target.value})}
        />
      </label>
      <div className="field-grid">
        <label className="field">
          <span>{t('代理')}</span>
          <select
            value={form.proxyMode}
            onChange={(e) => set({proxyMode: e.target.value as Settings['proxyMode']})}
          >
            <option value="none">{t('不使用')}</option>
            <option value="system">{t('系统代理')}</option>
            <option value="custom">{t('自定义')}</option>
          </select>
        </label>
        <label className="field">
          <span>{t('代理地址（http/socks5）')}</span>
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
        label={t('GitHub 镜像加速')}
        hint={t('下载 github.com 相关链接时自动改写为镜像地址')}
      />
      {form.githubMirror && (
        <label className="field">
          <span>{t('镜像模板（{url} 为原始链接）')}</span>
          <input
            type="text"
            value={form.mirrorTemplate}
            onChange={(e) => set({mirrorTemplate: e.target.value})}
          />
        </label>
      )}

      <h3>{t('压缩包')}</h3>
      <div className="archive-panel">
        <Switch
          checked={form.autoExtract}
          onChange={(v) => set({autoExtract: v})}
          label={t('自动解压压缩包')}
          hint={t('下载完成后解压到同目录下与压缩包同名的文件夹（zip / tar / tar.gz 等）')}
        />
        <div className="archive-state">{form.autoExtract ? t('已开启') : t('已关闭')}</div>
      </div>

      <h3>{t('通知')}</h3>
      <Switch
        checked={form.notifyOnComplete}
        onChange={(v) => set({notifyOnComplete: v})}
        label={t('下载完成通知')}
        hint={t('默认开启')}
      />
      <Switch
        checked={form.notifyOnFail}
        onChange={(v) => set({notifyOnFail: v})}
        label={t('下载失败通知')}
        hint={t('默认开启')}
      />
      <Switch
        checked={form.notifyOnCreate}
        onChange={(v) => set({notifyOnCreate: v})}
        label={t('创建任务通知')}
        hint={t('默认关闭，避免批量添加时打扰')}
      />
      <Switch
        checked={form.notifyOnPause}
        onChange={(v) => set({notifyOnPause: v})}
        label={t('暂停任务通知')}
        hint={t('默认关闭')}
      />

      <h3>{t('全部完成动作')}</h3>
      <label className="field">
        <span>{t('当所有任务下载结束后')}</span>
        <select
          value={form.afterComplete ?? 'none'}
          onChange={(e) => set({afterComplete: e.target.value as Settings['afterComplete']})}
        >
          <option value="none">{t('不执行')}</option>
          <option value="open_dir">{t('打开下载目录')}</option>
          <option value="shutdown">{t('关机（60 秒后）')}</option>
          <option value="sleep">{t('睡眠')}</option>
          <option value="exit_app">{t('退出本程序')}</option>
        </select>
      </label>

      <h3>{t('队列')}</h3>
      <Switch
        checked={!!form.queueSuspended}
        onChange={async (v) => {
          // 闸门即时生效，不等「保存」按钮：它是运行期开关而非常规设置
          try {
            await api.setQueueSuspended(v);
            const next = {...form, queueSuspended: v};
            setForm(next);
            onSaved(next);
            setError('');
          } catch (e) {
            setError(t('保存失败：{e}', {e: String(e)}));
          }
        }}
        label={t('暂停队列')}
        hint={t('闭闸后不再启动任何任务，正在下载的不受影响；重启后保持')}
      />
      <Switch
        checked={!!form.windowEnabled}
        onChange={(v) => set({windowEnabled: v})}
        label={t('带宽时间窗')}
        hint={t('只在指定时段内启动新任务；正在下载的任务不会被中断，窗外等于自动闭闸')}
      />
      {form.windowEnabled && (
        <>
          <div className="field-grid">
            <label className="field">
              <span>{t('带宽时间窗开始')}</span>
              <input
                type="time"
                value={form.windowStart ?? '00:00'}
                onChange={(e) => set({windowStart: e.target.value})}
              />
            </label>
            <label className="field">
              <span>{t('带宽时间窗结束')}</span>
              <input
                type="time"
                value={form.windowEnd ?? '00:00'}
                onChange={(e) => set({windowEnd: e.target.value})}
              />
            </label>
          </div>
          <div className="settings-note">
            {form.windowStart === form.windowEnd
              ? t('起止时间相同：等于全天放行')
              : form.windowStart && form.windowEnd && form.windowStart > form.windowEnd
                ? t('结束时间早于开始时间：视为跨零点（如 22:00–06:00）')
                : t('到点自动放行排队任务，无需手动开闸')}
          </div>
        </>
      )}

      <h3>{t('磁盘')}</h3>
      <Switch
        checked={!form.diskGuardOff}
        onChange={(v) => set({diskGuardOff: !v})}
        label={t('下载前检查磁盘空间')}
        hint={t('装不下时任务停在队列里并说明原因，腾出空间后自动继续下载')}
      />
      {!form.diskGuardOff && (
        <div className="field-grid">
          <label className="field">
            <span>{t('额外保留空间（MB，0 不留）')}</span>
            <input
              type="number"
              min={0}
              value={diskMinFreeMb}
              onChange={(e) => set({diskMinFree: Math.max(0, Number(e.target.value)) * 1024 * 1024})}
            />
          </label>
        </div>
      )}

      <h3>{t('任务列表')}</h3>
      <Switch
        checked={exportAuth}
        onChange={setExportAuth}
        label={t('导出时包含 Cookie / Referer')}
        hint={t('这两项常含会话凭据，默认剥离；仅在自己迁移任务包时勾选')}
      />
      <div className="field-grid">
        <button
          type="button"
          className="btn ghost"
          onClick={async () => {
            try {
              const json = await api.exportTasksJson(exportAuth);
              const blob = new Blob([json], {type: 'application/json'});
              const a = document.createElement('a');
              a.href = URL.createObjectURL(blob);
              a.download = `downloader-tasks-${Date.now()}.json`;
              a.click();
              URL.revokeObjectURL(a.href);
              setError('');
            } catch (e) {
              setError(t('导出失败：{e}', {e: String(e)}));
            }
          }}
        >
          {t('导出任务列表')}
        </button>
        <label className="btn ghost" style={{cursor: 'pointer', textAlign: 'center'}}>
          {t('导入任务列表')}
          <input
            type="file"
            accept="application/json,.json"
            style={{display: 'none'}}
            onChange={async (e) => {
              const file = e.target.files?.[0];
              if (!file) return;
              try {
                const text = await file.text();
                const res = await api.importTasksJson(text);
                const ok = res.tasks?.length ?? 0;
                const bad = res.errors?.length ?? 0;
                setError(
                  bad
                    ? t('导入完成：新建 {ok}，跳过/失败 {bad}', {ok, bad}) + '\n' + (res.errors || []).join('\n')
                    : t('已导入 {n} 个任务', {n: ok}),
                );
              } catch (err) {
                setError(t('导入失败：{e}', {e: String(err)}));
              }
              e.target.value = '';
            }}
          />
        </label>
      </div>

      <h3>{t('窗口')}</h3>
      <div className="field">
        <span>{t('点击关闭按钮时')}</span>
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
              <span>{t(o.label)}</span>
            </button>
          ))}
        </div>
      </div>
      <Switch
        checked={form.minimizeToTray ?? false}
        onChange={(v) => set({minimizeToTray: v})}
        label={t('最小化时隐藏到托盘')}
        hint={t('点击最小化按钮时隐藏到系统托盘，不占任务栏；从托盘图标唤回')}
      />

      <h3>{t('集成')}</h3>
      <Switch
        checked={form.clipboardWatch}
        onChange={(v) => set({clipboardWatch: v})}
        label={t('剪贴板监听')}
        hint={t('复制下载链接时自动弹出新建下载')}
      />
      <Switch
        checked={form.apiEnabled}
        onChange={(v) => set({apiEnabled: v})}
        label={t('本地 REST API')}
        hint={t('仅供本机访问的自动化接口')}
      />
      {form.apiEnabled && (
        <>
          <label className="field">
            <span>{t('API 端口（保存后立即生效）')}</span>
            <input
              type="number"
              min={1}
              max={65535}
              value={form.apiPort}
              onChange={(e) => set({apiPort: Number(e.target.value)})}
            />
          </label>
          <label className="field">
            <span>{t('API Token（请求须携带 X-API-Token 或 Authorization: Bearer 头）')}</span>
            <div className="token-row">
              <input type="text" readOnly value={form.apiToken || ''} onFocus={(e) => e.target.select()} />
              <button
                type="button"
                className="btn"
                onClick={() => navigator.clipboard?.writeText(form.apiToken || '').catch(() => {})}
              >
                {t('复制')}
              </button>
            </div>
          </label>
          <label className="field">
            <span>{t('API / 导入允许的保存目录白名单（每行一个，留空 = 仅默认下载目录）')}</span>
            <textarea
              rows={3}
              value={(form.allowSaveDirs ?? []).join('\n')}
              onChange={(e) =>
                set({allowSaveDirs: e.target.value.split('\n').map((s) => s.trim()).filter(Boolean)})
              }
            />
          </label>
        </>
      )}

      {error && <div className="dialog-error">{error}</div>}
      <div className="dialog-actions">
        <button className="btn ghost" onClick={onBack}>
          {t('返回')}
        </button>
        <button className="btn primary" onClick={save}>
          {saved ? t('已保存 ✓') : t('保存')}
        </button>
      </div>
    </div>

    {renameTarget != null && (
      <div
        className="overlay"
        onMouseDown={(e) => e.target === e.currentTarget && setRenameTarget(null)}
      >
        <div className="dialog dialog-compact">
          <h2>{t('重命名分类')}</h2>
          <label className="field">
            <span>{t('分类名称')}</span>
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
              {t('取消')}
            </button>
            <button className="btn primary" onClick={confirmRename}>
              {t('保存')}
            </button>
          </div>
        </div>
      </div>
    )}
    </>
  );
}

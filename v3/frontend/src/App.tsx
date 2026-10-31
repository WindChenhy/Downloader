import {useCallback, useEffect, useState} from 'react';

import {Events} from '@wailsio/runtime';

import type {Settings, Task, ThemeMode} from './types';
import {resolveLang, t, useI18n} from './i18n';
import {api} from './api';
import TaskList from './components/TaskList';
import AddTaskDialog from './components/AddTaskDialog';
import SettingsPage from './components/SettingsPage';
import StatsPage from './components/StatsPage';
import CloseConfirmDialog from './components/CloseConfirmDialog';
import {handleAppBack, registerBackHandler} from './lib/backHandler';
import {ChartIcon, GearIcon, MonitorIcon, MoonIcon, PlusIcon, SunIcon} from './components/icons';

function loadTheme(): ThemeMode {
  const v = localStorage.getItem('theme');
  return v === 'light' || v === 'dark' || v === 'system' ? v : 'system';
}

export default function App() {
  const {t, langPref, setLang} = useI18n();
  const [tasks, setTasks] = useState<Task[]>([]);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [view, setView] = useState<'list' | 'settings' | 'stats'>('list');
  const [showDialog, setShowDialog] = useState(false);
  const [showCloseConfirm, setShowCloseConfirm] = useState(false);
  const [prefillUrl, setPrefillUrl] = useState('');
  const [theme, setTheme] = useState<ThemeMode>(loadTheme);
  const [accent, setAccent] = useState(() => localStorage.getItem('accent') ?? 'blue');

  // Android 返回键/侧滑：Java 侧拦截后经 window.__wailsBack 询问前端是否消费——
  // 栈里有可关闭界面（弹窗/设置/统计视图）就关最上层；已在首页则交回系统退到后台
  //（moveTaskToBack，进程与下载保活），而不是结束应用。
  useEffect(() => {
    (window as unknown as {__wailsBack: () => boolean}).__wailsBack = () => handleAppBack();
  }, []);

  // 设置/统计视图：返回先回列表页
  useEffect(() => {
    if (view === 'list') return;
    return registerBackHandler(() => {
      setView('list');
      return true;
    });
  }, [view]);

  // 新建下载弹窗
  useEffect(() => {
    if (!showDialog) return;
    return registerBackHandler(() => {
      setShowDialog(false);
      setPrefillUrl('');
      return true;
    });
  }, [showDialog]);

  // 关闭确认弹窗
  useEffect(() => {
    if (!showCloseConfirm) return;
    return registerBackHandler(() => {
      setShowCloseConfirm(false);
      return true;
    });
  }, [showCloseConfirm]);

  // 启动自愈：Android 上 ServiceStartup 与 WebView 就绪存在竞态，引擎未就绪时
  // 绑定调用会 reject——有限重试直到就绪，而不是把失败当空态定格
  const refresh = useCallback(() => {
    const attempt = (left: number) => {
      api
        .getTasks()
        .then(setTasks)
        .catch(() => {
          if (left > 0) window.setTimeout(() => attempt(left - 1), 400);
        });
    };
    attempt(25);
  }, []);

  const refreshSettings = useCallback(() => {
    const attempt = (left: number) => {
      api
        .getSettings()
        .then(setSettings)
        .catch(() => {
          if (left > 0) window.setTimeout(() => attempt(left - 1), 400);
        });
    };
    attempt(25);
  }, []);

  // 主题三态：light / dark / 跟随系统（监听系统切换实时生效）
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const apply = () => {
      document.documentElement.dataset.theme = theme === 'system' ? (mq.matches ? 'dark' : 'light') : theme;
    };
    apply();
    mq.addEventListener('change', apply);
    localStorage.setItem('theme', theme);
    return () => mq.removeEventListener('change', apply);
  }, [theme]);

  useEffect(() => {
    document.documentElement.dataset.accent = accent;
    localStorage.setItem('accent', accent);
  }, [accent]);

  // 语言同步：把解析后的具体语言写入引擎设置，供系统通知与托盘菜单使用
  useEffect(() => {
    if (!settings) return;
    const resolved = resolveLang(langPref);
    if ((settings.language ?? 'zh-CN') !== resolved) {
      api.saveSettings({...settings, language: resolved})
        .then(() => api.getSettings())
        .then(setSettings)
        .catch(() => {});
    }
  }, [settings, langPref]);

  useEffect(() => {
    refresh();
    api.getSettings().then(setSettings).catch(() => {});
    // 引擎每 500ms 推送一次任务快照；Events.On 返回退订函数，
    // 卸载时必须退订，否则 StrictMode 双挂载下监听器成对泄漏。
    // v3 事件回调签名变了：载荷在 ev.data 里，不再展开为回调参数
    const unsubscribers = [
      Events.On('tasks:changed', (ev) => {
        const data = ev.data as Task[];
        if (Array.isArray(data)) setTasks(data);
      }),
      // 剪贴板监听到新链接 → 弹出预填好的新建下载
      Events.On('clipboard:url', (ev) => {
        const url = ev.data as unknown;
        if (typeof url === 'string' && url) {
          setView('list');
          setPrefillUrl(url);
          setShowDialog(true);
        }
      }),
      // 后端点击关闭且设置为「每次询问」时弹出确认框
      Events.On('app:confirm-close', () => {
        setShowCloseConfirm(true);
      }),
    ];
    return () => unsubscribers.forEach((unsubscribe) => unsubscribe());
  }, [refresh]);

  const cycleTheme = () =>
    setTheme(theme === 'light' ? 'dark' : theme === 'dark' ? 'system' : 'light');

  return (
    <div className="app">
      <header className="titlebar">
        <div className="brand">
          <div className="brand-mark">⇣</div>
          <div className="brand-text">
            <span className="brand-name">Downloader</span>
            <span className="brand-sub">{t('多线程下载工具')}</span>
          </div>
        </div>
        <div className="titlebar-actions">
          <button
            className="btn icon ghost theme-btn"
            title={theme === 'light' ? t('浅色') : theme === 'dark' ? t('深色') : t('跟随系统')}
            onClick={cycleTheme}
          >
            {theme === 'light' ? <SunIcon size={16} /> : theme === 'dark' ? <MoonIcon size={16} /> : <MonitorIcon size={16} />}
          </button>
          <button
            className={`btn icon ghost ${view === 'stats' ? 'active' : ''}`}
            title={t('下载统计')}
            onClick={() => setView(view === 'stats' ? 'list' : 'stats')}
          >
            <ChartIcon size={16} />
          </button>
          <button
            className={`btn icon ghost ${view === 'settings' ? 'active' : ''}`}
            title={t('设置')}
            onClick={() => setView(view === 'settings' ? 'list' : 'settings')}
          >
            <GearIcon size={16} />
          </button>
          <button
            className="btn icon primary"
            title={t('新建下载')}
            onClick={() => { setPrefillUrl(''); setShowDialog(true); }}
          >
            <PlusIcon size={16} />
          </button>
        </div>
      </header>

      <main className="content">
        {view === 'settings' && settings ? (
          <SettingsPage
            settings={settings}
            theme={theme}
            accent={accent}
            langPref={langPref}
            onLangPref={setLang}
            onTheme={setTheme}
            onAccent={setAccent}
            onSaved={setSettings}
            onBack={() => setView('list')}
          />
        ) : view === 'stats' ? (
          <StatsPage onBack={() => setView('list')} />
        ) : (
          <TaskList tasks={tasks} onChanged={refresh} onSettingsChanged={refreshSettings} />
        )}
      </main>

      {showDialog && settings && (
        <AddTaskDialog
          settings={settings}
          initialUrl={prefillUrl}
          onClose={() => { setShowDialog(false); setPrefillUrl(''); }}
          onAdded={refresh}
        />
      )}

      {showCloseConfirm && (
        <CloseConfirmDialog
          onClose={() => {
            setShowCloseConfirm(false);
            // 若用户勾选了记住选择，刷新本地设置缓存
            api.getSettings().then(setSettings).catch(() => {});
          }}
        />
      )}
    </div>
  );
}

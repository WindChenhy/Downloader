import {useCallback, useEffect, useState} from 'react';

import {EventsOn} from '../wailsjs/runtime/runtime';

import type {Settings, Task, ThemeMode} from './types';
import {api} from './api';
import TaskList from './components/TaskList';
import AddTaskDialog from './components/AddTaskDialog';
import SettingsPage from './components/SettingsPage';
import {GearIcon, PlusIcon} from './components/icons';

function loadTheme(): ThemeMode {
  const v = localStorage.getItem('theme');
  return v === 'light' || v === 'dark' || v === 'system' ? v : 'system';
}

export default function App() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [view, setView] = useState<'list' | 'settings'>('list');
  const [showDialog, setShowDialog] = useState(false);
  const [prefillUrl, setPrefillUrl] = useState('');
  const [theme, setTheme] = useState<ThemeMode>(loadTheme);
  const [accent, setAccent] = useState(() => localStorage.getItem('accent') ?? 'blue');

  const refresh = useCallback(() => {
    api.getTasks().then(setTasks).catch(() => {});
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

  useEffect(() => {
    refresh();
    api.getSettings().then(setSettings).catch(() => {});
    // 引擎每 500ms 推送一次任务快照
    EventsOn('tasks:changed', (data: Task[]) => {
      if (Array.isArray(data)) setTasks(data);
    });
    // 剪贴板监听到新链接 → 弹出预填好的新建下载
    EventsOn('clipboard:url', (url: string) => {
      if (typeof url === 'string' && url) {
        setView('list');
        setPrefillUrl(url);
        setShowDialog(true);
      }
    });
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
            <span className="brand-sub">多线程下载工具</span>
          </div>
        </div>
        <div className="titlebar-actions">
          <button className="btn ghost" title="切换主题" onClick={cycleTheme}>
            {theme === 'light' ? '☀' : theme === 'dark' ? '🌙' : '◐'}
          </button>
          <button
            className={`btn icon ghost ${view === 'settings' ? 'active' : ''}`}
            title="设置"
            onClick={() => setView(view === 'settings' ? 'list' : 'settings')}
          >
            <GearIcon size={16} />
          </button>
          <button
            className="btn icon primary"
            title="新建下载"
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
            onTheme={setTheme}
            onAccent={setAccent}
            onSaved={setSettings}
            onBack={() => setView('list')}
          />
        ) : (
          <TaskList tasks={tasks} onChanged={refresh} />
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
    </div>
  );
}

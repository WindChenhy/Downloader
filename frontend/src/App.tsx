import {useCallback, useEffect, useState} from 'react';

import {EventsOn} from '../wailsjs/runtime/runtime';

import type {Settings, Task} from './types';
import {api} from './api';
import TaskList from './components/TaskList';
import AddTaskDialog from './components/AddTaskDialog';
import SettingsPage from './components/SettingsPage';

export default function App() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [view, setView] = useState<'list' | 'settings'>('list');
  const [showDialog, setShowDialog] = useState(false);

  const refresh = useCallback(() => {
    api.getTasks().then(setTasks).catch(() => {});
  }, []);

  useEffect(() => {
    refresh();
    api.getSettings().then(setSettings).catch(() => {});
    // 引擎每 500ms 推送一次任务快照
    EventsOn('tasks:changed', (data: Task[]) => {
      if (Array.isArray(data)) setTasks(data);
    });
  }, [refresh]);

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
          <button
            className={`btn ghost ${view === 'settings' ? 'active' : ''}`}
            onClick={() => setView(view === 'settings' ? 'list' : 'settings')}
          >
            设置
          </button>
          <button className="btn primary" onClick={() => setShowDialog(true)}>
            ＋ 新建下载
          </button>
        </div>
      </header>

      <main className="content">
        {view === 'settings' && settings ? (
          <SettingsPage
            settings={settings}
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
          onClose={() => setShowDialog(false)}
          onAdded={refresh}
        />
      )}
    </div>
  );
}

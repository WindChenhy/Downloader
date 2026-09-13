import {useState} from 'react';

import type {Settings} from '../types';
import {api} from '../api';

interface Props {
  settings: Settings;
  onSaved: (s: Settings) => void;
  onBack: () => void;
}

export default function SettingsPage({settings, onSaved, onBack}: Props) {
  const [saveDir, setSaveDir] = useState(settings.saveDir);
  const [connections, setConnections] = useState(settings.connections);
  const [concurrentTasks, setConcurrentTasks] = useState(settings.concurrentTasks);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');

  const save = async () => {
    const next: Settings = {
      saveDir: saveDir.trim(),
      connections,
      concurrentTasks,
    };
    try {
      await api.saveSettings(next);
      setError('');
      onSaved(next);
      setSaved(true);
      setTimeout(() => setSaved(false), 1500);
    } catch (e) {
      setError(`保存失败：${e}`);
    }
  };

  return (
    <div className="settings">
      <h2>设置</h2>
      <label className="field">
        <span>默认保存目录</span>
        <input type="text" value={saveDir} onChange={(e) => setSaveDir(e.target.value)} />
      </label>
      <label className="field">
        <span>每任务连接数（1–32）</span>
        <input
          type="number"
          min={1}
          max={32}
          value={connections}
          onChange={(e) => setConnections(Number(e.target.value))}
        />
      </label>
      <label className="field">
        <span>同时下载任务数（1–10）</span>
        <input
          type="number"
          min={1}
          max={10}
          value={concurrentTasks}
          onChange={(e) => setConcurrentTasks(Number(e.target.value))}
        />
      </label>
      <div className="settings-note">
        关闭窗口时会最小化到系统托盘，任务进度自动保存，重启后可继续未完成的下载。
      </div>
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

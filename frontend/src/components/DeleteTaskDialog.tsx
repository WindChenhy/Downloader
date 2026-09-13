import {useState} from 'react';

import type {Task} from '../types';
import {formatBytes} from '../lib/format';
import {api} from '../api';

interface Props {
  task: Task;
  onClose: () => void;
  onDeleted: () => void;
}

// 删除确认对话框：让用户选择只删记录还是连文件一起删除。
export default function DeleteTaskDialog({task, onClose, onDeleted}: Props) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const remove = async (deleteFiles: boolean) => {
    setBusy(true);
    try {
      await api.removeTask(task.id, deleteFiles);
      onDeleted();
      onClose();
    } catch (e) {
      setError(`删除失败：${e}`);
      setBusy(false);
    }
  };

  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog dialog-compact">
        <h2>删除任务</h2>
        <p className="delete-target">
          「{task.fileName}」{task.status === 'completed' && ' 已下载完成'}
        </p>
        {task.status === 'completed' && task.totalSize > 0 && (
          <p className="delete-note">
            文件（{formatBytes(task.totalSize)}）保存在 {task.saveDir}
          </p>
        )}
        {task.status !== 'completed' && (
          <p className="delete-note">任务尚未完成，未下载的部分数据将被清理。</p>
        )}
        {error && <div className="dialog-error">{error}</div>}
        <div className="dialog-actions three">
          <button className="btn ghost" onClick={onClose} disabled={busy}>
            取消
          </button>
          <button className="btn ghost" onClick={() => remove(false)} disabled={busy}>
            仅删除记录
          </button>
          <button className="btn confirm-delete" onClick={() => remove(true)} disabled={busy}>
            连文件一起删除
          </button>
        </div>
      </div>
    </div>
  );
}

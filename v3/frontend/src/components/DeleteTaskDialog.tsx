import {useState} from 'react';

import {t, useI18n} from '../i18n';

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
  const {t} = useI18n();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const remove = async (deleteFiles: boolean) => {
    setBusy(true);
    try {
      await api.removeTask(task.id, deleteFiles);
      onDeleted();
      onClose();
    } catch (e) {
      setError(t('删除失败：{e}', {e: String(e)}));
      setBusy(false);
    }
  };

  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog dialog-compact">
        <h2>{t('删除任务')}</h2>
        <p className="delete-target">
          {t('「{name}」', {name: task.fileName})}
          {task.status === 'completed' && ` ${t('已下载完成')}`}
        </p>
        {task.status === 'completed' && task.totalSize > 0 && (
          <p className="delete-note">
            {t('文件（{size}）保存在 {dir}', {size: formatBytes(task.totalSize), dir: task.saveDir})}
          </p>
        )}
        {task.status !== 'completed' && (
          <p className="delete-note">{t('任务尚未完成，未下载的部分数据将被清理。')}</p>
        )}
        {error && <div className="dialog-error">{error}</div>}
        <div className="dialog-actions three">
          <button className="btn ghost" onClick={onClose} disabled={busy}>
            {t('取消')}
          </button>
          <button className="btn ghost" onClick={() => remove(false)} disabled={busy}>
            {t('仅删除记录')}
          </button>
          <button className="btn confirm-delete" onClick={() => remove(true)} disabled={busy}>
            {t('连文件一起删除')}
          </button>
        </div>
      </div>
    </div>
  );
}

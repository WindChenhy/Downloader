import {useEffect, useState} from 'react';

import {api} from '../api';

interface Props {
  onClose: () => void;
}

// 点击窗口关闭按钮时的确认弹窗。
// 默认主操作是「直接退出」；勾选「记住我的选择」后写入设置，之后不再询问。
export default function CloseConfirmDialog({onClose}: Props) {
  const [remember, setRemember] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !busy) onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose, busy]);

  const choose = async (action: 'exit' | 'minimize') => {
    setBusy(true);
    try {
      await api.applyCloseAction(action, remember);
      // exit 时进程即将退出；minimize 时窗口已隐藏
      onClose();
    } catch {
      setBusy(false);
    }
  };

  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div className="dialog dialog-compact close-dialog">
        <h2>关闭窗口</h2>
        <p className="close-dialog-text">
          要将 Downloader 最小化到系统托盘，还是直接退出程序？
        </p>
        <p className="delete-note">最小化后仍可继续下载，并从托盘图标重新打开窗口。</p>
        <label className="checkbox-row">
          <input
            type="checkbox"
            checked={remember}
            onChange={(e) => setRemember(e.target.checked)}
          />
          <span>记住我的选择，不再询问</span>
        </label>
        <div className="dialog-actions three">
          <button className="btn ghost" onClick={onClose} disabled={busy}>
            取消
          </button>
          <button className="btn ghost" onClick={() => choose('minimize')} disabled={busy}>
            最小化到托盘
          </button>
          <button className="btn primary" onClick={() => choose('exit')} disabled={busy}>
            直接退出
          </button>
        </div>
      </div>
    </div>
  );
}

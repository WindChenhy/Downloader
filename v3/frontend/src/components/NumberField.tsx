import {useEffect, useState} from 'react';

interface Props {
  value: number;
  min: number;
  max: number;
  onChange: (v: number) => void;
  placeholder?: string;
  disabled?: boolean;
}

/**
 * 数字输入框（替代 type="number" 的裸写法）：
 * - inputMode 触发数字键盘，非数字字符直接丢弃
 * - 前导零即时清除（输入 01111 → 显示 1111）
 * - 允许清空以便重输（无粘滞 0）；失焦时空值/越界回落到钳制值 [min, max]
 * - 触摸聚焦期间显示用户输入原文，失焦后与受控值对齐
 */
export default function NumberField({value, min, max, onChange, placeholder, disabled}: Props) {
  const clamp = (n: number) => Math.min(max, Math.max(min, n));
  const [text, setText] = useState(() => String(value));
  const [focused, setFocused] = useState(false);

  // 外部值变化（设置重载/表单重置）时同步显示；聚焦中不打断输入
  useEffect(() => {
    if (!focused) setText(String(value));
  }, [value, focused]);

  const commit = (raw: string) => {
    const n = parseInt(raw, 10);
    onChange(clamp(Number.isNaN(n) ? min : n));
  };

  return (
    <input
      type="text"
      inputMode="numeric"
      autoComplete="off"
      value={focused ? text : String(value)}
      placeholder={placeholder}
      disabled={disabled}
      onFocus={() => {
        setFocused(true);
        setText(String(value));
      }}
      onChange={(e) => {
        // 只留数字；多位时去掉前导零（单个 0 保留，供 min=0 的字段使用）
        const s = e.target.value.replace(/[^\d]/g, '').replace(/^0+(?=\d)/, '');
        setText(s);
        commit(s);
      }}
      onBlur={() => {
        setFocused(false);
        commit(text);
        setText(String(clamp(parseInt(text, 10) || min)));
      }}
    />
  );
}

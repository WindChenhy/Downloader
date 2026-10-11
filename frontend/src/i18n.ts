import {useSyncExternalStore} from 'react';

/**
 * 轻量 i18n：
 * - 语言偏好（跟随系统 / 简体中文 / English）存 localStorage（与主题/强调色同机制）。
 * - 中文为键：zh-CN 无需字典；en-US 缺条目时优雅回落中文，便于增量补全。
 * - 插值仅替换传入参数名的 {key} 占位，文案中的 {url}、{1..10} 等字面量不受影响。
 * - 界面语言即时生效；解析后的具体语言会同步进引擎设置（language 字段），
 *   供系统通知与托盘菜单使用（托盘菜单在下次启动时应用）。
 */

export type LangPref = 'system' | 'zh-CN' | 'en-US';
export type Lang = 'zh-CN' | 'en-US';

const LANG_KEY = 'lang';

function storageGet(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null; // 测试/受限环境无可用 localStorage
  }
}

function storageSet(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* 同上，忽略写入失败 */
  }
}

export function loadLangPref(): LangPref {
  const v = storageGet(LANG_KEY);
  return v === 'zh-CN' || v === 'en-US' || v === 'system' ? v : 'system';
}

/** 解析偏好为具体语言；system 跟随浏览器语言（zh 开头 → 中文）。 */
export function resolveLang(pref: LangPref): Lang {
  if (pref === 'zh-CN' || pref === 'en-US') return pref;
  return (navigator.language ?? '').toLowerCase().startsWith('zh') ? 'zh-CN' : 'en-US';
}

let currentPref: LangPref = loadLangPref();
const listeners = new Set<() => void>();

/** 订阅语言偏好变化（React 经 useSyncExternalStore 使用）。 */
export function subscribeLang(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function getLangPref(): LangPref {
  return currentPref;
}

export function setLangPref(pref: LangPref): void {
  currentPref = pref;
  storageSet(LANG_KEY, pref);
  listeners.forEach((fn) => fn());
}

/** useI18n 组件级入口：语言变化时触发重渲染。 */
export function useI18n() {
  const langPref = useSyncExternalStore(subscribeLang, getLangPref);
  return {t, langPref, setLang: setLangPref, lang: resolveLang(langPref)};
}

// 英文字典：键为中文原文。
const EN: Record<string, string> = {
  // 通用
  '取消': 'Cancel',
  '复制': 'Copy',
  '关闭': 'Close',
  '保存': 'Save',
  '返回': 'Back',
  '添加': 'Add',
  '已开启': 'On',
  '已关闭': 'Off',
  '已复制': 'Copied',

  // 顶栏
  '多线程下载工具': 'Multi-threaded Download Manager',
  '设置': 'Settings',
  '新建下载': 'New Download',
  '浅色': 'Light',
  '深色': 'Dark',
  '亮色': 'Light',
  '暗色': 'Dark',
  '跟随系统': 'System',

  // 任务状态与筛选
  '全部': 'All',
  '下载中': 'Downloading',
  '排队中': 'Queued',
  '已暂停': 'Paused',
  '已完成': 'Completed',
  '失败': 'Failed',

  // 任务列表
  '搜索文件名或链接…': 'Search file name or URL…',
  '还没有下载任务': 'No downloads yet',
  '点击右上角「新建下载」，粘贴链接开始':
    'Click "New Download" in the top-right corner and paste a link to start',
  '没有符合条件的项目': 'No matching items',
  '定时': 'Scheduled',
  '高优': 'High',
  '低优': 'Low',
  '{n} 连接': '{n} connections',
  '限 {v}': 'Cap {v}',
  '{v} 开始': 'Starts {v}',
  '剩余 {v}': '{v} left',
  '用时': 'Active',
  '总': 'Total',
  '均': 'Avg',
  '纯下载耗时 / 总耗时（含暂停，已冻结）':
    'Active download time / total time (incl. pauses, frozen when finished)',
  '上移': 'Move up',
  '下移': 'Move down',
  '打开所在目录': 'Open folder',
  '暂停': 'Pause',
  '继续': 'Resume',
  '重试': 'Retry',
  '删除任务': 'Delete Task',
  '操作失败：{e}': 'Action failed: {e}',
  '排序失败：{e}': 'Reorder failed: {e}',
  '打开目录失败：{e}': 'Failed to open folder: {e}',

  // 校验和
  '校验通过': 'Checksum OK',
  '校验失败': 'Checksum mismatch',
  '校验错误': 'Checksum error',
  '已计算': 'Calculated',

  // 任务详情
  '任务详情': 'Task Details',
  '文件名': 'File name',
  '链接': 'URL',
  '下载路径': 'Save path',
  '大小': 'Size',
  '状态': 'Status',
  '连接数': 'Connections',
  '优先级': 'Priority',
  '高': 'High',
  '低': 'Low',
  '普通': 'Normal',
  '限速': 'Speed limit',
  '跟随全局': 'Follow global',
  '定时开始': 'Scheduled start',
  '下载耗时': 'Active time',
  '总耗时': 'Total time',
  '平均速度': 'Average speed',
  '校验和': 'Checksum',
  '实际': 'Actual',
  '期望校验值': 'Expected checksum',
  '创建时间': 'Created at',
  '结束时间': 'Finished at',

  // 新建下载
  '下载链接': 'URL(s)',
  '已识别 {n} 条': '{n} URL(s) detected',
  '（批量）': '(batch)',
  '重命名（留空自动从链接获取）': 'Rename (leave empty to detect from the URL)',
  '批量时忽略': 'Ignored in batch mode',
  '可选，如： ubuntu-24.04.iso': 'Optional, e.g. ubuntu-24.04.iso',
  '目录分类': 'Folder category',
  '默认 / 自定义': 'Default / Custom',
  '未命名': 'Untitled',
  '保存到': 'Save to',
  '下载目录': 'Downloads folder',
  '连接数（1–128）': 'Connections (1–128)',
  '限速（KB/s，0 不限）': 'Speed limit (KB/s, 0 = unlimited)',
  '定时开始（可选）': 'Scheduled start (optional)',
  '校验算法（可选）': 'Checksum (optional)',
  '不校验': 'None',
  '期望校验值（可选）': 'Expected checksum (optional)',
  '留空则仅计算摘要': 'Leave empty to only compute the digest',
  '添加中…': 'Adding…',
  '批量创建 {n} 个任务': 'Create {n} tasks',
  '开始下载': 'Start Download',
  '请输入至少一条 http/https 下载链接（支持多行批量与 {1..10} 序列）':
    'Enter at least one http/https URL (multiple lines and {1..10} sequences are supported)',

  // 删除任务
  '「{name}」': '"{name}"',
  '已下载完成': 'downloaded',
  '文件（{size}）保存在 {dir}': 'The file ({size}) is saved in {dir}',
  '任务尚未完成，未下载的部分数据将被清理。':
    'The task is not finished; partial data will be cleaned up.',
  '删除失败：{e}': 'Delete failed: {e}',
  '仅删除记录': 'Delete record only',
  '连文件一起删除': 'Delete with files',

  // 关闭确认
  '关闭窗口': 'Close Window',
  '要将 Downloader 最小化到系统托盘，还是直接退出程序？':
    'Minimize Downloader to the system tray, or exit the application?',
  '最小化后仍可继续下载，并从托盘图标重新打开窗口。':
    'Downloads continue while minimized; reopen the window from the tray icon.',
  '记住我的选择，不再询问': "Remember my choice and don't ask again",
  '最小化到托盘': 'Minimize to tray',
  '直接退出': 'Exit',

  // 设置页
  '外观': 'Appearance',
  '主题': 'Theme',
  '强调色': 'Accent color',
  '语言': 'Language',
  '下载': 'Downloads',
  '默认保存目录': 'Default save folder',
  '下载目录分类': 'Folder categories',
  '分类名称，如：音乐': 'Category name, e.g. Music',
  '保存路径': 'Save path',
  '重命名分类': 'Rename category',
  '删除分类': 'Delete category',
  '每任务连接数（1–128）': 'Connections per task (1–128)',
  '同时下载任务数（1–10）': 'Concurrent tasks (1–10)',
  '限速（KB/s，0 不限速）': 'Speed limit (KB/s, 0 = unlimited)',
  '网络': 'Network',
  'User-Agent（留空使用内置默认）': 'User-Agent (leave empty for the built-in default)',
  '自定义请求头（每行一条 Key: Value）': 'Custom headers (one "Key: Value" per line)',
  '代理': 'Proxy',
  '不使用': 'None',
  '系统代理': 'System proxy',
  '自定义': 'Custom',
  '代理地址（http/socks5）': 'Proxy URL (http/socks5)',
  'GitHub 镜像加速': 'GitHub mirror acceleration',
  '下载 github.com 相关链接时自动改写为镜像地址':
    'Rewrite github.com URLs through the mirror automatically',
  '镜像模板（{url} 为原始链接）': 'Mirror template ({url} is the original URL)',
  '压缩包': 'Archives',
  '自动解压压缩包': 'Auto-extract archives',
  '下载完成后解压到同目录下与压缩包同名的文件夹（zip / tar / tar.gz 等）':
    'Extract to a same-named folder next to the archive when finished (zip / tar / tar.gz, etc.)',
  '通知': 'Notifications',
  '下载完成通知': 'Notify on download completed',
  '默认开启': 'On by default',
  '下载失败通知': 'Notify on download failed',
  '创建任务通知': 'Notify on task created',
  '默认关闭，避免批量添加时打扰': 'Off by default to avoid noise when adding in bulk',
  '暂停任务通知': 'Notify on task paused',
  '默认关闭': 'Off by default',
  '全部完成动作': 'After all downloads',
  '当所有任务下载结束后': 'When all downloads finish',
  '不执行': 'Do nothing',
  '打开下载目录': 'Open downloads folder',
  '关机（60 秒后）': 'Shut down (after 60 s)',
  '睡眠': 'Sleep',
  '退出本程序': 'Exit this app',
  '任务列表': 'Task list',
  '导出任务列表': 'Export tasks',
  '导入任务列表': 'Import tasks',
  '导出失败：{e}': 'Export failed: {e}',
  '导入完成：新建 {ok}，跳过/失败 {bad}': 'Imported: {ok} created, {bad} skipped/failed',
  '已导入 {n} 个任务': 'Imported {n} tasks',
  '导入失败：{e}': 'Import failed: {e}',
  '窗口': 'Window',
  '点击关闭按钮时': 'When clicking the close button',
  '每次询问': 'Ask every time',
  '集成': 'Integrations',
  '剪贴板监听': 'Clipboard monitoring',
  '复制下载链接时自动弹出新建下载': 'Open the new-download dialog when a link is copied',
  '本地 REST API': 'Local REST API',
  '仅供本机访问的自动化接口': 'Automation API for local access only',
  'API 端口（保存后立即生效）': 'API port (applied immediately after saving)',
  'API Token（请求须携带 X-API-Token 或 Authorization: Bearer 头）':
    'API token (send it via the X-API-Token or Authorization: Bearer header)',
  'API / 导入允许的保存目录白名单（每行一个，留空 = 仅默认下载目录）':
    'Allowed save directories for API / import (one per line; empty = default folder only)',
  '保存失败：{e}': 'Save failed: {e}',
  '已保存 ✓': 'Saved ✓',
  '分类名称': 'Category name',

  // 强调色
  '蓝': 'Blue',
  '紫': 'Violet',
  '绿': 'Green',
  '青': 'Teal',
  '橙': 'Orange',
  '红': 'Red',
  '粉': 'Pink',
  '灰': 'Slate',
};

/**
 * t 翻译：中文环境原样返回；英文环境查字典，缺条目回落中文。
 * params 只替换显式传入的 {key}，文案中的 {url}、{1..10} 等字面量不受影响。
 */
export function t(text: string, params?: Record<string, string | number>): string {
  let out = text;
  if (resolveLang(currentPref) === 'en-US') {
    const hit = EN[text];
    if (hit !== undefined) out = hit;
  }
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      out = out.replaceAll(`{${k}}`, String(v));
    }
  }
  return out;
}

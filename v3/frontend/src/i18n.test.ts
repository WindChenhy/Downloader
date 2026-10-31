import {afterEach, describe, expect, it, vi} from 'vitest';

import {getLangPref, loadLangPref, resolveLang, setLangPref, subscribeLang, t} from './i18n';

// node 测试环境无 localStorage/navigator：装内存假件
function installBrowserStubs(locale: string) {
  const store = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    clear: () => store.clear(),
  });
  vi.stubGlobal('navigator', {language: locale});
  return store;
}

describe('i18n', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    setLangPref('system');
  });

  it('中文环境下 t 原样返回（中文为键，无需字典）', () => {
    installBrowserStubs('zh-CN');
    setLangPref('zh-CN');
    expect(t('下载中')).toBe('下载中');
    expect(t('未收录的文案')).toBe('未收录的文案');
  });

  it('英文环境查字典，缺条目回落中文', () => {
    installBrowserStubs('en-US');
    setLangPref('en-US');
    expect(t('下载中')).toBe('Downloading');
    expect(t('尚未翻译的文案')).toBe('尚未翻译的文案');
  });

  it('插值只替换传入参数，文案中的 {1..10}、{url} 字面量不受影响', () => {
    installBrowserStubs('en-US');
    setLangPref('en-US');
    expect(t('{n} 连接', {n: 8})).toBe('8 connections');
    expect(t('剩余 {v}', {v: '5 分'})).toBe('5 分 left');
    expect(t('请输入至少一条 http/https 下载链接（支持多行批量与 {1..10} 序列）')).toBe(
      'Enter at least one http/https URL (multiple lines and {1..10} sequences are supported)',
    );
    expect(t('镜像模板（{url} 为原始链接）')).toBe('Mirror template ({url} is the original URL)');
  });

  it('system 跟随浏览器语言', () => {
    installBrowserStubs('zh-CN');
    expect(resolveLang('system')).toBe('zh-CN');
    installBrowserStubs('en-US');
    expect(resolveLang('system')).toBe('en-US');
    expect(resolveLang('en-US')).toBe('en-US');
  });

  it('偏好持久化到 localStorage，非法值回落 system', () => {
    installBrowserStubs('zh-CN');
    setLangPref('en-US');
    expect(localStorage.getItem('lang')).toBe('en-US');
    expect(loadLangPref()).toBe('en-US');
    localStorage.setItem('lang', 'fr-FR');
    expect(loadLangPref()).toBe('system');
  });

  it('setLangPref 通知订阅者', () => {
    installBrowserStubs('zh-CN');
    const spy = vi.fn();
    const unsub = subscribeLang(spy);
    setLangPref('zh-CN');
    expect(spy).toHaveBeenCalledTimes(1);
    unsub();
    setLangPref('en-US');
    expect(spy).toHaveBeenCalledTimes(1);
    expect(getLangPref()).toBe('en-US');
  });
});

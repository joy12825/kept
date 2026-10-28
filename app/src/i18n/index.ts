// 界面语言（v1.10.0）：中文原文作键，英文放在 en/ 下的对照表里；缺翻译时显示中文。
// 语言在页面启动时定好（main.tsx 先调 startLanguage 再加载界面），之后不变；换语言就重新加载页面。
// 所以 t() 可以在模块顶层用（状态名之类的常量），不需要 React 的上下文。

export type Lang = 'zh' | 'en';
/** 账号上的设置：'' 跟随浏览器。 */
export type LangPreference = '' | Lang;

/** 一条英文：普通文字，或按 n 区分单复数（# 换成 n）。 */
export type Entry = string | { one: string; other: string };
export type Catalog = Record<string, Entry>;

const STORAGE_KEY = 'kept.lang';
let lang: Lang = 'zh';
let catalog: Catalog = {};

export const currentLang = () => lang;
/** 给 Intl 用的语言标记。 */
export const locale = () => (lang === 'en' ? 'en' : 'zh-CN');

/** 浏览器的语言：中文（任何地区）用中文，其他用英文。 */
export function browserLang(langs: readonly string[] = navigator.languages ?? [navigator.language]): Lang {
  const first = langs.find(Boolean)?.toLowerCase() ?? 'zh';
  return first.startsWith('zh') ? 'zh' : 'en';
}

export function resolve(pref: LangPreference, langs?: readonly string[]): Lang {
  return pref === 'zh' || pref === 'en' ? pref : browserLang(langs);
}

/** 这台浏览器记下的设置（账号上的设置的副本）；读不到时跟随浏览器。 */
export function savedPreference(): LangPreference {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return v === 'zh' || v === 'en' ? v : '';
  } catch {
    return '';
  }
}

function savePreference(pref: LangPreference) {
  try {
    if (pref) localStorage.setItem(STORAGE_KEY, pref);
    else localStorage.removeItem(STORAGE_KEY);
  } catch { /* 隐私模式等：下次再按浏览器语言 */ }
}

/** 页面启动时调用一次：定语言，英文时加载对照表。 */
export async function startLanguage(): Promise<Lang> {
  lang = resolve(savedPreference());
  if (lang === 'en') catalog = (await import('./en')).default;
  document.documentElement.lang = locale();
  return lang;
}

/** 测试用：直接设语言和对照表。 */
export function setLanguageForTest(l: Lang, c: Catalog = {}) {
  lang = l;
  catalog = c;
}

/**
 * 换语言（账号设置改了，或登录后发现账号上的设置和这台浏览器记下的不同）。
 * 记下来；要显示的语言变了就重新加载页面，返回 true。
 */
export function applyPreference(pref: LangPreference, reload: () => void = () => window.location.reload()): boolean {
  if (pref === savedPreference()) return false;
  savePreference(pref);
  if (resolve(pref) === lang) return false;
  reload();
  return true;
}

const plural = new Map<Lang, Intl.PluralRules>();

function fill(s: string, vars?: Record<string, string | number>) {
  if (!vars) return s;
  return s.replace(/\{(\w+)\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m));
}

/**
 * 翻译一段界面文字。zh 是中文原文（也是对照表的键），{name} 是占位符。
 * 英文对照表里没有时用中文。
 */
export function t(zh: string, vars?: Record<string, string | number>): string {
  if (lang === 'zh') return fill(zh, vars);
  const e = catalog[zh];
  if (e === undefined) return fill(zh, vars);
  if (typeof e === 'string') return fill(e, vars);
  const n = Number(vars?.n ?? 0);
  let rules = plural.get(lang);
  if (!rules) plural.set(lang, (rules = new Intl.PluralRules(locale())));
  return fill((rules.select(n) === 'one' ? e.one : e.other).replace(/#/g, String(n)), vars);
}

/**
 * 带语境的翻译：同一个中文在不同地方意思不同时用（“目录”是目录页还是书的目录）。
 * 中文照常显示 zh；对照表的键是 “中文|语境”。
 */
export function tc(context: string, zh: string, vars?: Record<string, string | number>): string {
  if (lang === 'zh') return fill(zh, vars);
  const key = `${zh}|${context}`;
  return key in catalog ? t(key, vars) : fill(zh, vars);
}

import React, { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react';
import { defaultDocList, type DocListState } from '../api/filters';
import type { ListQuery } from '../api/documents';

export interface DocNav { ids: number[]; from: string; label: string; query?: ListQuery }
/** 任务托盘里的一行：先显示上传进度，拿到任务 ID 后改为显示入库任务的进度（见 api/documents.ts）。 */
export interface Upload { key: string; name: string; pct: number; jobId?: number; error?: string; documentId?: number; needsPassword?: boolean; versionOf?: number }
/** 确认对话框；safe 表示不是删除一类的操作，按钮用主色而不是红色。 */
export interface Confirm { title: string; body: string; verb: string; needText?: string; safe?: boolean; run: () => void }

/** 外观（v1.5.9）：跟随系统、浅色或深色；mode 是实际用的那一种。 */
export type ThemePref = 'system' | 'light' | 'dark';

interface State {
  theme: ThemePref; mode: 'light' | 'dark'; density: 'comfy' | 'compact';
  /** 真实文档列表的状态（全部文件、条目页），按列表区分 */
  docLists: Record<string, DocListState>;
  /** 打开详情页时记下的来源列表：上一份 / 下一份按它的顺序，返回时回到 from */
  docNav: DocNav | null;
  scroll: Record<string, number>;
  uploads: Upload[]; trayOpen: boolean;
  toast: { text: string; undo?: () => void; key: number } | null;
  cmdOpen: boolean; uploadOpen: boolean; confirm: Confirm | null;
  /** “移到其他空间”要移的文档（v1.3.8）；对话框放在布局里，详情页里的文档移走后对话框仍在 */
  moveDocs: { id: number; title: string }[] | null;
  collapsed: boolean | null; mobileNav: boolean;
  /** 快捷键说明（v1.5.9） */
  shortcutsOpen: boolean;
  /** 上传前发现同一抽屉里有同名文档，等用户决定（v1.7.13）；文件记在 api/documents.ts 里 */
  sameName: { token: string; matches: { name: string; documentId: number; title: string }[] } | null;
}

const LS = 'kept-proto-prefs';
function loadPrefs(): Partial<State> {
  try { const p = JSON.parse(localStorage.getItem(LS) || 'null'); return p || {}; } catch { return {}; }
}

const systemDark = () => typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-color-scheme: dark)').matches;
const resolve = (t: ThemePref): 'light' | 'dark' => (t === 'system' ? (systemDark() ? 'dark' : 'light') : t);

function initial(): State {
  const p = loadPrefs();
  // 以前只存了 mode（没选过也存成 light）：选过深色的保持深色，其余改为跟随系统
  const theme: ThemePref = p.theme === 'system' || p.theme === 'light' || p.theme === 'dark' ? p.theme : p.mode === 'dark' ? 'dark' : 'system';
  return {
    theme, mode: resolve(theme), density: (p.density as any) || 'comfy',
    docLists: {}, docNav: null, scroll: {},
    uploads: [], trayOpen: true, toast: null, cmdOpen: false, uploadOpen: false, confirm: null, moveDocs: null,
    collapsed: null, mobileNav: false, shortcutsOpen: false, sameName: null,
  };
}

function useStore() {
  const [s, setS] = useState<State>(initial);
  const ref = useRef(s); ref.current = s;
  const timer = useRef<number>();

  useEffect(() => {
    try { localStorage.setItem(LS, JSON.stringify({ theme: s.theme, mode: s.mode, density: s.density })); } catch { /* 隐私模式等：不记住也能用 */ }
  }, [s.theme, s.mode, s.density]);
  // 跟随系统时，系统切换深浅色立即跟着变
  useEffect(() => {
    if (s.theme !== 'system' || !window.matchMedia) return;
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const h = () => setS((p) => ({ ...p, mode: mq.matches ? 'dark' : 'light' }));
    mq.addEventListener?.('change', h);
    return () => mq.removeEventListener?.('change', h);
  }, [s.theme]);

  const set = useCallback((patch: Partial<State> | ((s: State) => Partial<State>)) => {
    setS((prev) => ({ ...prev, ...(typeof patch === 'function' ? patch(prev) : patch) }));
  }, []);

  const toast = useCallback((text: string, undo?: () => void) => {
    window.clearTimeout(timer.current);
    setS((p) => ({ ...p, toast: { text, undo, key: Date.now() } }));
    timer.current = window.setTimeout(() => setS((p) => ({ ...p, toast: null })), 4000);
  }, []);

  const setDocList = useCallback((key: string, patch: Partial<DocListState> | ((l: DocListState) => Partial<DocListState>)) => {
    setS((prev) => { const cur = prev.docLists[key] || defaultDocList(); const p = typeof patch === 'function' ? patch(cur) : patch; return { ...prev, docLists: { ...prev.docLists, [key]: { ...cur, ...p } } }; });
  }, []);
  const confirm = useCallback((c: Confirm | null) => set({ confirm: c }), [set]);
  const setTheme = useCallback((theme: ThemePref) => set({ theme, mode: resolve(theme) }), [set]);

  return { s, set, toast, setDocList, confirm, setTheme };
}

type Store = ReturnType<typeof useStore>;
const Ctx = createContext<Store | null>(null);

export function StoreProvider({ children }: { children: React.ReactNode }) {
  const store = useStore();
  return <Ctx.Provider value={store}>{children}</Ctx.Provider>;
}

export function useApp() {
  return useContext(Ctx)!;
}

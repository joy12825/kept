// 最近打开的文档和最近的搜索（⌘K 没有输入时显示），按空间分别记在浏览器里。
import { getWorkspaceId } from './client';

export interface RecentDoc { id: number; title: string }
interface Recent { docs: RecentDoc[]; searches: string[] }

const MAX = 5;
const key = () => `kept-recent:${getWorkspaceId() ?? 'personal'}`;

export function readRecent(): Recent {
  try {
    const r = JSON.parse(localStorage.getItem(key()) || 'null') as Recent | null;
    return { docs: r?.docs ?? [], searches: r?.searches ?? [] };
  } catch {
    return { docs: [], searches: [] };
  }
}

function write(r: Recent) {
  try { localStorage.setItem(key(), JSON.stringify(r)); } catch { /* 隐私模式或空间已满 */ }
}

export function rememberDoc(d: RecentDoc) {
  const r = readRecent();
  write({ ...r, docs: [d, ...r.docs.filter((x) => x.id !== d.id)].slice(0, MAX) });
}

export function rememberSearch(q: string) {
  const t = q.trim();
  if (!t) return;
  const r = readRecent();
  write({ ...r, searches: [t, ...r.searches.filter((x) => x !== t)].slice(0, MAX) });
}

export function forgetDoc(id: number) {
  const r = readRecent();
  write({ ...r, docs: r.docs.filter((x) => x.id !== id) });
}

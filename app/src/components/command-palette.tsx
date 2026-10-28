// ⌘K 命令面板（UI 设计规范 5.9）：边输边搜文档（最后一个词按前缀匹配），按名称或拼音找往来方、类型、标签、
// 自定义字段、抽屉、常用筛选、设置中的页面，以及常用操作；最后一项“在全部文件中搜索”。没有输入时显示最近打开的文档和最近的搜索。
// 原型中的“问问 AI”随 AI 阶段。
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Box, Dialog, InputBase, List, ListItemButton, ListItemIcon, ListItemText, ListSubheader, Typography } from '@mui/material';
import { useQuery } from '@tanstack/react-query';
import { CalendarDays, Files, FileText, Inbox, Library, ListFilter, Plus, Search, Settings, Tags, Trash2, BookUser, Shapes, History, TextCursorInput, CopyCheck, Users, ChartGantt } from 'lucide-react';
import { api, ApiError, unwrap } from '../api/client';
import type { DocumentList } from '../api/documents';
import { readRecent, rememberSearch } from '../api/recent';
import { useSavedViews } from '../api/views';
import { useMe } from '../api/session';
import { useTaxonomy } from '../api/taxonomy';
import { SETTINGS_KEYWORDS, settingsTabs } from '../pages/settings';
import { useRole } from '../api/workspaces';
import { useApp } from '../store/app-store';
import { searchPatch } from './doc-list';
import { DrawerIcon, SW } from './icons';
import { useTaxonomyIndex } from './pickers';
import { useApplyView } from './saved-view-item';
import { t, tc } from '../i18n';

interface Item { key: string; icon: React.ReactNode; text: React.ReactNode; label: string; sub?: string; run: () => void }
interface Group { label: string; items: Item[] }

/** 名称或拼音（连写、去空格）包含输入。 */
const matches = (name: string, sortKey: string | undefined, q: string) =>
  name.toLowerCase().includes(q) || (!!sortKey && sortKey.includes(q.replace(/\s+/g, '')));

function useDebounced<T>(value: T, ms: number) {
  const [v, setV] = useState(value);
  useEffect(() => { const timer = window.setTimeout(() => setV(value), ms); return () => window.clearTimeout(timer); }, [value, ms]);
  return v;
}

export function CommandPalette() {
  const app = useApp();
  const open = app.s.cmdOpen;
  return open ? <Palette onClose={() => app.set({ cmdOpen: false })} /> : null;
}

function Palette({ onClose }: { onClose: () => void }) {
  const app = useApp();
  const nav = useNavigate();
  const role = useRole();
  const ix = useTaxonomyIndex();
  const views = useSavedViews();
  const fields = useTaxonomy('custom-fields');
  const me = useMe().data;
  const applyView = useApplyView();
  const [q, setQ] = useState('');
  const [active, setActive] = useState(0);
  const listRef = useRef<HTMLUListElement>(null);
  const cq = q.trim().toLowerCase();
  const dq = useDebounced(q.trim(), 150);
  const recent = useMemo(readRecent, []);

  const docs = useQuery<DocumentList, ApiError>({
    queryKey: ['documents', 'palette', dq],
    queryFn: async () => unwrap(await api.GET('/api/v1/documents', {
      params: { query: { filter: JSON.stringify({ text: dq, prefix: true }), sort: 'relevance', limit: 5 } },
    })),
    enabled: dq.length > 0,
    placeholderData: (prev) => prev,
  });

  const go = (to: string, state?: unknown) => { nav(to, { state }); onClose(); };
  const searchAll = (text: string) => {
    rememberSearch(text);
    app.setDocList('files', (l) => ({ ...searchPatch(l, text), f: { drawer: [], type: [], correspondent: [], tag: [], year: [] }, extra: undefined }));
    go('/files');
  };

  const groups: Group[] = useMemo(() => {
    const doc = (id: number, title: string, sub?: string, snippet?: React.ReactNode): Item => ({
      key: `doc:${id}`, icon: <FileText size={18} strokeWidth={SW} />, text: snippet ? <>{title}<Typography component="span" sx={{ display: 'block', fontSize: 12, color: 'text.secondary' }} noWrap>{snippet}</Typography></> : title,
      // 搜出来的文档：带上搜索词，预览里自动查找（v1.5.4）
      label: title, sub, run: () => go(`/docs/${id}`, cq ? { find: q.trim() } : undefined),
    });
    if (!cq) {
      return [
        { label: t('最近打开'), items: recent.docs.map((d) => doc(d.id, d.title)) },
        { label: t('最近搜索'), items: recent.searches.map((s) => ({ key: `search:${s}`, icon: <History size={18} strokeWidth={SW} />, text: s, label: s, run: () => searchAll(s) })) },
      ].filter((g) => g.items.length);
    }
    const taxo = (kind: 'correspondents' | 'document-types' | 'tags' | 'custom-fields' | 'persons', icon: React.ReactNode, list: { id: number; name: string; sortKey: string }[], path?: (id: number) => string) =>
      list.filter((x) => matches(path?.(x.id) ?? x.name, x.sortKey, cq)).slice(0, 3)
        .map((x) => ({ key: `${kind}:${x.id}`, icon, text: path?.(x.id) ?? x.name, label: x.name, run: () => go(`/directory/${kind}/${x.id}`) }));
    const actions: [string, React.ReactNode, () => void, boolean][] = [
      [t('上传文件'), <Plus size={18} strokeWidth={SW} />, () => app.set({ uploadOpen: true, cmdOpen: false }), role.canWrite],
      [t('去待整理'), <Inbox size={18} strokeWidth={SW} />, () => go('/inbox'), true],
      [t('去全部文件'), <Files size={18} strokeWidth={SW} />, () => go('/files'), true],
      [t('去年度回顾'), <CalendarDays size={18} strokeWidth={SW} />, () => go('/review'), true],
      [t('去目录'), <Library size={18} strokeWidth={SW} />, () => go('/directory'), true],
      [t('去回收站'), <Trash2 size={18} strokeWidth={SW} />, () => go('/trash'), true],
      [t('去近似重复'), <CopyCheck size={18} strokeWidth={SW} />, () => go('/duplicates'), true],
      [t('任务记录'), <ChartGantt size={18} strokeWidth={SW} />, () => go('/tasks'), true],
      [t('打开设置'), <Settings size={18} strokeWidth={SW} />, () => go('/settings'), true],
    ];
    const tagPath = (id: number) => ix.tagTree.find((n) => n.tag.id === id)?.path ?? '';
    return [
      {
        label: t('文档'), items: (docs.data?.items ?? []).map((d) => doc(d.id, d.title, d.createdDate,
          d.highlight?.length ? d.highlight.map((s, i) => (s.hit ? <Box key={i} component="mark" sx={{ bgcolor: 'surface.secondaryContainer', color: 'text.primary', borderRadius: 0.5 }}>{s.text}</Box> : <span key={i}>{s.text}</span>)) : undefined)),
      },
      {
        label: tc('复数', '抽屉'), items: ix.drawerList.filter((d) => d.name.toLowerCase().includes(cq)).slice(0, 3)
          .map((d) => ({ key: `drawer:${d.id}`, icon: <DrawerIcon drawer={d} size={20} />, text: d.name, label: d.name, sub: t('{n} 份', { n: d.documentCount }), run: () => go(`/drawers/${d.id}`) })),
      },
      { label: tc('复数', '往来方'), items: taxo('correspondents', <BookUser size={18} strokeWidth={SW} />, ix.correspondentList) },
      { label: tc('复数', '家人'), items: taxo('persons', <Users size={18} strokeWidth={SW} />, ix.personList) },
      { label: tc('复数', '类型'), items: taxo('document-types', <Shapes size={18} strokeWidth={SW} />, ix.typeList) },
      { label: tc('复数', '标签'), items: taxo('tags', <Tags size={18} strokeWidth={SW} />, ix.tagTree.map((n) => n.tag), tagPath) },
      { label: tc('复数', '自定义字段'), items: taxo('custom-fields', <TextCursorInput size={18} strokeWidth={SW} />, fields.data ?? []) },
      {
        label: t('设置'), items: settingsTabs({ isAdmin: !!me?.user.isAdmin, role: me?.workspace.role }).filter(([k, l]) => `${l} ${SETTINGS_KEYWORDS[k] ?? ''}`.toLowerCase().includes(cq)).slice(0, 4)
          .map(([k, l]) => ({ key: `settings:${k}`, icon: <Settings size={18} strokeWidth={SW} />, text: t('设置 · {name}', { name: l }), label: l, run: () => go(`/settings/${k}`) })),
      },
      {
        label: t('常用筛选'), items: (views.data ?? []).filter((v) => v.name.toLowerCase().includes(cq)).slice(0, 3)
          .map((v) => ({ key: `view:${v.id}`, icon: <ListFilter size={18} strokeWidth={SW} />, text: v.name, label: v.name, run: () => { applyView(v); onClose(); } })),
      },
      { label: t('操作'), items: actions.filter(([l, , , ok]) => ok && l.toLowerCase().includes(cq)).map(([l, icon, run]) => ({ key: `action:${l}`, icon, text: l, label: l, run })) },
    ].filter((g) => g.items.length);
  }, [cq, docs.data, ix, views.data, recent, role.canWrite, fields.data, me]); // eslint-disable-line react-hooks/exhaustive-deps

  // 键盘上下选择；最后一项“在全部文件中搜索”
  const flat: Item[] = useMemo(() => {
    const items = groups.flatMap((g) => g.items);
    if (cq) items.push({ key: 'search-all', icon: <Search size={18} strokeWidth={SW} />, text: t('在全部文件中搜索“{q}”', { q: q.trim() }), label: t('在全部文件中搜索'), run: () => searchAll(q.trim()) });
    return items;
  }, [groups, cq, q]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => setActive(0), [cq]);
  useEffect(() => {
    listRef.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' });
  }, [active]);

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setActive((a) => Math.min(flat.length - 1, a + 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setActive((a) => Math.max(0, a - 1)); }
    else if (e.key === 'Enter') { e.preventDefault(); flat[active]?.run(); }
  };

  let index = 0;
  const row = (it: Item) => {
    const i = index++;
    return (
      <ListItemButton key={it.key} data-index={i} id={`palette-${i}`} role="option" aria-selected={i === active} selected={i === active}
        onClick={it.run} onMouseMove={() => setActive(i)} sx={{ borderRadius: 2, minHeight: 40, color: it.key === 'search-all' ? 'primary.main' : undefined }}>
        <ListItemIcon sx={{ color: it.key === 'search-all' ? 'primary.main' : 'text.secondary', minWidth: 36 }}>{it.icon}</ListItemIcon>
        <ListItemText primary={it.text} slotProps={{ primary: { noWrap: true, component: 'div' } }} />
        {it.sub && <Typography sx={{ fontSize: 12, color: 'text.secondary', ml: 1, flex: 'none' }}>{it.sub}</Typography>}
        {it.key === 'search-all' && <Typography sx={{ fontSize: 11, color: 'text.secondary', flex: 'none' }}>Enter</Typography>}
      </ListItemButton>
    );
  };

  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="sm" aria-label={t('搜索与命令')}
      slotProps={{ paper: { sx: { alignSelf: 'flex-start', mt: '12vh', p: 0, overflow: 'hidden', width: 640 } } }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, height: 56, px: 2.5, borderBottom: 1, borderColor: 'divider', color: 'text.secondary' }}>
        <Search size={18} strokeWidth={SW} />
        <InputBase autoFocus fullWidth value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={onKey}
          placeholder={t('搜索文档、往来方、标签，或输入操作…')} sx={{ fontSize: 16 }}
          inputProps={{ 'aria-label': t('搜索与命令'), role: 'combobox', 'aria-expanded': true, 'aria-controls': 'palette-list', 'aria-activedescendant': flat.length ? `palette-${active}` : undefined }} />
        <Box sx={{ fontSize: 11, px: 0.75, border: 1, borderColor: 'divider', borderRadius: 1 }}>Esc</Box>
      </Box>
      <List ref={listRef} id="palette-list" role="listbox" sx={{ maxHeight: 420, overflow: 'auto', px: 1 }} dense>
        {groups.map((g) => (
          <React.Fragment key={g.label}>
            <ListSubheader role="presentation" sx={{ lineHeight: '32px', fontSize: 12 }}>{g.label}</ListSubheader>
            {g.items.map(row)}
          </React.Fragment>
        ))}
        {cq && row(flat[flat.length - 1])}
        {!cq && !groups.length && <Typography sx={{ px: 2, py: 3, fontSize: 13, color: 'text.secondary' }}>{t('输入关键词搜索文档，或输入“上传”“回收站”等操作。')}</Typography>}
        {cq && docs.error?.code === 'search_unavailable' && <Typography sx={{ px: 2, py: 1, fontSize: 12, color: 'text.secondary' }}>{docs.error.message}</Typography>}
      </List>
    </Dialog>
  );
}

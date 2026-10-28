import React, { useEffect, useRef, useState } from 'react';
import { warmPdf } from './pdf-lazy';
import { Outlet, useLocation, useMatch, useNavigate } from 'react-router-dom';
import { Box, Button, Divider, Drawer as MuiDrawer, IconButton, List, ListItemButton, ListItemIcon, ListItemText, Stack, Tooltip, Typography, useMediaQuery } from '@mui/material';
import { CalendarDays, CircleDashed, Files, Home, Inbox, LayoutGrid, Library, Menu as MenuIcon, MessagesSquare, PanelLeft, Plus, Search, Settings, Trash2, ClipboardCheck } from 'lucide-react';
import { useApp } from '../store/app-store';
import { useScenes } from '../api/health';
import { BrandMark, DrawerIcon, SW } from './icons';
import { WorkspaceSwitcher } from './workspace-switcher';
import { useRole } from '../api/workspaces';
import { useBranding } from '../api/branding';
import { useDrawers, usePreviewDrawers, useReorderDrawers, useSharedDrawers } from '../api/drawers';
import { useSharedCollections } from '../api/collections';
import { CollectionIcon } from '../pages/shared/sources';
import { DrawerDialog } from './drawer-dialogs';
import { t, tc } from '../i18n';

const READ_ONLY_UPLOAD = t('只看的成员不能上传');
import { ConfirmDialog, Toast } from './overlays';
import { CommandPalette } from './command-palette';
import { ShortcutsDialog } from './shortcuts-dialog';
import { GlobalDrop, SameNameDialog, TaskTray, UploadDialog } from './uploads';
import { TaskButton } from './task-center';
import { useEvents } from '../api/events';
import { AlertNotifier } from './alert-notifier';
import { useFacets, useInboxDocuments } from '../api/documents';
import { fromFilter, toFilter } from '../api/filters';
import { SavedViewItem, useApplyView } from './saved-view-item';
import { useReorderViews, useSavedViews } from '../api/views';
import { PrototypeBanner } from './prototype-banner';
import { MoveDialog } from './move-dialog';
import { EmergencyNotice, eeBuild } from '@ee';
import { EditionBanner } from './edition-notice';

function Sidebar({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
  const app = useApp(); const { s } = app;
  const role = useRole();
  const drawers = useDrawers().data;
  const shared = useSharedDrawers().data ?? [];
  const sharedCollections = useSharedCollections(eeBuild).data ?? [];
  const preview = usePreviewDrawers();
  const reorder = useReorderDrawers();
  const [newOpen, setNewOpen] = useState(false);
  const brand = useBranding();
  const inboxCount = useInboxDocuments().data?.total ?? 0;
  const nav = useNavigate(); const loc = useLocation();
  const dragId = useRef<number | null>(null);
  const dragView = useRef<number | null>(null);
  const reorderViews = useReorderViews();
  const go = (to: string) => { nav(to); onNavigate?.(); };
  const path = loc.pathname;
  // 详情页属于打开它的列表；docNav.from 可能带查询参数
  const fromPath = path.startsWith('/docs/') ? (s.docNav?.from ?? '/files').split('?')[0] : path;
  const views = useSavedViews();
  const applyView = useApplyView();
  const filesFilter = s.docLists.files ? toFilter(s.docLists.files) : '';
  // 启用的场景（v1.9.0）：“体检与化验”是“健康”，其余各占一项
  const scenes = useScenes(eeBuild).data?.scenes ?? [];
  const main = [
    { to: '/', label: t('首页'), icon: <Home size={18} strokeWidth={SW} /> },
    { to: '/inbox', label: t('待整理'), icon: <Inbox size={18} strokeWidth={SW} />, count: inboxCount || '' },
    { to: '/files', label: t('全部文件'), icon: <Files size={18} strokeWidth={SW} /> },
    { to: '/review', label: t('年度回顾'), icon: <CalendarDays size={18} strokeWidth={SW} /> },
    { to: '/ask', label: t('问答'), icon: <MessagesSquare size={18} strokeWidth={SW} /> },
    { to: '/checklists', label: t('办事清单'), icon: <ClipboardCheck size={18} strokeWidth={SW} /> },
    // 场景（v1.9.0）：健康只是其中之一，全都收在一个入口下
    ...(scenes.length ? [{ to: '/scenes', label: t('场景'), icon: <LayoutGrid size={18} strokeWidth={SW} /> }] : []),
  ];
  const item = (selected: boolean, onClick: () => void, icon: React.ReactNode, label: string, right?: React.ReactNode, extra?: Record<string, any>) => {
    const { key: k, ...rest } = extra || {};
    return (
      <Tooltip title={collapsed ? label : ''} placement="right" key={label + (k || '')}>
        <ListItemButton selected={selected} onClick={onClick} sx={{ flex: 'none', mb: 0.25, px: collapsed ? 1.5 : 1.625, justifyContent: collapsed ? 'center' : 'flex-start' }} {...rest}>
          <ListItemIcon sx={{ minWidth: collapsed ? 0 : 32 }}>{icon}</ListItemIcon>
          {!collapsed && <ListItemText primary={label} slotProps={{
            primary: { noWrap: true, sx: { fontSize: 14, fontWeight: selected ? 500 : 400 } }
          }} />}
          {!collapsed && right}
        </ListItemButton>
      </Tooltip>
    ); };
  const count = (n: React.ReactNode) => <Typography sx={{ fontSize: 12, color: 'text.secondary', ml: 1 }}>{n}</Typography>;

  // 拖动调整抽屉顺序：排在前面的规则优先，会让文档换抽屉时先说明再保存
  const moveDrawer = (from: number | null, to: number) => {
    if (!drawers || from === null || from === to) return;
    const ids = drawers.items.map((d) => d.id).filter((x) => x !== from);
    ids.splice(ids.indexOf(to), 0, from);
    const save = () => reorder.mutate(ids, { onSuccess: () => app.toast(t('已调整抽屉顺序')), onError: (e) => app.toast(e.message) });
    preview.mutate({ order: ids }, {
      onSuccess: (p) => {
        if (!p.moves) { save(); return; }
        app.confirm({
          title: t('调整抽屉的顺序？'), verb: t('调整'), safe: true,
          body: p.samples.length
            ? t('多个抽屉的规则都满足时，文档放进排在前面的抽屉。按新的顺序，{n} 份文档会换抽屉，例如“{title}”。手动放入的不受影响。', { n: p.moves, title: p.samples[0].title })
            : t('多个抽屉的规则都满足时，文档放进排在前面的抽屉。按新的顺序，{n} 份文档会换抽屉。手动放入的不受影响。', { n: p.moves }),
          run: () => { app.confirm(null); save(); },
        });
      },
      onError: (e) => app.toast(e.message),
    });
  };

  // 拖动或上移、下移调整常用筛选的顺序（v1.6.5）：只是显示顺序，直接保存
  const moveView = (from: number | null, to: number) => {
    const list = views.data ?? [];
    if (from === null || list[to]?.id === from) return;
    const ids = list.map((v) => v.id).filter((x) => x !== from);
    ids.splice(to, 0, from);
    reorderViews.mutate(ids, { onError: (e) => app.toast(e.message) });
  };

  return (
    <Box component="nav" aria-label={t('主导航')} className="kept-chrome" sx={{ width: collapsed ? 72 : 256, height: '100vh', display: 'flex', flexDirection: 'column', bgcolor: 'surface.containerLow', transition: 'width 200ms ease-out', overflow: 'hidden' }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.25, height: 64, px: collapsed ? 2.5 : 2.75, flex: 'none' }}>
        <BrandMark size={28} />
        {!collapsed && <Typography noWrap sx={{ flex: 1, fontSize: 16, fontWeight: 500 }}>{brand.name}</Typography>}
        {!collapsed && <Tooltip title={t('折叠侧边栏  [')}><IconButton size="small" onClick={() => app.set({ collapsed: true })}><PanelLeft size={18} strokeWidth={SW} /></IconButton></Tooltip>}
      </Box>
      {collapsed && <Box sx={{ px: 2, mb: 0.5 }}><Tooltip title={t('展开侧边栏  [')} placement="right"><IconButton onClick={() => app.set({ collapsed: false })}><PanelLeft size={18} strokeWidth={SW} /></IconButton></Tooltip></Box>}
      <Box sx={{ px: 1.5, pb: 1, flex: 'none' }}>
        <Box component="button" onClick={() => app.set({ cmdOpen: true })} sx={{ width: '100%', display: 'flex', alignItems: 'center', gap: 1, height: 40, px: 1.5, border: 0, borderRadius: 5, bgcolor: 'surface.containerHigh', color: 'text.secondary', font: 'inherit', fontSize: 13, cursor: 'pointer', justifyContent: collapsed ? 'center' : 'flex-start' }}>
          <Search size={16} strokeWidth={SW} />
          {!collapsed && <><Box component="span" sx={{ flex: 1, textAlign: 'left' }}>{t('搜索…')}</Box><Box component="span" sx={{ fontSize: 11, px: 0.5, border: 1, borderColor: 'divider', borderRadius: 1 }}>⌘K</Box></>}
        </Box>
      </Box>
      <List sx={{ flex: 1, minHeight: 0, overflowY: 'auto', overflowX: 'hidden', px: 1.5, py: 0.5 }} disablePadding>
        {main.map((m) => item(fromPath === m.to || ((m.to === '/ask' || m.to === '/checklists' || m.to === '/scenes') && fromPath.startsWith(m.to + '/'))
          || (m.to === '/scenes' && (fromPath === '/health' || fromPath.startsWith('/health/') || fromPath.startsWith('/visits/'))), () => go(m.to), m.icon, m.label, m.count ? count(m.count) : undefined))}
        <Box sx={{ display: 'flex', alignItems: 'center', height: 36, mt: 1.5, px: collapsed ? 0 : 2, justifyContent: collapsed ? 'center' : 'flex-start' }}>
          {collapsed ? <Divider sx={{ width: 24 }} /> : <><Typography
            variant="h6"
            sx={{
              color: "text.secondary",
              flex: 1
            }}>{tc('复数', '抽屉')}</Typography>{role.canWrite && <Tooltip title={t('新建抽屉')}><IconButton size="small" aria-label={t('新建抽屉')} onClick={() => setNewOpen(true)}><Plus size={16} strokeWidth={SW} /></IconButton></Tooltip>}</>}
        </Box>
        {(drawers?.items ?? []).map((d) => item(fromPath === `/drawers/${d.id}`, () => go(`/drawers/${d.id}`), <DrawerIcon drawer={d} size={22} shared={!!d.sharedCount} />, d.name,
          <>{d.alertCount > 0 && <Box aria-label={t('有提醒')} sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: 'warning.main', ml: 1 }} />}{count(d.documentCount)}</>,
          { key: d.id, draggable: role.canWrite, onDragStart: () => (dragId.current = d.id), onDragOver: (e: React.DragEvent) => e.preventDefault(),
            onDrop: (e: React.DragEvent) => { e.preventDefault(); moveDrawer(dragId.current, d.id); } }))}
        {shared.map((d) => item(fromPath === `/shared/${d.id}`, () => go(`/shared/${d.id}`), <DrawerIcon drawer={d} size={22} shared />, d.name,
          count(d.documentCount), { key: `shared-${d.id}`, title: t('来自“{name}”的只读共享', { name: d.sourceWorkspaceName }) }))}
        {sharedCollections.map((c) => item(fromPath === `/shared-collections/${c.id}`, () => go(`/shared-collections/${c.id}`), <CollectionIcon size={22} />, c.name,
          count(c.documentCount), { key: `collection-${c.id}`, title: t('来自“{name}”的只读共享集', { name: c.sourceWorkspaceName }) }))}
        {drawers && drawers.unfiled > 0 && item(false, () => {
          app.setDocList('files', (l) => ({ ...l, f: { ...l.f, drawer: [0] }, sel: [] }));
          go('/files');
        }, <Box sx={{ width: 22, display: 'flex', justifyContent: 'center', color: 'text.secondary' }}><CircleDashed size={18} strokeWidth={SW} /></Box>, t('未入抽屉'), count(drawers.unfiled), { key: 'unfiled' })}
        {!collapsed && (
          <>
            <Typography
              variant="h6"
              sx={{
                color: "text.secondary",
                height: 36,
                display: 'flex',
                alignItems: 'center',
                mt: 1.5,
                px: 2
              }}>{t('常用筛选')}</Typography>
            {(views.data ?? []).map((v) => {
              const state = fromFilter(v.filter);
              const active = fromPath === '/files' && filesFilter === toFilter(state);
              const apply = () => { applyView(v); onNavigate?.(); };
              const list = views.data ?? [];
              const i = list.indexOf(v);
              return (
                <SavedViewItem key={v.id} view={v} active={active} onApply={apply}
                  drag={role.canWrite ? { draggable: true, onDragStart: () => (dragView.current = v.id), onDragOver: (e) => e.preventDefault(),
                    onDrop: (e) => { e.preventDefault(); moveView(dragView.current, i); } } : undefined}
                  onMove={role.canWrite ? { up: i > 0 ? () => moveView(v.id, i - 1) : undefined, down: i < list.length - 1 ? () => moveView(v.id, i + 1) : undefined } : undefined} />
              );
            })}
            {views.data?.length === 0 && <Typography sx={{ px: 2, fontSize: 12, color: 'text.secondary' }}>{t('在全部文件中筛选后点“保存筛选”，会出现在这里。')}</Typography>}
          </>
        )}
      </List>
      <Box sx={{ flex: 'none', borderTop: 1, borderColor: 'divider', p: 1.5 }}>
        <Stack
          direction={collapsed ? 'column' : 'row'}
          spacing={0.5}
          sx={{
            justifyContent: "space-between",
            mb: 0.75
          }}>
          {([['/directory', t('目录'), Library], ['/trash', t('回收站'), Trash2], ['/settings', t('设置'), Settings]] as const).map(([to, label, I]) => {
            const act = fromPath.startsWith(to);
            return collapsed
              ? <Tooltip key={to} title={label} placement="right"><IconButton onClick={() => go(to)} sx={{ bgcolor: act ? 'surface.secondaryContainer' : undefined }}><I size={18} strokeWidth={SW} /></IconButton></Tooltip>
              : <Button key={to} size="small" onClick={() => go(to)} startIcon={<I size={16} strokeWidth={SW} />} sx={{ px: 1.25, minWidth: 0, color: act ? 'surface.onSecondaryContainer' : 'text.secondary', bgcolor: act ? 'surface.secondaryContainer' : undefined, '& .MuiButton-startIcon': { mr: 0.5 } }}>{label}</Button>;
          })}
        </Stack>
        <WorkspaceSwitcher collapsed={collapsed} go={go} />
      </Box>
      <DrawerDialog open={newOpen} onClose={() => setNewOpen(false)} onCreated={(d) => go(`/drawers/${d.id}`)} />
    </Box>
  );
}

export default function Layout() {
  useEvents();
  const inboxTotal = useInboxDocuments().data?.total ?? 0;
  const loc0 = useLocation();
  const allFiles = useFacets('', ['year'], loc0.pathname === '/files').data?.year?.reduce((n, b) => n + b.count, 0);
  const app = useApp(); const { s } = app; const nav = useNavigate(); const loc = useLocation();
  const md = useMediaQuery('(min-width:1024px)'); const lg = useMediaQuery('(min-width:1280px)');
  const collapsed = md ? (s.collapsed ?? !lg) : false;
  // 详情页和版本对比（v1.9.2）有自己的页头
  // 两个 useMatch 都要每次调用：用 || 连起来时进详情页会少调一个 hook，React 报错白屏
  const detailMatch = useMatch('/docs/:id'), compareMatch = useMatch('/docs/:id/compare');
  const isDetail = !!detailMatch || !!compareMatch;
  const g = useRef(false);
  const role = useRole();
  // 角色还没取到时不拦（服务端才是最终的把关）；取到后，只看的成员不能上传
  const readOnly = role.role !== undefined && !role.canWrite;

  // 空闲时预取 PDF 预览需要的 pdf.js，第一次打开文档时不用等它下载
  useEffect(() => { warmPdf(); }, []);

  // 上传有好几个入口（顶栏按钮、快捷键 U、命令面板、各页空状态）：在这里统一拦住只看的成员
  useEffect(() => {
    if (readOnly && s.uploadOpen) { app.set({ uploadOpen: false }); app.toast(READ_ONLY_UPLOAD); }
  }, [readOnly, s.uploadOpen, app]);

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement).tagName; const typing = ['INPUT', 'TEXTAREA', 'SELECT'].includes(tag) || (e.target as HTMLElement).isContentEditable;
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); app.set({ cmdOpen: true }); return; }
      if (typing || e.metaKey || e.ctrlKey || e.altKey || app.s.cmdOpen || app.s.uploadOpen) return;
      const k = e.key.toLowerCase();
      if (g.current) { g.current = false; const m: Record<string, string> = { h: '/', i: '/inbox', f: '/files', r: '/review' }; if (m[k]) nav(m[k]); return; }
      if (k === 'g') { g.current = true; window.setTimeout(() => (g.current = false), 1200); return; }
      if (e.key === '[') { if (md) app.set({ collapsed: !collapsed }); else app.set((p) => ({ mobileNav: !p.mobileNav })); return; }
      if (k === 'u') { e.preventDefault(); if (readOnly) app.toast(READ_ONLY_UPLOAD); else app.set({ uploadOpen: true }); }
      if (e.key === '?') { e.preventDefault(); app.set({ shortcutsOpen: true }); }
    };
    window.addEventListener('keydown', h); return () => window.removeEventListener('keydown', h);
  }, [app, nav, md, collapsed, readOnly]);

  const sceneList = useScenes(eeBuild).data?.scenes;
  const title = (() => {
    const p = loc.pathname;
    if (p === '/scenes') return t('场景');
    if (p.startsWith('/scenes/')) return sceneList?.find((x) => `/scenes/${x.id}` === p)?.name ?? '';
    if (p === '/inbox') return t('待整理 · {n} 份', { n: inboxTotal });
    if (p === '/files') return allFiles === undefined ? t('全部文件') : t('全部文件 · {count} 份', { count: allFiles.toLocaleString('en-US'), n: allFiles });
    if (p.startsWith('/directory')) return t('目录');
    if (p === '/trash') return t('回收站');
    if (p === '/tasks') return t('任务记录');
    if (p === '/cleanup') return t('可以清理');
    if (p === '/checklists') return t('办事清单');
    if (p === '/health') return t('体检与化验');
    if (p === '/health/report') return t('给医生看');
    if (p.startsWith('/settings')) return t('设置');
    return '';
  })();

  return (
    <Box sx={{ display: 'flex', alignItems: 'flex-start', minHeight: '100vh', bgcolor: 'background.default' }}>
      <AlertNotifier />
      {md ? (
        <Box className="kept-chrome" sx={{ position: 'sticky', top: 0, flex: 'none', zIndex: 1100 }}><Sidebar collapsed={collapsed} /></Box>
      ) : (
        <MuiDrawer open={s.mobileNav} onClose={() => app.set({ mobileNav: false })}><Sidebar collapsed={false} onNavigate={() => app.set({ mobileNav: false })} /></MuiDrawer>
      )}
      <Box component="main" sx={{ flex: 1, minWidth: 0, minHeight: '100vh' }}>
        {!isDetail && (
          <Box className="kept-chrome" sx={{ position: 'sticky', top: 0, zIndex: 1000, display: 'flex', alignItems: 'center', gap: 1.5, height: 64, px: 3, bgcolor: 'background.default' }}>
            {!md && <IconButton onClick={() => app.set({ mobileNav: true })} aria-label={t('打开菜单')}><MenuIcon size={20} strokeWidth={SW} /></IconButton>}
            {title && <Typography variant="h4" noWrap>{title}</Typography>}
            <Box sx={{
              flex: 1
            }} />
            <TaskButton />
            <Button variant="outlined" startIcon={<Search size={16} strokeWidth={SW} />}
              onClick={() => app.set({ cmdOpen: true })} sx={{ height: 36 }}>{t('搜索')}<Box component="span" sx={{ ml: 1, fontSize: 11, color: 'text.secondary' }}>⌘K</Box></Button>
            <Tooltip title={readOnly ? READ_ONLY_UPLOAD : ''}>
              {/* span：禁用的按钮不触发悬停事件，提示要挂在外层 */}
              <span><Button variant="contained" disabled={readOnly} startIcon={<Plus size={18} strokeWidth={SW} />}
                onClick={() => app.set({ uploadOpen: true })}>{t('上传')}</Button></span>
            </Tooltip>
          </Box>
        )}
        <PrototypeBanner />
        {!isDetail && <EmergencyNotice />}
        {!isDetail && <EditionBanner />}
        <Outlet />
      </Box>
      <CommandPalette /><UploadDialog /><SameNameDialog /><ConfirmDialog /><TaskTray /><Toast /><GlobalDrop /><ShortcutsDialog />
      <MoveDialog open={!!s.moveDocs} docs={s.moveDocs ?? []} onClose={() => app.set({ moveDocs: null })} />
    </Box>
  );
}

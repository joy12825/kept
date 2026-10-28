// 侧边栏里的一个常用筛选：点击套用到全部文件；悬停出现“⋯”，创建者和空间管理员可以用当前的设置更新、重命名、删除；
// 空间管理员还可以按这个条件共享给其他空间（v1.0 阶段 37）。
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { api, unwrap } from '../api/client';
import { fromFilter, parseSort, toFilter, type ColumnKey, type DocListState } from '../api/filters';
import { Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, ListItemButton, ListItemIcon, ListItemText, Menu, MenuItem, TextField, Typography } from '@mui/material';
import { Ellipsis, ListFilter } from 'lucide-react';
import { useMe } from '../api/session';
import { useTaxonomy } from '../api/taxonomy';
import { useDeleteView, useUpdateView, type SavedView } from '../api/views';
import { useRole } from '../api/workspaces';
import { useApp } from '../store/app-store';
import { SW } from './icons';
import { CollectionDialog } from '@ee';
import { t } from '../i18n';

/** 列表的显示方式，保存在常用筛选里。 */
export const viewDisplay = (l: DocListState): SavedView['display'] => ({ view: l.view, ...(l.columns ? { columns: l.columns } : {}) });

/** 套用一个常用筛选：全部文件换成它的条件、排序和显示方式。 */
export function useApplyView() {
  const app = useApp();
  const nav = useNavigate();
  const fields = useTaxonomy('custom-fields').data;
  return (v: SavedView) => {
    app.setDocList('files', {
      ...fromFilter(v.filter, (id) => fields?.find((f) => f.id === id)?.dataType), ...(parseSort(v.sort) ?? { sort: 'created', dir: 'desc' }), sortBeforeSearch: undefined, sel: [],
      view: v.display?.view ?? 'list', columns: v.display?.columns as ColumnKey[] | undefined,
    });
    nav('/files');
  };
}

/** 符合常用筛选条件的份数（v1.5.6）；键以 documents 开头，文档有变化时随列表一起刷新。 */
function useViewCount(view: SavedView) {
  const filter = JSON.stringify(view.filter ?? {});
  return useQuery({
    queryKey: ['documents', 'view-count', view.id, filter],
    queryFn: async () => unwrap(await api.GET('/api/v1/documents', { params: { query: { filter: filter === '{}' ? undefined : filter, limit: 1 } } })).total,
    staleTime: 30_000,
  });
}

/** drag：拖动调整顺序的事件（v1.6.5）；onMove：菜单里的上移、下移，到头时不传。 */
export function SavedViewItem({ view, active, onApply, drag, onMove }: {
  view: SavedView; active: boolean; onApply: () => void;
  drag?: React.HTMLAttributes<HTMLDivElement> & { draggable?: boolean }; onMove?: { up?: () => void; down?: () => void };
}) {
  const app = useApp();
  const me = useMe().data?.user.id;
  const role = useRole();
  const canEdit = role.isOwner || (role.canWrite && view.createdBy === me);
  const [menu, setMenu] = useState<HTMLElement | null>(null);
  const total = useViewCount(view).data;
  const [renaming, setRenaming] = useState(false);
  const [sharing, setSharing] = useState(false);
  const del = useDeleteView();
  const update = useUpdateView();
  // 用全部文件当前的条件、排序和显示方式覆盖这个常用筛选
  const files = app.s.docLists.files;
  const refresh = () => {
    if (!files) return;
    const f = toFilter(files);
    update.mutate({ id: view.id, patch: { filter: f ? JSON.parse(f) : {}, sort: `${files.sort}:${files.dir}`, display: viewDisplay(files) } }, {
      onSuccess: () => app.toast(t('已用当前的设置更新“{name}”', { name: view.name })), onError: (e) => app.toast(e.message),
    });
  };
  const remove = () => app.confirm({
    title: t('删除常用筛选“{name}”？', { name: view.name }), body: t('只删除这个常用筛选，文档不受影响。空间里的其他成员也将看不到它。'), verb: t('删除'),
    run: () => { app.confirm(null); del.mutate(view.id, { onSuccess: () => app.toast(t('已删除常用筛选“{name}”', { name: view.name })), onError: (e) => app.toast(e.message) }); },
  });
  return (
    <>
      <ListItemButton selected={active} onClick={onApply} title={view.creatorName ? t('{name} 创建', { name: view.creatorName }) : undefined} {...drag}
        sx={{ flex: 'none', mb: 0.25, px: 1.625, '& .more': { opacity: menu ? 1 : 0 }, '&:hover .more, &:focus-within .more': { opacity: 1 } }}>
        <ListItemIcon sx={{ minWidth: 32, color: 'text.secondary' }}><ListFilter size={18} strokeWidth={SW} /></ListItemIcon>
        <ListItemText primary={view.name} slotProps={{ primary: { noWrap: true, sx: { fontSize: 14, fontWeight: active ? 500 : 400 } } }} />
        {total !== undefined && <Typography aria-label={t('{n} 份', { n: total })} sx={{ fontSize: 12, color: 'text.secondary', ml: 1 }}>{total}</Typography>}
        {canEdit && (
          <IconButton className="more" size="small" aria-label={t('“{name}”的更多操作', { name: view.name })} onClick={(e) => { e.stopPropagation(); setMenu(e.currentTarget); }} sx={{ ml: 0.5, my: -0.5, transition: 'opacity 150ms' }}>
            <Ellipsis size={16} strokeWidth={SW} />
          </IconButton>
        )}
      </ListItemButton>
      <Menu anchorEl={menu} open={!!menu} onClose={() => setMenu(null)}>
        <MenuItem disabled={!active || !files} onClick={() => { setMenu(null); refresh(); }} sx={{ minWidth: 140 }}>{t('用当前的设置更新')}</MenuItem>
        <MenuItem onClick={() => { setMenu(null); setRenaming(true); }}>{t('重命名')}</MenuItem>
        {onMove?.up && <MenuItem onClick={() => { setMenu(null); onMove.up!(); }}>{t('上移')}</MenuItem>}
        {onMove?.down && <MenuItem onClick={() => { setMenu(null); onMove.down!(); }}>{t('下移')}</MenuItem>}
        {role.isOwner && <MenuItem onClick={() => { setMenu(null); setSharing(true); }}>{t('共享给其他空间')}</MenuItem>}
        <MenuItem onClick={() => { setMenu(null); remove(); }} sx={{ color: 'error.main' }}>{t('删除')}</MenuItem>
      </Menu>
      {renaming && <RenameDialog view={view} onClose={() => setRenaming(false)} />}
      {sharing && <CollectionDialog open filter={view.filter as Record<string, unknown>} defaultName={view.name} onClose={() => setSharing(false)} />}
    </>
  );
}

function RenameDialog({ view, onClose }: { view: SavedView; onClose: () => void }) {
  const update = useUpdateView();
  const [name, setName] = useState(view.name);
  const [error, setError] = useState('');
  const save = () => update.mutate({ id: view.id, patch: { name: name.trim() } }, { onSuccess: onClose, onError: (e) => setError(e.message) });
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="rename-view-title">
      <DialogTitle id="rename-view-title">{t('重命名常用筛选')}</DialogTitle>
      <DialogContent>
        <Box sx={{ pt: 1 }}>
          <TextField autoFocus fullWidth label={t('名称')} value={name} error={!!error} helperText={error || ' '} slotProps={{ htmlInput: { maxLength: 32 } }}
            onChange={(e) => { setName(e.target.value); setError(''); }} onKeyDown={(e) => { if (e.key === 'Enter' && name.trim()) save(); }} />
        </Box>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!name.trim() || name.trim() === view.name || update.isPending} onClick={save}>{t('保存')}</Button>
      </DialogActions>
    </Dialog>
  );
}

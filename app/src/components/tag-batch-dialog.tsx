// 批量改标签（v1.5.6，参照 paperless-ngx 的批量编辑）：列出所有标签，显示选中的文档里有几份带着它（全有打勾、部分是横线），
// 一次勾上、去掉多个，再一起应用。按筛选条件“选择全部”时文档没有都载入，不显示份数，只能选“加上 / 去掉”。
import { useEffect, useMemo, useState } from 'react';
import { Box, Button, Checkbox, Dialog, DialogActions, DialogContent, DialogTitle, InputAdornment, List, ListItemButton, ListItemIcon, ListItemText, TextField, Typography } from '@mui/material';
import { Search } from 'lucide-react';
import { useBulk, type DocumentSummary } from '../api/documents';
import { useApp } from '../store/app-store';
import { SW, TagDot } from './icons';
import type { TaxonomyIndex } from './pickers';
import { t, tc } from '../i18n';

type Change = 'add' | 'remove';

/** 选中的文档里带着各个标签的份数；选中的文档没有都载入时为 null。 */
export function tagCounts(selection: { ids: number[] } | { filter: unknown }, items: DocumentSummary[]): Map<number, number> | null {
  if (!('ids' in selection)) return null;
  const byId = new Map(items.map((d) => [d.id, d]));
  const counts = new Map<number, number>();
  for (const id of selection.ids) {
    const d = byId.get(id);
    if (!d) return null;
    for (const tag of d.tagIds) counts.set(tag, (counts.get(tag) ?? 0) + 1);
  }
  return counts;
}

export function TagBatchDialog({ open, onClose, onDone, selection, items, count, ix }: {
  open: boolean; onClose: () => void; onDone: () => void;
  selection: { ids: number[] } | { filter: Record<string, unknown> }; items: DocumentSummary[]; count: number; ix: TaxonomyIndex;
}) {
  const app = useApp();
  const bulk = useBulk();
  const [q, setQ] = useState('');
  const [changes, setChanges] = useState<Map<number, Change>>(new Map());
  useEffect(() => { if (open) { setQ(''); setChanges(new Map()); } }, [open]);
  const counts = useMemo(() => (open ? tagCounts(selection, items) : null), [open, selection, items]);

  const query = q.trim().toLowerCase();
  const shown = ix.tagTree.filter(({ tag, path }) => !query || path.toLowerCase().includes(query) || tag.sortKey.includes(query.replace(/\s+/g, '')));
  // 点一下换到下一种状态：全有的 → 去掉 → 不变；没有的 → 加上 → 不变；部分有的、份数未知的 → 加上 → 去掉 → 不变
  const cycle = (id: number) => setChanges((m) => {
    const n = counts?.get(id) ?? 0;
    const order: (Change | undefined)[] = counts && n === count ? [undefined, 'remove'] : counts && n === 0 ? [undefined, 'add'] : [undefined, 'add', 'remove'];
    const next = order[(order.indexOf(m.get(id)) + 1) % order.length];
    const out = new Map(m);
    if (next) out.set(id, next); else out.delete(id);
    return out;
  });

  const add = [...changes].filter(([, c]) => c === 'add').map(([id]) => id);
  const remove = [...changes].filter(([, c]) => c === 'remove').map(([id]) => id);
  const apply = () => bulk.mutate({ ...selection, edit: { addTagIds: add, removeTagIds: remove } }, {
    onSuccess: (r) => {
      app.toast(add.length && remove.length
        ? t('已为 {n} 份文档加上 {add} 个标签、去掉 {remove} 个标签', { n: r.count, add: add.length, remove: remove.length })
        : add.length ? t('已为 {n} 份文档加上 {add} 个标签', { n: r.count, add: add.length })
          : t('已为 {n} 份文档去掉 {remove} 个标签', { n: r.count, remove: remove.length }));
      onClose(); onDone();
    },
    onError: (e) => app.toast(e.message),
  });

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="tag-batch-title">
      <DialogTitle id="tag-batch-title">{t('改 {n} 份文档的标签', { n: count })}</DialogTitle>
      <DialogContent sx={{ pb: 0 }}>
        <TextField size="small" fullWidth label={t('搜索标签')} value={q} onChange={(e) => setQ(e.target.value)} sx={{ mt: 1 }}
          slotProps={{ input: { startAdornment: <InputAdornment position="start"><Search size={16} strokeWidth={SW} /></InputAdornment> } }} />
        {!counts && <Typography variant="caption" component="p" sx={{ color: 'text.secondary', mt: 1 }}>{t('选中的文档较多，没有列出已有的标签；点一下“加上”，再点一下“去掉”。')}</Typography>}
        <List dense aria-label={tc('复数', '标签')} sx={{ mt: 1, maxHeight: 360, overflow: 'auto', mx: -1 }}>
          {shown.map(({ tag, depth, path }) => {
            const n = counts?.get(tag.id) ?? 0;
            const change = changes.get(tag.id);
            const checked = change === 'add' || (!change && !!counts && n === count);
            const partial = !change && !!counts && n > 0 && n < count;
            const status = change === 'add' ? t('将加上') : change === 'remove' ? t('将去掉') : counts ? (n ? t('{n} / {total} 份', { n, total: count }) : '') : '';
            return (
              <ListItemButton key={tag.id} onClick={() => cycle(tag.id)} title={path} sx={{ pl: 1 + depth * 2, borderRadius: 1 }}>
                <ListItemIcon sx={{ minWidth: 32 }}>
                  <Checkbox size="small" edge="start" tabIndex={-1} disableRipple checked={checked} indeterminate={partial}
                    slotProps={{ input: { 'aria-label': status ? t('{name}（{status}）', { name: tag.name, status }) : tag.name } }} sx={{ p: 0.25 }} />
                </ListItemIcon>
                <ListItemText primary={<Box sx={{ display: 'flex', alignItems: 'center', minWidth: 0 }}><TagDot name={tag.name} color={tag.color} /></Box>} />
                <Typography sx={{ fontSize: 12, ml: 1, flex: 'none', color: change === 'remove' ? 'error.main' : change === 'add' ? 'primary.main' : 'text.secondary' }}>{status}</Typography>
              </ListItemButton>
            );
          })}
          {!shown.length && <Typography variant="body2" sx={{ color: 'text.secondary', px: 1, py: 2 }}>{t('没有找到标签')}</Typography>}
        </List>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!changes.size || bulk.isPending} onClick={apply}>{t('应用')}</Button>
      </DialogActions>
    </Dialog>
  );
}

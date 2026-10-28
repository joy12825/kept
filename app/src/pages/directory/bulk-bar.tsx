// 目录的批量整理（v1.5.8，参照 paperless-ngx 的批量编辑）：选中几个往来方、类型或标签，一起删除，或者合并到其中一个。
// “往来方按发件人”开着时往来方会多出不少，用这里清理。
import { useState } from 'react';
import { Button, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, IconButton, Paper, Radio, RadioGroup, Tooltip, Typography } from '@mui/material';
import { X } from 'lucide-react';
import { KIND_LABEL, useBulkDeleteTaxonomy, useMergeTaxonomy, type DirectoryKind } from '../../api/taxonomy';
import { useApp } from '../../store/app-store';
import { SW } from '../../components/icons';
import { currentLang, t } from '../../i18n';

/** “3 个往来方”：英文里名词要按数量变。 */
const NOUN: Record<DirectoryKind, (n: number) => string> = {
  correspondents: (n) => t('{n} 个往来方', { n }),
  'document-types': (n) => t('{n} 个类型', { n }),
  tags: (n) => t('{n} 个标签', { n }),
  persons: (n) => t('{n} 个家人', { n }),
};
/** 几句话连起来：英文句子之间要空格。 */
const sentences = (xs: string[]) => xs.filter(Boolean).join(currentLang() === 'en' ? ' ' : '');

export interface Picked { id: number; name: string; documentCount: number }

export function DirectoryBulkBar({ kind, picked, onDone }: { kind: DirectoryKind; picked: Picked[]; onDone: () => void }) {
  const app = useApp();
  const del = useBulkDeleteTaxonomy(kind);
  const merge = useMergeTaxonomy(kind);
  const [merging, setMerging] = useState(false);
  const docs = picked.reduce((n, p) => n + p.documentCount, 0);
  const remove = () => app.confirm({
    title: t('删除 {items}？', { items: NOUN[kind](picked.length) }),
    body: sentences([docs ? t('{n} 份文档会不再带着它们，文档本身不删除。', { n: docs }) : t('它们还没有文档。'), kind === 'tags' ? t('子标签会移到上一级。') : '', t('删除后不能恢复。')]),
    verb: t('删除'),
    run: () => del.mutate(picked.map((p) => p.id), {
      onSuccess: (r) => { app.confirm(null); app.toast(t('已删除 {items}', { items: NOUN[kind](r.count) })); onDone(); },
      onError: (e) => { app.confirm(null); app.toast(e.message); },
    }),
  });
  return (
    <>
      <Paper elevation={2} role="toolbar" aria-label={t('批量整理')} sx={{ position: 'fixed', left: '50%', bottom: 24, transform: 'translateX(-50%)', zIndex: 1200, display: 'flex', alignItems: 'center', gap: 0.5, p: 1, pl: 2, borderRadius: 3, bgcolor: 'surface.container', maxWidth: 'calc(100vw - 32px)' }}>
        <Typography variant="h6" sx={{ mr: 1 }}>{t('已选 {n} 个', { n: picked.length })}</Typography>
        <Tooltip title={picked.length < 2 ? t('至少选两个才能合并') : ''}><span><Button size="small" disabled={picked.length < 2} onClick={() => setMerging(true)}>{t('合并')}</Button></span></Tooltip>
        <Button size="small" color="error" disabled={!picked.length || del.isPending} onClick={remove}>{t('删除')}</Button>
        <Tooltip title={t('取消选择')}><IconButton size="small" aria-label={t('取消选择')} onClick={onDone}><X size={18} strokeWidth={SW} /></IconButton></Tooltip>
      </Paper>
      {merging && <MergeDialog kind={kind} picked={picked} busy={merge.isPending} onClose={() => setMerging(false)}
        onMerge={(keep) => merge.mutate({ keep, merge: picked.map((p) => p.id).filter((id) => id !== keep) }, {
          onSuccess: (r) => { setMerging(false); app.toast(t('已合并，{n} 份文档改为“{name}”', { n: r.documents, name: picked.find((p) => p.id === keep)?.name ?? '' })); onDone(); },
          onError: (e) => app.toast(e.message),
        })} />}
    </>
  );
}

function MergeDialog({ kind, picked, busy, onClose, onMerge }: { kind: DirectoryKind; picked: Picked[]; busy: boolean; onClose: () => void; onMerge: (keep: number) => void }) {
  // 默认保留文档最多的那个
  const [keep, setKeep] = useState(() => [...picked].sort((a, b) => b.documentCount - a.documentCount)[0].id);
  const label = KIND_LABEL[kind];
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="merge-title">
      <DialogTitle id="merge-title">{t('合并 {items}', { items: NOUN[kind](picked.length) })}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>
          {kind === 'tags'
            ? t('保留哪一个？其余的{label}会删除，它们的文档改为保留的这个，子标签移到它下面；抽屉规则里提到的也会换过来。', { label: label.toLowerCase() })
            : t('保留哪一个？其余的{label}会删除，它们的文档改为保留的这个；抽屉规则里提到的也会换过来。', { label: label.toLowerCase() })}
        </Typography>
        <RadioGroup value={keep} onChange={(e) => setKeep(Number(e.target.value))} aria-label={t('保留的{label}', { label })}>
          {picked.map((p) => <FormControlLabel key={p.id} value={p.id} control={<Radio />} label={t('{name}（{n} 份）', { name: p.name, n: p.documentCount })} />)}
        </RadioGroup>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={busy} onClick={() => onMerge(keep)}>{t('合并')}</Button>
      </DialogActions>
    </Dialog>
  );
}

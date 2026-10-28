// 清单里的一项材料（v1.6.7，v1.6.8 改版）：状态写在标题旁，找到的文档列在下面。
// 缺的时候点“补充材料”：对话框里先写明这一项要什么，再上传新文件（可以用项名作标题）或从已有文档里选几份。
// 熟练的话也可以直接把文件拖到卡片上（用文件名作标题）。上传的文件照常进入待整理，同时算作这一项；手动放入的可以移出。
import { useEffect, useMemo, useRef, useState, type DragEvent } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import {
  Alert, Box, Button, ButtonBase, Card, Checkbox, Chip, Dialog, DialogActions, DialogContent, DialogTitle, Divider, FormControlLabel, IconButton,
  InputBase, LinearProgress, Link, Paper, Stack, Tooltip, Typography,
} from '@mui/material';
import { CircleAlert, CircleCheck, CircleDashed, FileText, Plus, Search, Upload, X } from 'lucide-react';
import { uploadToChecklistItem, useChecklistDocument, type ChecklistCheckItem } from '../../api/checklists';
import { api, unwrap, type ApiError } from '../../api/client';
import { useJobs, type DocumentSummary } from '../../api/documents';
import { useApp } from '../../store/app-store';
import { docHref, docLinkClick } from '../../components/doc-link';
import { SW } from '../../components/icons';
import { useTaxonomyIndex } from '../../components/pickers';
import { ACCEPT } from '../../components/uploads';
import { t } from '../../i18n';

/** 状态：标题旁的小标签。 */
function StatusChip({ it }: { it: ChecklistCheckItem }) {
  if (it.status === 'ok') return <Chip size="small" color="success" variant="outlined" icon={<CircleCheck size={14} strokeWidth={SW} />} label={it.count > 1 ? t('{n} 份都有', { n: it.count }) : t('齐了')} />;
  if (it.status === 'partial') return <Chip size="small" color="warning" variant="outlined" icon={<CircleAlert size={14} strokeWidth={SW} />} label={t('还差 {n} 份', { n: it.count - it.documents.length })} />;
  return <Chip size="small" variant="outlined" icon={<CircleDashed size={14} strokeWidth={SW} />} label={t('还没有')} />;
}

/** 这一项要什么：“要 3 份，还差 1 份”。 */
const need = (it: ChecklistCheckItem) => {
  const lack = Math.max(0, it.count - it.documents.length);
  if (!lack) return it.count > 1 ? t('要 {n} 份，都有了', { n: it.count }) : t('已经有了');
  return it.count > 1 ? t('要 {n} 份，还差 {lack} 份', { n: it.count, lack }) : t('要 1 份，还没有');
};

const MAX_SIZE = 200 << 20;

interface Pending { key: string; name: string; pct: number; jobId?: number; error?: string }

export function ChecklistItemCard({ checklistId, it, canWrite, onOpen }: {
  checklistId: number; it: ChecklistCheckItem; canWrite: boolean; onOpen: (docId: number) => void;
}) {
  const app = useApp();
  const qc = useQueryClient();
  const docs = useChecklistDocument(checklistId);
  const [pending, setPending] = useState<Pending[]>([]);
  const [over, setOver] = useState(false);
  const [adding, setAdding] = useState(false);
  const refresh = () => qc.invalidateQueries({ queryKey: ['documents', 'checklist', checklistId] });
  const patch = (key: string, p: Partial<Pending>) => setPending((xs) => xs.map((x) => (x.key === key ? { ...x, ...p } : x)));
  const drop = (key: string) => setPending((xs) => xs.filter((x) => x.key !== key));

  // 上传完的文件在后台识别：任务结束后从“正在识别”里去掉，清单随之刷新
  const waiting = pending.some((p) => p.jobId && !p.error);
  const jobs = useJobs();
  useEffect(() => {
    if (!waiting || !jobs.data) return;
    for (const p of pending) {
      const job = p.jobId ? jobs.data.find((j) => j.id === p.jobId) : undefined;
      if (job?.status === 'succeeded') { drop(p.key); void refresh(); }
      else if (job?.status === 'failed') patch(p.key, { error: job.error ?? t('没有识别成功') });
    }
  }, [jobs.data, waiting]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!waiting) return;
    const timer = window.setInterval(() => void jobs.refetch(), 2000);
    return () => window.clearInterval(timer);
  }, [waiting]); // eslint-disable-line react-hooks/exhaustive-deps

  /** 上传；titled 时用项名作标题（几个文件时加上序号）。 */
  const upload = (files: File[], titled = false) => {
    files.forEach((file, i) => {
      const key = `${Date.now()}-${Math.random()}`;
      setPending((xs) => [...xs, { key, name: file.name, pct: 0 }]);
      if (file.size > MAX_SIZE) { patch(key, { error: t('文件超过 200 MB 的上限') }); return; }
      if (file.size === 0) { patch(key, { error: t('文件是空的') }); return; }
      const title = titled ? (files.length > 1 ? `${it.label}（${i + 1}）` : it.label) : undefined;
      uploadToChecklistItem(checklistId, it.id, file, (pct) => patch(key, { pct }), title)
        .then((r) => {
          if (r.jobId) { patch(key, { pct: 100, jobId: r.jobId }); return; }
          drop(key); void refresh(); app.toast(t('这份文件已经在空间里，已放进“{name}”', { name: it.label }));
        })
        .catch((e: ApiError) => patch(key, { error: e.message }));
    });
  };
  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation(); // 不交给全局的拖放上传
    setOver(false);
    if (canWrite && e.dataTransfer.files.length) upload([...e.dataTransfer.files]);
  };
  const remove = (d: DocumentSummary) => docs.mutate({ itemId: it.id, documentId: d.id, remove: true }, {
    onSuccess: () => app.toast(t('已把《{title}》移出“{name}”', { title: d.title, name: it.label })), onError: (e) => app.toast(e.message),
  });
  const lacking = it.status !== 'ok';

  return (
    <Card component="li" variant="outlined" id={`item-${it.id}`} aria-label={it.label}
      onDragOver={canWrite ? (e) => { e.preventDefault(); e.stopPropagation(); setOver(true); } : undefined}
      onDragLeave={canWrite ? (e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setOver(false); } : undefined}
      onDrop={canWrite ? onDrop : undefined}
      sx={{ position: 'relative', p: 2, scrollMarginTop: 80, bgcolor: lacking ? 'surface.containerLow' : 'background.paper',
        outline: over ? '2px dashed' : 'none', outlineColor: 'primary.main', outlineOffset: -2 }}>
      <Stack direction="row" spacing={1.5} sx={{ alignItems: 'flex-start' }}>
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Stack direction="row" spacing={1} useFlexGap sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
            <Typography sx={{ fontWeight: 500, fontSize: 16 }}>{it.label}</Typography>
            <StatusChip it={it} />
          </Stack>
          {(it.note || it.count > 1) && (
            <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.25 }}>{[it.count > 1 ? t('要 {n} 份', { n: it.count }) : '', it.note ?? ''].filter(Boolean).join(' · ')}</Typography>
          )}
        </Box>
        {canWrite && (
          <Button size="small" variant={lacking ? 'contained' : 'text'} startIcon={<Plus size={14} strokeWidth={SW} />} sx={{ flex: 'none' }} onClick={() => setAdding(true)}>
            {t('补充材料')}
          </Button>
        )}
      </Stack>

      {it.documents.length > 0 && (
        <Box component="ul" sx={{ listStyle: 'none', p: 0, m: 0, mt: 1, borderTop: 1, borderColor: 'divider' }}>
          {it.documents.map((d) => {
            const manual = (it.attachedIds ?? []).includes(d.id);
            return (
              <Stack component="li" key={d.id} direction="row" spacing={1} sx={{ alignItems: 'center', minWidth: 0, minHeight: 36, borderBottom: 1, borderColor: 'divider' }}>
                <Box sx={{ color: 'text.secondary', display: 'flex', flex: 'none' }}><FileText size={16} strokeWidth={SW} /></Box>
                <Link component={RouterLink} to={docHref(d.id)} onClick={docLinkClick(() => onOpen(d.id))} underline="hover" variant="body2" noWrap sx={{ minWidth: 0, flex: 1 }}>{d.title}</Link>
                {manual && <Typography variant="caption" sx={{ color: 'text.secondary', flex: 'none' }}>{t('手动放入')}</Typography>}
                {it.expiredIds.includes(d.id) && <Chip size="small" color="warning" variant="outlined" label={t('已过期 {date}', { date: d.dueDate ?? '' })} sx={{ flex: 'none' }} />}
                <Typography variant="caption" sx={{ color: 'text.secondary', flex: 'none', fontVariantNumeric: 'tabular-nums' }}>{d.createdDate}</Typography>
                {canWrite && (manual ? (
                  <Tooltip title={t('移出这一项（文档本身保留）')}>
                    <IconButton size="small" aria-label={t('把《{title}》移出这一项', { title: d.title })} onClick={() => remove(d)} sx={{ flex: 'none' }}><X size={14} strokeWidth={SW} /></IconButton>
                  </Tooltip>
                ) : <Box sx={{ width: 30, flex: 'none' }} />)}
              </Stack>
            );
          })}
        </Box>
      )}

      {pending.map((p) => (
        <Box key={p.key} sx={{ mt: 1 }}>
          {p.error ? (
            <Alert severity="error" sx={{ py: 0 }} action={<IconButton size="small" aria-label={t('关闭')} onClick={() => drop(p.key)}><X size={14} strokeWidth={SW} /></IconButton>}>
              {t('{name}：{error}', { name: p.name, error: p.error })}
            </Alert>
          ) : (
            <>
              <Typography variant="caption" sx={{ color: 'text.secondary' }}>{p.name} · {p.jobId ? t('正在识别…') : t('上传中 {pct}%', { pct: p.pct })}</Typography>
              <LinearProgress variant={p.jobId ? 'indeterminate' : 'determinate'} value={p.pct} sx={{ height: 4, borderRadius: 2 }} />
            </>
          )}
        </Box>
      ))}

      {over && (
        <Box aria-hidden sx={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', bgcolor: 'action.hover', borderRadius: 'inherit', pointerEvents: 'none' }}>
          <Typography sx={{ fontWeight: 500, color: 'primary.main' }}>{t('松开，上传到“{name}”', { name: it.label })}</Typography>
        </Box>
      )}
      {adding && <SupplementDialog checklistId={checklistId} it={it} onUpload={upload} onClose={() => setAdding(false)} />}
    </Card>
  );
}

/** 补充材料：先说这一项要什么，再上传新文件或从已有文档里选。 */
function SupplementDialog({ checklistId, it, onUpload, onClose }: {
  checklistId: number; it: ChecklistCheckItem; onUpload: (files: File[], titled: boolean) => void; onClose: () => void;
}) {
  const app = useApp();
  const ix = useTaxonomyIndex();
  const docs = useChecklistDocument(checklistId);
  const input = useRef<HTMLInputElement>(null);
  const [titled, setTitled] = useState(true);
  const [over, setOver] = useState(false);
  const [q, setQ] = useState(it.label);
  const [results, setResults] = useState<DocumentSummary[] | null>(null);
  const [picked, setPicked] = useState<number[]>([]);
  const [saving, setSaving] = useState(false);
  const lack = Math.max(0, it.count - it.documents.length);
  const search = useMemo(() => {
    let timer = 0;
    return (text: string) => {
      window.clearTimeout(timer);
      timer = window.setTimeout(async () => {
        const filter = text.trim() ? JSON.stringify({ text: text.trim() }) : undefined;
        try {
          const page = unwrap(await api.GET('/api/v1/documents', { params: { query: { filter, sort: text.trim() ? 'relevance' : 'added', dir: 'desc', limit: 20 } } }));
          setResults(page.items.filter((d) => !it.documents.some((x) => x.id === d.id)));
        } catch {
          setResults([]); // 搜不到就当没有可选的文档，不让错误漏出去
        }
      }, 250);
    };
  }, [it.documents]);
  useEffect(() => { search(q); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const send = (files: FileList | File[] | null) => {
    if (!files || !files.length) return;
    onUpload([...files], titled);
    onClose();
  };
  const toggle = (id: number) => setPicked((xs) => (xs.includes(id) ? xs.filter((x) => x !== id) : [...xs, id]));
  const put = async () => {
    setSaving(true);
    try {
      for (const documentId of picked) await docs.mutateAsync({ itemId: it.id, documentId });
      app.toast(t('已放进“{name}” {n} 份', { name: it.label, n: picked.length }));
      onClose();
    } catch (e) {
      app.toast((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="sm" aria-labelledby="supplement-title">
      <DialogTitle id="supplement-title">{t('补充“{name}”', { name: it.label })}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: lack ? 'warning.main' : 'text.secondary' }}>{need(it)}</Typography>
        {it.note && <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('备注：{note}', { note: it.note })}</Typography>}

        <Typography variant="h6" sx={{ mt: 2.5, mb: 1 }}>{t('上传新文件')}</Typography>
        <ButtonBase component="div" role="button" aria-label={t('选择要上传的文件')} onClick={() => input.current?.click()}
          onDragOver={(e) => { e.preventDefault(); e.stopPropagation(); setOver(true); }} onDragLeave={() => setOver(false)}
          onDrop={(e) => { e.preventDefault(); e.stopPropagation(); setOver(false); send(e.dataTransfer.files); }}
          sx={{ width: '100%', flexDirection: 'column', gap: 0.5, py: 3, border: '1px dashed', borderColor: over ? 'primary.main' : 'divider', borderRadius: 2,
            bgcolor: over ? 'action.hover' : 'transparent', color: 'text.secondary' }}>
          <Upload size={24} strokeWidth={SW} />
          <Typography variant="body2" sx={{ color: 'text.primary' }}>{t('把文件拖到这里，或点击选择')}</Typography>
          <Typography variant="caption">{t('PDF、照片、Office 文档等；手机上可以直接拍照')}</Typography>
        </ButtonBase>
        <input ref={input} hidden multiple type="file" accept={ACCEPT} aria-label={t('上传到“{name}”', { name: it.label })} onChange={(e) => { send(e.target.files); e.target.value = ''; }} />
        <FormControlLabel sx={{ mt: 0.5 }} control={<Checkbox size="small" checked={titled} onChange={(e) => setTitled(e.target.checked)} />}
          label={<Typography variant="body2">{t('用“{name}”作标题（否则用文件名）', { name: it.label })}</Typography>} />
        <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>{t('上传的文件会照常进入待整理，同时算作这一项。')}</Typography>

        <Divider sx={{ my: 2.5 }}>{t('或者')}</Divider>

        <Typography variant="h6" sx={{ mb: 1 }}>{t('从已有文档里选')}</Typography>
        <Paper elevation={0} sx={{ display: 'flex', alignItems: 'center', gap: 1, height: 40, px: 1.5, borderRadius: 5, bgcolor: 'surface.containerHigh', color: 'text.secondary' }}>
          <Search size={16} strokeWidth={SW} />
          <InputBase value={q} onChange={(e) => { setQ(e.target.value); search(e.target.value); }} placeholder={t('搜索标题、正文')} inputProps={{ 'aria-label': t('搜索已有文档') }} sx={{ flex: 1, fontSize: 14 }} />
        </Paper>
        <Box role="listbox" aria-label={t('已有文档')} aria-multiselectable sx={{ mt: 1, maxHeight: 280, overflowY: 'auto' }}>
          {results === null && <LinearProgress sx={{ my: 2 }} />}
          {results?.length === 0 && <Typography variant="body2" sx={{ color: 'text.secondary', py: 2, textAlign: 'center' }}>{t('没有找到文档，换个词试试')}</Typography>}
          {results?.map((d) => {
            const on = picked.includes(d.id);
            const type = d.documentTypeId ? ix.types.get(d.documentTypeId)?.name : undefined;
            return (
              <ButtonBase key={d.id} role="option" aria-selected={on} onClick={() => toggle(d.id)}
                sx={{ width: '100%', justifyContent: 'flex-start', gap: 1, px: 1, py: 0.75, borderRadius: 1.5, textAlign: 'left', bgcolor: on ? 'action.selected' : 'transparent', '&:hover': { bgcolor: on ? 'action.selected' : 'action.hover' } }}>
                <Checkbox size="small" checked={on} tabIndex={-1} sx={{ p: 0.25 }} />
                <Typography variant="body2" noWrap sx={{ flex: 1, minWidth: 0 }}>{d.title}</Typography>
                {type && <Typography variant="caption" sx={{ color: 'text.secondary', flex: 'none' }}>{type}</Typography>}
                <Typography variant="caption" sx={{ color: 'text.secondary', flex: 'none' }}>{d.createdDate}</Typography>
              </ButtonBase>
            );
          })}
        </Box>
        <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary', mt: 0.5 }}>
          {t('放进来的会一直算作这一项，不管标题和内容里有没有“{name}”。', { name: it.label })}{lack > 1 && t('还差 {n} 份，可以多选。', { n: lack })}
        </Typography>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!picked.length || saving} onClick={() => void put()}>{picked.length ? t('放进来（{n}）', { n: picked.length }) : t('放进来')}</Button>
      </DialogActions>
    </Dialog>
  );
}

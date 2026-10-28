// PDF 页面编辑：旋转、删除页面、调整页序（v1.3.9），拆分，合并（阶段 12）。原件不变：编辑只重新生成可搜索版。
import { Fragment, useEffect, useMemo, useRef, useState } from 'react';
import {
  Box, Button, Checkbox, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, IconButton,
  Stack, Tooltip, Typography, useMediaQuery, useTheme,
} from '@mui/material';
import { ArrowDown, ArrowUp, ChevronLeft, ChevronRight, RotateCcw, RotateCw, Scissors, Trash2, Undo2 } from 'lucide-react';
import * as pdfjs from 'pdfjs-dist';
import type { PDFDocumentProxy } from 'pdfjs-dist';
import { fileUrl, useMergeDocuments, useSetPages, useSplitDocument, type DocumentDetail } from '../api/documents';
import { useApp } from '../store/app-store';
import { SW } from './icons';
import { t } from '../i18n';
import './pdf-preview'; // 设置 pdf.js 的 worker

/** 载入一份 PDF（失败时 error 为 true）。 */
function usePdf(url: string, open: boolean) {
  const [doc, setDoc] = useState<PDFDocumentProxy | null>(null);
  const [error, setError] = useState(false);
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setDoc(null); setError(false);
    const task = pdfjs.getDocument({ url, withCredentials: true });
    task.promise.then((d) => { if (!cancelled) setDoc(d); }, () => { if (!cancelled) setError(true); });
    return () => { cancelled = true; void task.destroy(); };
  }, [url, open]);
  return { doc, error };
}

const TILE = 132;

/** 一页的缩略图，放在正方形的格子里，按 rotate 顺时针旋转。 */
function Thumb({ doc, n, rotate = 0, dim = false }: { doc: PDFDocumentProxy; n: number; rotate?: number; dim?: boolean }) {
  const canvas = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    let cancelled = false;
    let task: { cancel: () => void; promise: Promise<unknown> } | null = null;
    void doc.getPage(n).then((page) => {
      if (cancelled || !canvas.current) return;
      const base = page.getViewport({ scale: 1 });
      const vp = page.getViewport({ scale: (TILE - 16) / Math.max(base.width, base.height) });
      const ratio = window.devicePixelRatio || 1;
      const c = canvas.current;
      c.width = Math.floor(vp.width * ratio); c.height = Math.floor(vp.height * ratio);
      c.style.width = `${vp.width}px`; c.style.height = `${vp.height}px`;
      task = page.render({ canvas: c, viewport: vp, transform: ratio !== 1 ? [ratio, 0, 0, ratio, 0, 0] : undefined });
      task.promise.catch(() => { /* 关闭时被取消 */ });
    });
    return () => { cancelled = true; task?.cancel(); };
  }, [doc, n]);
  return (
    <Box sx={{ width: TILE, height: TILE, display: 'flex', alignItems: 'center', justifyContent: 'center', bgcolor: 'surface.containerLow', borderRadius: 1.5, overflow: 'hidden' }}>
      <Box component="canvas" ref={canvas} aria-label={t('第 {n} 页', { n })}
        sx={{ boxShadow: 1, bgcolor: '#fff', transform: `rotate(${rotate}deg)`, transition: 'transform 150ms', opacity: dim ? 0.25 : 1 }} />
    </Box>
  );
}

function Loading({ error }: { error: boolean }) {
  return (
    <Stack sx={{ alignItems: 'center', justifyContent: 'center', py: 6 }} spacing={1}>
      {error ? <Typography sx={{ color: 'error.main' }}>{t('PDF 没能打开，请稍后重试')}</Typography> : <CircularProgress size={28} />}
    </Stack>
  );
}

const editsOf = (d: DocumentDetail) => ({
  rotate: Object.fromEntries(Object.entries(d.pageEdits?.rotate ?? {}).map(([k, v]) => [Number(k), v])) as Record<number, number>,
  delete: new Set(d.pageEdits?.delete ?? []),
  order: d.pageEdits?.order ?? [],
});

/** 把第 from 个移到第 to 个的位置。 */
function moved<T>(xs: T[], from: number, to: number) {
  const out = [...xs];
  const [x] = out.splice(from, 1);
  out.splice(to, 0, x);
  return out;
}

/** 旋转、删除页面、调整页序。页码按编辑之前的 PDF；保存的是完整的目标状态。 */
export function PageEditorDialog({ d, open, onClose }: { d: DocumentDetail; open: boolean; onClose: () => void }) {
  const app = useApp();
  const full = useMediaQuery(useTheme().breakpoints.down('sm'));
  const base = fileUrl(d.id, 'preview', d.modifiedAt);
  const url = `${base}${base.includes('?') ? '&' : '?'}unedited=true`;
  const { doc, error } = usePdf(url, open);
  const save = useSetPages(d.id);
  const [rot, setRot] = useState<Record<number, number>>({});
  const [del, setDel] = useState<Set<number>>(new Set());
  const [order, setOrder] = useState<number[]>([]); // 编辑之前的页码，按显示顺序；空表示原来的顺序
  const [dragged, setDragged] = useState<number | null>(null);
  useEffect(() => { if (open) { const e = editsOf(d); setRot(e.rotate); setDel(e.delete); setOrder(e.order); } }, [open, d]);

  const n = doc?.numPages ?? 0;
  const shown = order.length === n ? order : Array.from({ length: n }, (_, i) => i + 1);
  const move = (from: number, to: number) => { if (to >= 0 && to < n && from !== to) setOrder(moved(shown, from, to)); };
  const natural = (o: number[]) => o.every((p, i) => p === i + 1);
  const orderOf = (o: number[]) => (o.length && !natural(o) ? o : []);
  const turn = (p: number, by: number) => setRot((r) => { const v = (((r[p] ?? 0) + by) % 360 + 360) % 360; const next = { ...r, [p]: v }; if (!v) delete next[p]; return next; });
  const toggle = (p: number) => setDel((s) => { const next = new Set(s); if (next.has(p)) next.delete(p); else next.add(p); return next; });
  const before = editsOf(d);
  const changed = JSON.stringify(Object.entries(rot).sort()) !== JSON.stringify(Object.entries(before.rotate).sort()) ||
    [...del].sort().join() !== [...before.delete].sort().join() || orderOf(shown).join() !== orderOf(before.order).join();
  const submit = (rotate: Record<number, number>, deleted: Set<number>, pageOrder: number[], done: string) => save.mutate(
    { rotate: Object.fromEntries(Object.entries(rotate).map(([k, v]) => [k, v])), delete: [...deleted].sort((a, b) => a - b), ...(orderOf(pageOrder).length ? { order: orderOf(pageOrder) } : {}) },
    { onSuccess: () => { app.toast(done); onClose(); }, onError: (e) => app.toast(e.message) },
  );

  return (
    <Dialog open={open} onClose={onClose} fullScreen={full} fullWidth maxWidth="md" aria-labelledby="page-editor-title">
      <DialogTitle id="page-editor-title">{t('编辑页面')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 2 }}>{t('旋转、删除页面或拖动调整顺序后，预览、缩略图和搜索都会随之改变；原件保持不变，随时可以恢复原样。')}</Typography>
        {!doc ? <Loading error={error} /> : (
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 2 }}>
            {shown.map((p, i) => {
              const gone = del.has(p);
              return (
                <Box key={p} sx={{ width: TILE, opacity: dragged === i ? 0.4 : 1, cursor: 'grab' }} draggable
                  onDragStart={(e) => { setDragged(i); e.dataTransfer.effectAllowed = 'move'; }}
                  onDragOver={(e) => { if (dragged !== null) { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; } }}
                  onDrop={(e) => { e.preventDefault(); if (dragged !== null) move(dragged, i); setDragged(null); }}
                  onDragEnd={() => setDragged(null)}>
                  <Box sx={{ position: 'relative' }}>
                    <Thumb doc={doc} n={p} rotate={rot[p] ?? 0} dim={gone} />
                    {p !== i + 1 && (
                      <Typography variant="caption" title={t('原来的第 {n} 页', { n: p })}
                        sx={{ position: 'absolute', left: 6, top: 6, px: 0.75, borderRadius: 1, bgcolor: 'primary.main', color: 'primary.contrastText', lineHeight: '18px' }}>{t('原 {n}', { n: p })}</Typography>
                    )}
                    {gone && <Typography variant="caption" sx={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'error.main', fontWeight: 500 }}>{t('将删除')}</Typography>}
                  </Box>
                  <Stack direction="row" sx={{ alignItems: 'center', mt: 0.5 }}>
                    <Typography variant="caption" sx={{ flex: 1, color: 'text.secondary' }}>{t('第 {n} 页', { n: i + 1 })}</Typography>
                    <Tooltip title={t('向左旋转')}><span><IconButton size="small" aria-label={t('第 {n} 页向左旋转', { n: i + 1 })} disabled={gone} onClick={() => turn(p, -90)}><RotateCcw size={16} strokeWidth={SW} /></IconButton></span></Tooltip>
                    <Tooltip title={t('向右旋转')}><span><IconButton size="small" aria-label={t('第 {n} 页向右旋转', { n: i + 1 })} disabled={gone} onClick={() => turn(p, 90)}><RotateCw size={16} strokeWidth={SW} /></IconButton></span></Tooltip>
                    <Tooltip title={gone ? t('不删除') : t('删除这一页')}>
                      <IconButton size="small" aria-label={gone ? t('不删除第 {n} 页', { n: i + 1 }) : t('删除第 {n} 页', { n: i + 1 })} onClick={() => toggle(p)}>
                        {gone ? <Undo2 size={16} strokeWidth={SW} /> : <Trash2 size={16} strokeWidth={SW} />}
                      </IconButton>
                    </Tooltip>
                  </Stack>
                  <Stack direction="row" sx={{ justifyContent: 'center' }}>
                    <Tooltip title={t('前移')}><span><IconButton size="small" aria-label={t('第 {n} 页前移', { n: i + 1 })} disabled={i === 0} onClick={() => move(i, i - 1)}><ChevronLeft size={16} strokeWidth={SW} /></IconButton></span></Tooltip>
                    <Tooltip title={t('后移')}><span><IconButton size="small" aria-label={t('第 {n} 页后移', { n: i + 1 })} disabled={i === n - 1} onClick={() => move(i, i + 1)}><ChevronRight size={16} strokeWidth={SW} /></IconButton></span></Tooltip>
                  </Stack>
                </Box>
              );
            })}
          </Box>
        )}
        {!!n && del.size >= n && <Typography variant="body2" sx={{ color: 'error.main', mt: 2 }}>{t('至少要保留一页。')}</Typography>}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        {d.pageEdits && <Button sx={{ mr: 'auto' }} disabled={save.isPending} onClick={() => submit({}, new Set(), [], t('已恢复原样'))}>{t('恢复原样')}</Button>}
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!doc || !changed || save.isPending || del.size >= n} onClick={() => submit(rot, del, shown, t('页面已保存'))}>{t('保存')}</Button>
      </DialogActions>
    </Dialog>
  );
}

/** 拆分：在页与页之间选择拆开的位置，每一段成为一份新文档。 */
export function SplitDialog({ d, open, onClose }: { d: DocumentDetail; open: boolean; onClose: () => void }) {
  const app = useApp();
  const full = useMediaQuery(useTheme().breakpoints.down('sm'));
  const { doc, error } = usePdf(fileUrl(d.id, 'preview', d.modifiedAt), open);
  const split = useSplitDocument(d.id);
  const [cuts, setCuts] = useState<Set<number>>(new Set());
  const [trash, setTrash] = useState(false);
  useEffect(() => { if (open) { setCuts(new Set()); setTrash(false); } }, [open]);
  const n = doc?.numPages ?? 0;
  const parts = useMemo(() => {
    const out: string[] = [];
    let from = 1;
    for (const c of [...cuts].sort((a, b) => a - b).concat(n)) { out.push(from === c ? t('第 {n} 页', { n: c }) : t('第 {from}–{to} 页', { from, to: c })); from = c + 1; }
    return out;
  }, [cuts, n]);
  const toggle = (p: number) => setCuts((s) => { const next = new Set(s); if (next.has(p)) next.delete(p); else next.add(p); return next; });
  const submit = () => split.mutate({ after: [...cuts].sort((a, b) => a - b), trashOriginal: trash }, {
    onSuccess: () => { app.toast(t('正在拆分为 {n} 份，新文档会出现在待整理', { n: cuts.size + 1 })); onClose(); },
    onError: (e) => app.toast(e.message),
  });

  return (
    <Dialog open={open} onClose={onClose} fullScreen={full} fullWidth maxWidth="md" aria-labelledby="split-title">
      <DialogTitle id="split-title">{t('拆分')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 2 }}>{t('点页与页之间的剪刀选择在哪里拆开。每一段会成为一份新文档，带上原来的类型、往来方、标签和日期。')}</Typography>
        {!doc ? <Loading error={error} /> : (
          <Box sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', rowGap: 2 }}>
            {Array.from({ length: n }, (_, i) => i + 1).map((p) => (
              <Fragment key={p}>
                <Box sx={{ width: TILE }}>
                  <Thumb doc={doc} n={p} />
                  <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', textAlign: 'center', mt: 0.5 }}>{t('第 {n} 页', { n: p })}</Typography>
                </Box>
                {p < n && (
                  <Tooltip title={cuts.has(p) ? t('不在这里拆开') : t('在这里拆开')}>
                    <IconButton aria-label={t('在第 {n} 页之后拆开', { n: p })} aria-pressed={cuts.has(p)} onClick={() => toggle(p)}
                      sx={{ mx: 0.5, alignSelf: 'stretch', borderRadius: 1.5, color: cuts.has(p) ? 'primary.main' : 'text.secondary', bgcolor: cuts.has(p) ? 'primary.light' : 'transparent', borderLeft: cuts.has(p) ? 2 : 0, borderColor: 'primary.main' }}>
                      <Scissors size={16} strokeWidth={SW} />
                    </IconButton>
                  </Tooltip>
                )}
              </Fragment>
            ))}
          </Box>
        )}
        {cuts.size > 0 && <Typography variant="body2" sx={{ mt: 2 }}>{t('将拆成 {n} 份：{parts}', { n: parts.length, parts: parts.join(t('、')) })}</Typography>}
        <FormControlLabel sx={{ mt: 1 }} control={<Checkbox checked={trash} onChange={(e) => setTrash(e.target.checked)} />}
          label={t('拆分后把这份文档移到回收站')} />
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!cuts.size || split.isPending} onClick={submit}>{t('拆分')}</Button>
      </DialogActions>
    </Dialog>
  );
}

/** 合并：调整顺序后合并为一份新文档。 */
export function MergeDialog({ docs, open, onClose, onDone }: { docs: { id: number; title: string }[]; open: boolean; onClose: () => void; onDone: () => void }) {
  const app = useApp();
  const merge = useMergeDocuments();
  const [order, setOrder] = useState(docs);
  const [trash, setTrash] = useState(false);
  useEffect(() => { if (open) { setOrder(docs); setTrash(false); } }, [open, docs]);
  const move = (i: number, by: number) => setOrder((o) => { const next = [...o]; const [x] = next.splice(i, 1); next.splice(i + by, 0, x); return next; });
  const submit = () => merge.mutate({ ids: order.map((d) => d.id), trashOriginals: trash }, {
    onSuccess: () => { app.toast(t('正在合并 {n} 份文档，新文档会出现在待整理', { n: order.length })); onDone(); onClose(); },
    onError: (e) => app.toast(e.message),
  });
  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="merge-title">
      <DialogTitle id="merge-title">{t('合并为一份')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>{t('按下面的顺序合并。新文档的标题、类型、往来方和日期取第一份，标签取所有文档的标签。')}</Typography>
        {order.map((d, i) => (
          <Stack key={d.id} direction="row" sx={{ alignItems: 'center', py: 0.5, borderTop: i ? 1 : 0, borderColor: 'divider' }}>
            <Typography variant="body2" sx={{ color: 'text.secondary', width: 24, flex: 'none' }}>{i + 1}</Typography>
            <Typography sx={{ flex: 1, minWidth: 0, overflowWrap: 'anywhere' }}>{d.title}</Typography>
            <IconButton size="small" aria-label={t('上移“{title}”', { title: d.title })} disabled={i === 0} onClick={() => move(i, -1)}><ArrowUp size={16} strokeWidth={SW} /></IconButton>
            <IconButton size="small" aria-label={t('下移“{title}”', { title: d.title })} disabled={i === order.length - 1} onClick={() => move(i, 1)}><ArrowDown size={16} strokeWidth={SW} /></IconButton>
          </Stack>
        ))}
        <FormControlLabel sx={{ mt: 1 }} control={<Checkbox checked={trash} onChange={(e) => setTrash(e.target.checked)} />}
          label={t('合并后把原来的文档移到回收站')} />
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={merge.isPending} onClick={submit}>{t('合并')}</Button>
      </DialogActions>
    </Dialog>
  );
}

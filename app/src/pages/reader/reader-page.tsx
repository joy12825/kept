// 阅读器（阶段 26）：全屏阅读 PDF 与 EPUB，记住每个人读到哪儿。
import { useCallback, useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { Box, Button, IconButton, Menu, MenuItem, Popover, Stack, TextField, ToggleButton, ToggleButtonGroup, Tooltip, Typography } from '@mui/material';
import { AArrowDown, AArrowUp, ArrowLeft, ChevronLeft, ChevronRight, ListTree, MessageSquareText, StickyNote } from 'lucide-react';
import { fileUrl, useDocument, type DocumentDetail } from '../../api/documents';
import {
  readingKey, saveReading, useCreateHighlight, useDeleteHighlight, useHighlights, useReadingProgress, useUpdateHighlight,
  type Highlight, type HighlightColor, type ReadingProgress,
} from '../../api/reading';
import { useApp } from '../../store/app-store';
import { HighlightPopover, HighlightsPanel, NoteDialog, SelectionPopover } from './highlight-tools';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import { canRead, isEpub } from '../../components/doc-preview';
import { SW } from '../../components/icons';
import { PdfPreview, type PdfController } from '../../components/pdf-preview';
import { EpubView, THEME_COLORS, type EpubHandle, type Point, type ReaderTheme, type TocItem } from './epub-view';
import { t, tc } from '../../i18n';

const THEME_KEY = 'kept-reader-theme';
const FONT_KEY = 'kept-reader-font';
const read = (k: string) => { try { return localStorage.getItem(k); } catch { return null; } };
const write = (k: string, v: string) => { try { localStorage.setItem(k, v); } catch { /* 隐私模式 */ } };

export default function ReaderPage() {
  const id = Number(useParams().id);
  const nav = useNavigate();
  const from = (useLocation().state as { from?: string } | null)?.from ?? `/docs/${id}`;
  const doc = useDocument(Number.isFinite(id) ? id : undefined);
  const progress = useReadingProgress(Number.isFinite(id) ? id : undefined);
  const back = useCallback(() => nav(from), [nav, from]);
  // ?at=… 从“我的批注”打开到某条高亮
  const at = useSearchParams()[0].get('at');

  if (doc.isPending || progress.isPending) return <LoadingState />;
  if (doc.isError) {
    return doc.error.status === 404
      ? <EmptyState title={t('找不到这份文档')} text={t('它可能已被删除。')} action={t('返回')} onAction={() => nav('/')} />
      : <ErrorState onRetry={() => void doc.refetch()} />;
  }
  if (!canRead(doc.data)) return <EmptyState title={t('这份文档不能在阅读器中打开')} text={t('阅读器支持 PDF 和 EPUB。')} action={t('返回')} onAction={back} />;
  return <Reader d={doc.data} initial={at ?? progress.data?.position ?? ''} onBack={back} />;
}

function Reader({ d, initial, onBack }: { d: DocumentDetail; initial: string; onBack: () => void }) {
  const epub = isEpub(d.mimeType);
  const [theme, setTheme] = useState<ReaderTheme>(() => (read(THEME_KEY) as ReaderTheme) || 'light');
  const [font, setFont] = useState(() => Number(read(FONT_KEY)) || 100);
  const [label, setLabel] = useState('');
  const [toc, setToc] = useState<TocItem[]>([]);
  const [tocEl, setTocEl] = useState<HTMLElement | null>(null);
  const [jumpEl, setJumpEl] = useState<HTMLElement | null>(null);
  const [jump, setJump] = useState('');
  const [page, setPage] = useState<{ n: number; total: number } | null>(null);
  const epubRef = useRef<EpubHandle>(null);
  const pdfRef = useRef<PdfController | null>(null);
  const app = useApp();

  // 高亮与批注：选中文字 → 颜色条；点高亮 → 编辑；批注对话框；右侧面板
  const highlights = useHighlights(d.id).data ?? [];
  const create = useCreateHighlight(d.id);
  const update = useUpdateHighlight(d.id);
  const remove = useDeleteHighlight(d.id);
  const [selection, setSelection] = useState<{ cfi: string; text: string; at: Point } | null>(null);
  const [editing, setEditing] = useState<{ id: number; at: Point } | null>(null);
  const [note, setNote] = useState<{ kind: 'selection' | 'page' | 'edit'; quote?: string; initial?: string } | null>(null);
  const [panel, setPanel] = useState(false);
  const current = highlights.find((h) => h.id === editing?.id) ?? null;
  const fail = (e: Error) => app.toast(e.message);
  const addFromSelection = (color: HighlightColor, text?: string) => {
    if (!selection) return;
    create.mutate({ locator: selection.cfi, quote: selection.text, color, note: text }, { onError: fail });
    epubRef.current?.clearSelection();
    setSelection(null);
  };
  const saveNote = (text: string) => {
    if (note?.kind === 'selection') addFromSelection('yellow', text);
    else if (note?.kind === 'page' && page) create.mutate({ locator: `page:${page.n}`, note: text }, { onError: fail, onSuccess: () => app.toast(t('已给第 {n} 页写批注', { n: page.n })) });
    else if (note?.kind === 'edit' && current) update.mutate({ id: current.id, note: text }, { onError: fail });
    setNote(null);
  };
  const jumpTo = (h: Highlight) => {
    setPanel(false);
    const pg = /^page:(\d+)$/.exec(h.locator)?.[1];
    if (pg) pdfRef.current?.goto(Number(pg));
    else epubRef.current?.goto(h.locator);
  };

  // 保存：位置变了 2 秒后保存一次；离开阅读器、关掉页面时立即保存
  const pending = useRef<{ position: string; percent: number } | null>(null);
  const timer = useRef<number>();
  const flush = useCallback((keepalive = false) => {
    window.clearTimeout(timer.current);
    if (pending.current) { void saveReading(d.id, pending.current.position, pending.current.percent, keepalive); pending.current = null; }
  }, [d.id]);
  const qc = useQueryClient();
  const remember = useCallback((position: string, percent: number) => {
    pending.current = { position, percent };
    // 缓存里也改掉：马上再打开时直接从这里开始，不等保存的请求
    qc.setQueryData<ReadingProgress>(readingKey(d.id), (old) => (old ? { ...old, position, percent } : old));
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => flush(), 2000);
  }, [flush, qc, d.id]);
  useEffect(() => {
    const h = () => flush(true);
    window.addEventListener('pagehide', h);
    return () => { window.removeEventListener('pagehide', h); flush(true); };
  }, [flush]);

  const onPage = useCallback((n: number, total: number) => { setPage({ n, total }); remember(`page:${n}`, total ? n / total : 0); }, [remember]);
  const onLocation = useCallback((cfi: string, percent: number, chapter: string) => {
    setLabel(`${chapter ? `${chapter} · ` : ''}${Math.round(percent * 100)}%`);
    remember(cfi, percent);
  }, [remember]);

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (document.querySelector('.MuiPopover-root, .MuiMenu-root, .MuiDialog-root, .MuiDrawer-root')) return;
      if ((e.target as HTMLElement).tagName === 'INPUT') return;
      if (e.key === 'Escape') onBack();
      else if (epub && (e.key === 'ArrowRight' || e.key === 'PageDown' || e.key === ' ')) { e.preventDefault(); epubRef.current?.next(); }
      else if (epub && (e.key === 'ArrowLeft' || e.key === 'PageUp')) { e.preventDefault(); epubRef.current?.prev(); }
    };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  }, [epub, onBack]);

  const pickTheme = (th: ReaderTheme) => { setTheme(th); write(THEME_KEY, th); };
  const pickFont = (f: number) => { const v = Math.min(200, Math.max(70, f)); setFont(v); write(FONT_KEY, String(v)); };
  const initialPage = /^page:(\d+)$/.exec(initial)?.[1];
  const colors = THEME_COLORS[theme];
  const doJump = () => {
    const n = Number(jump);
    if (page && Number.isInteger(n) && n >= 1 && n <= page.total) { pdfRef.current?.goto(n); setJumpEl(null); setJump(''); }
  };

  return (
    <Box sx={{ height: '100vh', display: 'flex', flexDirection: 'column', bgcolor: epub ? colors.bg : 'surface.containerLow' }}>
      <Stack component="header" direction="row" spacing={1} sx={{ alignItems: 'center', height: 56, px: 1, flex: 'none', borderBottom: 1, borderColor: 'divider', bgcolor: 'background.default' }}>
        <Tooltip title={t('返回（Esc）')}><IconButton aria-label={t('返回')} onClick={onBack}><ArrowLeft size={20} strokeWidth={SW} /></IconButton></Tooltip>
        <Typography noWrap variant="h6" component="h1" sx={{ flex: 1, minWidth: 0 }}>{d.title}</Typography>
        <Typography variant="body2" aria-live="polite" sx={{ color: 'text.secondary', whiteSpace: 'nowrap', display: { xs: 'none', sm: 'block' } }}>
          {epub ? label : page ? t('第 {n} / {total} 页', { n: page.n, total: page.total }) : ''}
        </Typography>
        <Tooltip title={t('批注')}><IconButton aria-label={t('批注（{n}）', { n: highlights.length })} onClick={() => setPanel(true)}><MessageSquareText size={20} strokeWidth={SW} /></IconButton></Tooltip>
        {!epub && <Tooltip title={t('给这一页写批注')}><span><IconButton aria-label={t('给这一页写批注')} disabled={!page} onClick={() => setNote({ kind: 'page' })}><StickyNote size={20} strokeWidth={SW} /></IconButton></span></Tooltip>}
        {epub ? (
          <>
            <Tooltip title={tc('大纲', '目录')}><span><IconButton aria-label={tc('大纲', '目录')} disabled={!toc.length} onClick={(e) => setTocEl(e.currentTarget)}><ListTree size={20} strokeWidth={SW} /></IconButton></span></Tooltip>
            <Tooltip title={t('缩小字号')}><IconButton aria-label={t('缩小字号')} onClick={() => pickFont(font - 10)}><AArrowDown size={20} strokeWidth={SW} /></IconButton></Tooltip>
            <Tooltip title={t('放大字号')}><IconButton aria-label={t('放大字号')} onClick={() => pickFont(font + 10)}><AArrowUp size={20} strokeWidth={SW} /></IconButton></Tooltip>
            <ToggleButtonGroup size="small" exclusive value={theme} onChange={(_, v) => v && pickTheme(v)} aria-label={t('底色')}>
              <ToggleButton value="light">{t('白天')}</ToggleButton>
              <ToggleButton value="sepia">{t('护眼')}</ToggleButton>
              <ToggleButton value="dark">{t('夜间')}</ToggleButton>
            </ToggleButtonGroup>
          </>
        ) : (
          <Button size="small" disabled={!page} onClick={(e) => setJumpEl(e.currentTarget)}>{t('跳到…')}</Button>
        )}
      </Stack>
      <Menu anchorEl={tocEl} open={!!tocEl} onClose={() => setTocEl(null)} slotProps={{ paper: { sx: { maxHeight: '70vh', minWidth: 240 } } }}>
        {toc.map((item, i) => (
          <MenuItem key={i} sx={{ pl: 2 + item.depth * 2 }} onClick={() => { setTocEl(null); epubRef.current?.goto(item.href); }}>{item.label}</MenuItem>
        ))}
      </Menu>
      <SelectionPopover at={selection?.at ?? null} onClose={() => setSelection(null)} onColor={(c) => addFromSelection(c)}
        onNote={() => setNote({ kind: 'selection', quote: selection?.text })} />
      <HighlightPopover h={current} at={editing?.at ?? null} onClose={() => setEditing(null)}
        onColor={(c) => current && update.mutate({ id: current.id, color: c }, { onError: fail })}
        onNote={() => { setNote({ kind: 'edit', quote: current?.quote, initial: current?.note }); setEditing(null); }}
        onDelete={() => { if (current) remove.mutate(current.id, { onError: fail }); setEditing(null); }} />
      <NoteDialog open={!!note} title={note?.kind === 'page' ? t('给第 {n} 页写批注', { n: page?.n ?? '' }) : t('批注')} quote={note?.quote} initial={note?.initial}
        busy={create.isPending || update.isPending} onClose={() => setNote(null)} onSave={saveNote} />
      <HighlightsPanel open={panel} items={highlights} onClose={() => setPanel(false)} onPick={jumpTo} />
      <Popover open={!!jumpEl} anchorEl={jumpEl} onClose={() => setJumpEl(null)} anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }} transformOrigin={{ vertical: 'top', horizontal: 'right' }}>
        <Stack direction="row" spacing={1} sx={{ p: 2, alignItems: 'center' }}>
          <TextField autoFocus size="small" label={t('页码（1–{total}）', { total: page?.total ?? 1 })} value={jump} onChange={(e) => setJump(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') doJump(); }} slotProps={{ htmlInput: { inputMode: 'numeric' } }} sx={{ width: 140 }} />
          <Button variant="contained" onClick={doJump}>{t('跳转')}</Button>
        </Stack>
      </Popover>

      {epub ? (
        <Box sx={{ flex: 1, minHeight: 0, position: 'relative', display: 'flex' }}>
          <IconButton aria-label={t('上一页')} onClick={() => epubRef.current?.prev()} sx={{ flex: 'none', alignSelf: 'center', mx: { xs: 0, sm: 1 }, color: colors.fg, opacity: 0.6 }}>
            <ChevronLeft size={28} strokeWidth={SW} />
          </IconButton>
          <Box sx={{ flex: 1, minWidth: 0, maxWidth: 760, mx: 'auto', py: 3 }}>
            <EpubView ref={epubRef} url={fileUrl(d.id, 'original')} initial={initial || undefined} theme={theme} fontSize={font} onLocation={onLocation} onToc={setToc}
              highlights={highlights} onSelect={(cfi, text, at) => { setEditing(null); setSelection({ cfi, text, at }); }} onHighlight={(id, at) => { setSelection(null); setEditing({ id, at }); }} />
          </Box>
          <IconButton aria-label={t('下一页')} onClick={() => epubRef.current?.next()} sx={{ flex: 'none', alignSelf: 'center', mx: { xs: 0, sm: 1 }, color: colors.fg, opacity: 0.6 }}>
            <ChevronRight size={28} strokeWidth={SW} />
          </IconButton>
        </Box>
      ) : (
        <Box sx={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
          <Box sx={{ maxWidth: 1000, mx: 'auto' }}>
            <PdfPreview url={fileUrl(d.id, 'preview', String(new Date(d.modifiedAt).getTime()))} initialPage={initialPage ? Number(initialPage) : undefined} onPage={onPage} controller={pdfRef} pageIndicator={false} />
          </Box>
        </Box>
      )}
    </Box>
  );
}

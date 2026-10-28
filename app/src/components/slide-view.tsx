// 幻灯片（v1.9.7）：演示文稿的可搜索版一页就是一张幻灯片，一屏一张地看，下面一排缩略图；
// ← → 翻页，F 全屏放映，放映时点右边下一张、点左边上一张。不自己解析 PPT，看的是 LibreOffice 转出的原样。
import { useEffect, useRef, useState } from 'react';
import { Box, ButtonBase, CircularProgress, Skeleton, Stack, Typography } from '@mui/material';
import { ChevronLeft, ChevronRight, Maximize, Minimize } from 'lucide-react';
import * as pdfjs from 'pdfjs-dist';
import type { PDFDocumentProxy } from 'pdfjs-dist';
import workerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url';
import { BarButton, BarDivider, BarText, FloatingBar, useFullscreen, usePreviewKeys } from './preview/toolbar';
import { t } from '../i18n';

pdfjs.GlobalWorkerOptions.workerSrc = workerUrl;

type RenderTask = ReturnType<Awaited<ReturnType<PDFDocumentProxy['getPage']>>['render']>;

/** 把第 n 页画进 canvas，最大 maxW × maxH（CSS 像素），保持比例。 */
function useSlideCanvas(doc: PDFDocumentProxy | null, n: number, maxW: number, maxH: number, enabled = true) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const [ready, setReady] = useState(false);
  useEffect(() => {
    setReady(false);
    if (!doc || !enabled || maxW <= 0 || maxH <= 0) return;
    let cancelled = false;
    let task: RenderTask | null = null;
    void doc.getPage(n).then((page) => {
      if (cancelled || !canvas.current) return;
      const one = page.getViewport({ scale: 1 });
      const vp = page.getViewport({ scale: Math.min(maxW / one.width, maxH / one.height) });
      const ratio = window.devicePixelRatio || 1;
      const c = canvas.current;
      c.width = Math.floor(vp.width * ratio); c.height = Math.floor(vp.height * ratio);
      c.style.width = `${Math.floor(vp.width)}px`; c.style.height = `${Math.floor(vp.height)}px`;
      task = page.render({ canvas: c, viewport: vp, transform: ratio !== 1 ? [ratio, 0, 0, ratio, 0, 0] : undefined });
      task.promise.then(() => { if (!cancelled) setReady(true); }, () => { /* 被取消 */ });
    });
    return () => { cancelled = true; task?.cancel(); };
  }, [doc, n, maxW, maxH, enabled]);
  return { canvas, ready };
}

function Thumb({ doc, n, active, onClick }: { doc: PDFDocumentProxy; n: number; active: boolean; onClick: () => void }) {
  const box = useRef<HTMLButtonElement>(null);
  const [visible, setVisible] = useState(n <= 8);
  useEffect(() => {
    const el = box.current;
    if (!el || visible) return;
    const io = new IntersectionObserver((es) => { if (es.some((e) => e.isIntersecting)) setVisible(true); }, { rootMargin: '400px' });
    io.observe(el);
    return () => io.disconnect();
  }, [visible]);
  useEffect(() => { if (active) box.current?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' }); }, [active]);
  const { canvas, ready } = useSlideCanvas(doc, n, 120, 90, visible);
  return (
    <ButtonBase ref={box} onClick={onClick} aria-label={t('第 {n} 张', { n })} aria-current={active ? 'true' : undefined}
      sx={{ flex: 'none', flexDirection: 'column', gap: 0.5, p: 0.75, borderRadius: 1.5, transition: 'background-color 150ms ease-out', '&:hover': { bgcolor: 'action.hover' } }}>
      <Box sx={{ position: 'relative', lineHeight: 0, minWidth: 120, minHeight: 68, display: 'flex', alignItems: 'center', justifyContent: 'center',
        outline: active ? '2px solid' : '1px solid', outlineColor: active ? 'primary.main' : 'divider', bgcolor: '#fff' }}>
        {!ready && <Skeleton variant="rectangular" width={120} height={68} sx={{ position: 'absolute', inset: 0 }} />}
        <canvas ref={canvas} />
      </Box>
      <Typography sx={{ fontSize: 12, color: active ? 'primary.main' : 'text.secondary', fontVariantNumeric: 'tabular-nums' }}>{n}</Typography>
    </ButtonBase>
  );
}

export function SlideView({ url, shortcuts = false }: { url: string; shortcuts?: boolean }) {
  const [doc, setDoc] = useState<PDFDocumentProxy | null>(null);
  const [error, setError] = useState(false);
  const [n, setN] = useState(1);
  const root = useRef<HTMLDivElement>(null);
  const stage = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ w: 0, h: 0 });
  const full = useFullscreen(root);

  useEffect(() => {
    setDoc(null); setError(false); setN(1);
    let cancelled = false;
    const task = pdfjs.getDocument({ url, withCredentials: true });
    task.promise.then((d) => { if (!cancelled) setDoc(d); }, () => { if (!cancelled) setError(true); });
    return () => { cancelled = true; void task.destroy(); };
  }, [url]);

  // 幻灯片按舞台的大小画：窗口变化、进出全屏时重画
  useEffect(() => {
    const el = stage.current;
    if (!el) return;
    const measure = () => setSize({ w: Math.floor(el.clientWidth - 32), h: Math.floor(el.clientHeight - 32) });
    measure();
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(measure) : null;
    ro?.observe(el);
    return () => ro?.disconnect();
  }, [doc, full.on]);

  const total = doc?.numPages ?? 0;
  const go = (k: number) => setN((x) => Math.min(Math.max(1, x + k), Math.max(1, total)));
  const { canvas, ready } = useSlideCanvas(doc, n, size.w, size.h);

  usePreviewKeys(shortcuts || full.on, {
    arrowright: () => go(1), arrowdown: () => go(1), pagedown: () => go(1), ' ': () => go(1),
    arrowleft: () => go(-1), arrowup: () => go(-1), pageup: () => go(-1),
    home: () => setN(1), end: () => setN(Math.max(1, total)), f: full.toggle,
  });

  if (error) return <Typography sx={{ p: 3, color: 'text.secondary' }}>{t('幻灯片没能打开，可以换到“可搜索版”看。')}</Typography>;
  if (!doc) return <Box sx={{ display: 'flex', justifyContent: 'center', p: 6 }}><CircularProgress size={24} /></Box>;

  // 放映时点右边下一张、点左边上一张
  const onStageClick = (e: React.MouseEvent<HTMLDivElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    go(e.clientX - r.left < r.width / 3 ? -1 : 1);
  };

  return (
    <Box ref={root} aria-label={t('幻灯片')} sx={{ display: 'flex', flexDirection: 'column', bgcolor: full.on ? '#000' : 'surface.containerLow', height: full.on ? '100vh' : 'auto' }}>
      <Box ref={stage} role="group" aria-roledescription={t('幻灯片')} aria-label={t('第 {n} 张，共 {total} 张', { n, total })} onClick={onStageClick}
        sx={{ position: 'relative', height: full.on ? '100%' : { xs: '56vw', md: 'min(62vh, 56vw)' }, minHeight: 240, flex: full.on ? 1 : 'none',
          display: 'flex', alignItems: 'center', justifyContent: 'center', cursor: 'pointer', userSelect: 'none' }}>
        {!ready && <CircularProgress size={24} sx={{ position: 'absolute' }} />}
        <canvas ref={canvas} style={{ boxShadow: full.on ? 'none' : '0 1px 4px rgba(0,0,0,.18)', background: '#fff' }} />
      </Box>
      {!full.on && (
        <Stack direction="row" spacing={0.5} aria-label={t('全部幻灯片')} sx={{ overflowX: 'auto', px: 1.5, pt: 1, pb: 7, borderTop: 1, borderColor: 'divider' }}>
          {Array.from({ length: total }, (_, i) => <Thumb key={i + 1} doc={doc} n={i + 1} active={i + 1 === n} onClick={() => setN(i + 1)} />)}
        </Stack>
      )}
      <Box sx={full.on ? { position: 'fixed', left: 0, right: 0, bottom: 0, pb: 2 } : undefined}>
        <FloatingBar>
          <BarButton icon={ChevronLeft} label={t('上一张')} keys="←" disabled={n <= 1} onClick={() => go(-1)} />
          <BarText width={72} live>{n} / {total}</BarText>
          <BarButton icon={ChevronRight} label={t('下一张')} keys="→" disabled={n >= total} onClick={() => go(1)} />
          {full.supported && (
            <>
              <BarDivider />
              <BarButton icon={full.on ? Minimize : Maximize} label={full.on ? t('退出放映') : t('全屏放映')} keys="F" onClick={full.toggle} />
            </>
          )}
        </FloatingBar>
      </Box>
    </Box>
  );
}

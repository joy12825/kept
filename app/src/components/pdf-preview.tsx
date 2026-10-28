// 用 pdf.js 预览 PDF：逐页懒渲染（滚到附近才画），可以缩放。
// v1.9.1 预览器：底部浮条补上旋转查看（只转显示）、页面缩略图和目录侧栏、全屏，以及快捷键 + − 0 R P F。
import { useEffect, useMemo, useRef, useState } from 'react';
import { Box, ButtonBase, CircularProgress, Skeleton, Stack, ToggleButton, ToggleButtonGroup, Typography } from '@mui/material';
import { Maximize, Minimize, Minus, MoveHorizontal, PanelLeft, Plus, RotateCw } from 'lucide-react';
import * as pdfjs from 'pdfjs-dist';
import type { PDFDocumentProxy } from 'pdfjs-dist';
import workerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url';
import { pageHits, type Hit, type TextRun } from '../utils/pdf-find';
import type { FindState } from './find-bar';
import { HIT, HIT_CURRENT } from './find-text';
import { BarButton, BarDivider, BarText, FloatingBar, useFullscreen, usePreviewKeys } from './preview/toolbar';
import { t, tc } from '../i18n';

pdfjs.GlobalWorkerOptions.workerSrc = workerUrl;

/** 预先下载 worker，放进浏览器缓存（pdf-lazy 的 warmPdf 调用）。 */
export function prefetchWorker() {
  return fetch(workerUrl, { credentials: 'same-origin' }).then((r) => r.blob()).then(() => undefined);
}

const ZOOMS = [0.5, 0.75, 1, 1.25, 1.5, 2, 3];
const FIT = ZOOMS.indexOf(1);

type Rect = [number, number, number, number];

/** 把页面坐标里的一块 [x, y, w, h]（页面宽 w0、高 h0）顺时针转 rot 度 */
export function rotateRect([x, y, w, h]: Rect, w0: number, h0: number, rot: number): Rect {
  switch (rot) {
    case 90: return [h0 - y - h, x, h, w];
    case 180: return [w0 - x - w, h0 - y - h, w, h];
    case 270: return [y, w0 - x - w, h, w];
    default: return [x, y, w, h];
  }
}

function Page({ doc, n, scale, rotation, est, hits, current, mark }: {
  doc: PDFDocumentProxy; n: number; scale: number; rotation: number; est?: { w: number; h: number };
  /** 这一页上的查找结果和它们的序号 */
  hits?: { hit: Hit; i: number }[]; current?: number;
  /** 标出一块（比例坐标 x0,y0,x1,y1，v1.8.2 核对检查结果） */
  mark?: [number, number, number, number];
}) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const box = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(n === 1);
  // 还没取到这一页的尺寸时按第一页估计，跳到某一页时位置才准
  const [size, setSize] = useState<{ w: number; h: number } | null>(est ?? null);
  // 不转时的尺寸（比例 1），查找结果按它转过去
  const [base, setBase] = useState<{ w: number; h: number } | null>(null);

  useEffect(() => {
    const el = box.current;
    if (!el || visible) return;
    const io = new IntersectionObserver((es) => { if (es.some((e) => e.isIntersecting)) setVisible(true); }, { rootMargin: '600px' });
    io.observe(el);
    return () => io.disconnect();
  }, [visible]);

  useEffect(() => {
    // 离得远的页先按第一页的尺寸占位，不去取：打开时一次取所有页会和第一页抢带宽
    if (!visible) return;
    let cancelled = false;
    let task: ReturnType<Awaited<ReturnType<PDFDocumentProxy['getPage']>>['render']> | null = null;
    void doc.getPage(n).then((page) => {
      if (cancelled) return;
      const b = page.getViewport({ scale: 1 });
      setBase({ w: b.width, h: b.height });
      const vp = page.getViewport({ scale, rotation: (page.rotate + rotation) % 360 });
      setSize({ w: vp.width, h: vp.height });
      if (!visible || !canvas.current) return;
      const ratio = window.devicePixelRatio || 1;
      const c = canvas.current;
      c.width = Math.floor(vp.width * ratio);
      c.height = Math.floor(vp.height * ratio);
      c.style.width = `${vp.width}px`;
      c.style.height = `${vp.height}px`;
      task = page.render({ canvas: c, viewport: vp, transform: ratio !== 1 ? [ratio, 0, 0, ratio, 0, 0] : undefined });
      task.promise.catch(() => { /* 切换文档、缩放或旋转时被取消 */ });
    });
    return () => { cancelled = true; task?.cancel(); };
  }, [doc, n, scale, rotation, visible]);

  const at = (r: Rect) => (base ? rotateRect(r, base.w, base.h, rotation) : r);
  const markRect = mark && rotateRect([mark[0], mark[1], mark[2] - mark[0], mark[3] - mark[1]], 1, 1, rotation);
  return (
    <Box ref={box} data-page={n} sx={{ position: 'relative', bgcolor: '#fff', boxShadow: 1, mx: 'auto', width: size?.w ?? '100%', height: size?.h ?? 800, flex: 'none' }}>
      <canvas ref={canvas} aria-label={t('第 {n} 页', { n })} />
      {hits?.map(({ hit, i }) => hit.rects.map((r, k) => {
        const [x, y, w, h] = at(r as Rect);
        return (
          <Box key={`${i}-${k}`} data-find-hit={i} data-find-current={(i === current && k === 0) || undefined}
            sx={{ position: 'absolute', left: x * scale, top: y * scale, width: w * scale, height: h * scale, bgcolor: i === current ? HIT_CURRENT : HIT, mixBlendMode: 'multiply', borderRadius: '2px', pointerEvents: 'none' }} />
        );
      }))}
      {markRect && size && (
        <Box data-mark sx={{ position: 'absolute', left: markRect[0] * size.w - 4, top: markRect[1] * size.h - 3, width: markRect[2] * size.w + 8, height: markRect[3] * size.h + 6,
          border: '2px solid', borderColor: 'primary.main', bgcolor: HIT, mixBlendMode: 'multiply', borderRadius: '4px', pointerEvents: 'none' }} />
      )}
    </Box>
  );
}

/** 侧栏里的一页缩略图：滚到附近才画 */
function Thumb({ doc, n, rotation, active, onClick }: { doc: PDFDocumentProxy; n: number; rotation: number; active: boolean; onClick: () => void }) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const box = useRef<HTMLButtonElement>(null);
  const [visible, setVisible] = useState(n <= 6);
  const [ready, setReady] = useState(false);
  useEffect(() => {
    const el = box.current;
    if (!el || visible) return;
    const io = new IntersectionObserver((es) => { if (es.some((e) => e.isIntersecting)) setVisible(true); }, { rootMargin: '300px' });
    io.observe(el);
    return () => io.disconnect();
  }, [visible]);
  useEffect(() => {
    if (!visible) return;
    let cancelled = false;
    let task: ReturnType<Awaited<ReturnType<PDFDocumentProxy['getPage']>>['render']> | null = null;
    void doc.getPage(n).then((page) => {
      if (cancelled || !canvas.current) return;
      const rot = (page.rotate + rotation) % 360;
      const one = page.getViewport({ scale: 1, rotation: rot });
      const vp = page.getViewport({ scale: 96 / one.width, rotation: rot });
      const ratio = window.devicePixelRatio || 1;
      const c = canvas.current;
      c.width = Math.floor(vp.width * ratio); c.height = Math.floor(vp.height * ratio);
      c.style.width = `${vp.width}px`; c.style.height = `${vp.height}px`;
      task = page.render({ canvas: c, viewport: vp, transform: ratio !== 1 ? [ratio, 0, 0, ratio, 0, 0] : undefined });
      task.promise.then(() => { if (!cancelled) setReady(true); }, () => { /* 被取消 */ });
    });
    return () => { cancelled = true; task?.cancel(); };
  }, [doc, n, rotation, visible]);
  return (
    <ButtonBase ref={box} onClick={onClick} aria-label={t('第 {n} 页', { n })} aria-current={active ? 'page' : undefined}
      sx={{ flexDirection: 'column', gap: 0.5, py: 1, borderRadius: 1.5, width: '100%', transition: 'background-color 150ms ease-out', '&:hover': { bgcolor: 'action.hover' } }}>
      <Box sx={{ position: 'relative', lineHeight: 0, outline: active ? '2px solid' : '1px solid', outlineColor: active ? 'primary.main' : 'divider', bgcolor: '#fff' }}>
        {!ready && <Skeleton variant="rectangular" width={96} height={128} sx={{ position: 'absolute', inset: 0 }} />}
        <canvas ref={canvas} style={{ width: 96, height: 128 }} />
      </Box>
      <Typography sx={{ fontSize: 12, color: active ? 'primary.main' : 'text.secondary', fontVariantNumeric: 'tabular-nums' }}>{n}</Typography>
    </ButtonBase>
  );
}

interface OutlineItem { title: string; dest: string | unknown[] | null; items: OutlineItem[] }

/** 目录项指向的页（从 1 数）；找不到时为 null */
async function destPage(doc: PDFDocumentProxy, dest: OutlineItem['dest']): Promise<number | null> {
  try {
    const d = typeof dest === 'string' ? await doc.getDestination(dest) : dest;
    if (!d || !d.length) return null;
    const ref = d[0];
    const idx = typeof ref === 'number' ? ref : await doc.getPageIndex(ref as Parameters<PDFDocumentProxy['getPageIndex']>[0]);
    return idx + 1;
  } catch { return null; }
}

function Outline({ items, depth, onGo }: { items: OutlineItem[]; depth: number; onGo: (dest: OutlineItem['dest']) => void }) {
  return (
    <>
      {items.map((it, i) => (
        <Box key={i}>
          <ButtonBase onClick={() => onGo(it.dest)} disabled={!it.dest}
            sx={{ display: 'block', width: '100%', textAlign: 'left', py: 0.75, pr: 1, pl: 1 + depth * 1.5, borderRadius: 1, fontSize: 13, lineHeight: '20px',
              color: depth ? 'text.secondary' : 'text.primary', transition: 'background-color 150ms ease-out', '&:hover': { bgcolor: 'action.hover' } }}>
            {it.title || t('（无标题）')}
          </ButtonBase>
          {it.items?.length > 0 && <Outline items={it.items} depth={depth + 1} onGo={onGo} />}
        </Box>
      ))}
    </>
  );
}

/** 阅读器用：跳到某一页。 */
export interface PdfController { goto: (page: number) => void }

export function PdfPreview({ url, initialPage, onPage, controller, pageIndicator = true, find, mark, shortcuts = false }: {
  url: string;
  /** 打开时先跳到这一页（阅读器恢复位置） */
  initialPage?: number;
  /** 当前看得最多的那一页变了 */
  onPage?: (page: number, total: number) => void;
  controller?: React.MutableRefObject<PdfController | null>;
  /** 底部浮条里显示“第 n / 共 页”（阅读器自己在顶栏显示，传 false） */
  pageIndicator?: boolean;
  /** 查找框（详情页）：标出 terms，跳到第 current 处 */
  find?: FindState;
  /** 标出某一页上的一块并滚到那里（v1.8.2：核对检查结果时点的那一行） */
  mark?: { page: number; box: [number, number, number, number] } | null;
  /** 响应 + − 0 R P F 快捷键（详情页，v1.9.1） */
  shortcuts?: boolean;
}) {
  const [doc, setDoc] = useState<PDFDocumentProxy | null>(null);
  const [error, setError] = useState(false);
  const [zoom, setZoom] = useState(FIT);
  const [rotation, setRotation] = useState(0);
  const [side, setSide] = useState<null | 'pages' | 'outline'>(null);
  const [outline, setOutline] = useState<OutlineItem[]>([]);
  // 100% 指“适合宽度”：按第一页的宽度和预览区的宽度算出基础比例
  const root = useRef<HTMLDivElement>(null);
  const box = useRef<HTMLDivElement>(null);
  const [boxWidth, setBoxWidth] = useState(0);
  const [pageWidth, setPageWidth] = useState(0);
  const [pageHeight, setPageHeight] = useState(0);
  const jumped = useRef(false);
  const [current, setCurrent] = useState(1);
  const full = useFullscreen(root);

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setBoxWidth(el.clientWidth));
    ro.observe(el);
    setBoxWidth(el.clientWidth);
    return () => ro.disconnect();
  }, [doc]);

  useEffect(() => {
    if (!doc) return;
    let cancelled = false;
    void doc.getPage(1).then((p) => {
      if (cancelled) return;
      const vp = p.getViewport({ scale: 1 });
      setPageWidth(vp.width); setPageHeight(vp.height);
    });
    void doc.getOutline().then((o) => { if (!cancelled) setOutline((o ?? []) as OutlineItem[]); }, () => { /* 没有目录 */ });
    return () => { cancelled = true; };
  }, [doc]);

  useEffect(() => {
    setDoc(null); setError(false); setOutline([]); setRotation(0);
    let cancelled = false;
    const task = pdfjs.getDocument({ url, withCredentials: true });
    // 被取消的加载也会以“Loading aborted”失败，不能把它当成错误
    task.promise.then((d) => { if (!cancelled) setDoc(d); }, () => { if (!cancelled) setError(true); });
    return () => { cancelled = true; void task.destroy(); };
  }, [url]);

  // 查找：逐页取文字（取过的留着），算出结果；先算完的页先显示、先计数
  const texts = useRef(new Map<number, { runs: TextRun[]; vp: number[] }>());
  useEffect(() => { texts.current = new Map(); }, [doc]);
  const [hits, setHits] = useState<Hit[]>([]);
  const termsKey = find?.terms.join(' ') ?? '';
  const onCount = find?.onCount;
  useEffect(() => {
    setHits([]);
    if (!doc || !termsKey) return;
    const terms = termsKey.split(' ');
    let cancelled = false;
    void (async () => {
      const all: Hit[] = [];
      for (let n = 1; n <= doc.numPages && !cancelled; n++) {
        let text = texts.current.get(n);
        if (!text) {
          try {
            const page = await doc.getPage(n);
            const tc = await page.getTextContent();
            const runs = tc.items.filter((it): it is typeof it & TextRun => 'str' in it);
            text = { runs, vp: page.getViewport({ scale: 1 }).transform };
            texts.current.set(n, text);
          } catch { continue; }
        }
        if (cancelled) return;
        const found = pageHits(text.runs, text.vp, terms, n);
        if (found.length) { all.push(...found); setHits([...all]); onCount?.(all.length); }
      }
      if (!cancelled) onCount?.(all.length);
    })();
    return () => { cancelled = true; };
  }, [doc, termsKey, onCount]);
  const byPage = useMemo(() => {
    const m = new Map<number, { hit: Hit; i: number }[]>();
    hits.forEach((hit, i) => { m.set(hit.page, [...(m.get(hit.page) ?? []), { hit, i }]); });
    return m;
  }, [hits]);
  const findCurrent = find && hits.length ? Math.min(find.current, hits.length - 1) : -1;
  const hasCurrent = findCurrent >= 0;
  useEffect(() => {
    if (!hasCurrent) return;
    requestAnimationFrame(() => box.current?.querySelector('[data-find-current]')?.scrollIntoView?.({ block: 'center' }));
  }, [find?.seq, hasCurrent]); // eslint-disable-line react-hooks/exhaustive-deps

  const goto = (n: number) => box.current?.querySelector(`[data-page="${n}"]`)?.scrollIntoView({ block: 'start' });
  // 标出的一块：先到那一页，等它排好再滚到那一块
  const markKey = mark ? `${mark.page}:${mark.box.join(',')}` : '';
  useEffect(() => {
    if (!mark || !doc) return;
    goto(mark.page);
    const timer = window.setTimeout(() => box.current?.querySelector('[data-mark]')?.scrollIntoView?.({ block: 'center' }), 200);
    return () => window.clearTimeout(timer);
  }, [markKey, doc]); // eslint-disable-line react-hooks/exhaustive-deps
  if (controller) controller.current = { goto };

  // 恢复位置：页面占位排好之后跳一次
  useEffect(() => {
    if (!doc || !pageWidth || jumped.current) return;
    jumped.current = true;
    if (initialPage && initialPage > 1) requestAnimationFrame(() => goto(Math.min(initialPage, doc.numPages)));
  }, [doc, pageWidth]); // eslint-disable-line react-hooks/exhaustive-deps

  // 当前页：露出部分最多的那一页（底部浮条显示，阅读器记位置）
  useEffect(() => {
    const el = box.current;
    if (!doc || !pageWidth || !el || typeof IntersectionObserver === 'undefined') return;
    const ratios = new Map<number, number>();
    let last = 0;
    const io = new IntersectionObserver((es) => {
      for (const e of es) ratios.set(Number((e.target as HTMLElement).dataset.page), e.intersectionRatio);
      let best = 0, bestRatio = 0;
      for (const [n, r] of ratios) if (r > bestRatio || (r === bestRatio && n < best)) { best = n; bestRatio = r; }
      if (best && best !== last) { last = best; setCurrent(best); onPage?.(best, doc.numPages); }
    }, { threshold: [0, 0.1, 0.25, 0.5, 0.75, 1] });
    el.querySelectorAll('[data-page]').forEach((p) => io.observe(p));
    return () => io.disconnect();
  }, [doc, pageWidth, rotation, onPage]);

  // 转过之后，当前页留在原处
  const rotate = () => { const at = current; setRotation((r) => (r + 90) % 360); requestAnimationFrame(() => goto(at)); };
  const toggleSide = () => setSide((s) => (s ? null : 'pages'));
  usePreviewKeys(shortcuts && !!doc, {
    '+': () => setZoom((z) => Math.min(ZOOMS.length - 1, z + 1)), '-': () => setZoom((z) => Math.max(0, z - 1)), '0': () => setZoom(FIT),
    r: rotate, p: toggleSide, f: full.toggle,
  });

  if (error) return <Typography sx={{ color: 'text.secondary', p: 4, textAlign: 'center' }}>{t('无法预览这份文档，可以下载原件查看。')}</Typography>;
  if (!doc) return <Box sx={{ display: 'flex', justifyContent: 'center', p: 6 }}><CircularProgress size={24} /></Box>;
  const turned = rotation % 180 !== 0;
  const w1 = turned ? pageHeight : pageWidth, h1 = turned ? pageWidth : pageHeight;
  const fit = boxWidth && w1 ? Math.max(0.25, (boxWidth - 32) / w1) : 1;
  const scale = ZOOMS[zoom];
  const roomForSide = (root.current?.clientWidth ?? 1024) >= 600;
  const sideOpen = !!side && roomForSide;
  return (
    <Box ref={root} sx={{ position: 'relative', '&:fullscreen': { overflow: 'auto', bgcolor: 'surface.containerLow' } }}>
      <Box sx={{ display: 'flex', alignItems: 'flex-start' }}>
        {sideOpen && (
          <Box component="nav" aria-label={side === 'outline' ? tc('大纲', '目录') : t('页面')}
            sx={{ position: 'sticky', top: full.on ? 0 : 48, flex: 'none', width: 148, maxHeight: full.on ? '100vh' : 'calc(100vh - 56px - 64px)', overflowY: 'auto',
              borderRight: 1, borderColor: 'divider', bgcolor: 'background.paper', borderRadius: '0 8px 8px 0', ml: 0, my: 2, px: 1, pb: 1 }}>
            {outline.length > 0 && (
              <Stack sx={{ position: 'sticky', top: 0, zIndex: 1, bgcolor: 'background.paper', pt: 1, pb: 0.5 }}>
                <ToggleButtonGroup size="small" exclusive fullWidth value={side} onChange={(_, v) => v && setSide(v)} aria-label={t('侧栏内容')}>
                  <ToggleButton value="pages">{t('页面')}</ToggleButton>
                  <ToggleButton value="outline">{tc('大纲', '目录')}</ToggleButton>
                </ToggleButtonGroup>
              </Stack>
            )}
            {side === 'outline' && outline.length > 0
              ? <Box sx={{ pt: 0.5 }}><Outline items={outline} depth={0} onGo={(dest) => void destPage(doc, dest).then((n) => { if (n) goto(n); })} /></Box>
              : Array.from({ length: doc.numPages }, (_, i) => (
                <Thumb key={i + 1} doc={doc} n={i + 1} rotation={rotation} active={current === i + 1} onClick={() => goto(i + 1)} />
              ))}
          </Box>
        )}
        {/* 不用 alignItems:center：页面比预览区宽时会被裁掉左边；Page 自己用 mx:auto 居中 */}
        <Box ref={box} sx={{ flex: 1, minWidth: 0 }}>
          <Stack spacing={2} sx={{ pt: 2, pb: 9, px: 2, overflowX: 'auto' }}>
            {pageWidth > 0 && Array.from({ length: doc.numPages }, (_, i) => (
              <Page key={i + 1} doc={doc} n={i + 1} scale={scale * fit} rotation={rotation} est={{ w: w1 * scale * fit, h: h1 * scale * fit }}
                hits={byPage.get(i + 1)} current={findCurrent} mark={mark?.page === i + 1 ? mark.box : undefined} />
            ))}
          </Stack>
        </Box>
      </Box>
      <FloatingBar>
        {roomForSide && <BarButton icon={PanelLeft} label={sideOpen ? t('收起侧栏') : t('页面和目录')} keys="P" active={sideOpen} onClick={toggleSide} />}
        {pageIndicator && <BarText live>{current} / {doc.numPages}</BarText>}
        {(pageIndicator || roomForSide) && <BarDivider />}
        <BarButton icon={Minus} label={t('缩小')} keys="−" disabled={zoom === 0} onClick={() => setZoom((z) => z - 1)} />
        <BarText width={40}>{Math.round(scale * 100)}%</BarText>
        <BarButton icon={Plus} label={t('放大')} keys="+" disabled={zoom === ZOOMS.length - 1} onClick={() => setZoom((z) => z + 1)} />
        <BarButton icon={MoveHorizontal} label={t('适合宽度')} keys="0" disabled={zoom === FIT} onClick={() => setZoom(FIT)} />
        <BarDivider />
        <BarButton icon={RotateCw} label={t('旋转查看')} keys="R" onClick={rotate} />
        {full.supported && <BarButton icon={full.on ? Minimize : Maximize} label={full.on ? t('退出全屏') : t('全屏')} keys="F" onClick={full.toggle} />}
      </FloatingBar>
    </Box>
  );
}

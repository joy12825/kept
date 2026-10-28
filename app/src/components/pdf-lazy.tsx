// pdf.js 按需加载（v1.3.2）：它和 worker 共约 2.9 MB，不放进首屏的包里。
// 应用空闲后预先取好（warmPdf），第一次打开 PDF 时就不用再等。
import { lazy, Suspense, type ComponentProps } from 'react';
import { Box, CircularProgress } from '@mui/material';

const load = () => import('./pdf-preview');
const tools = () => import('./page-tools');

const PdfPreviewImpl = lazy(() => load().then((m) => ({ default: m.PdfPreview })));
const SlideViewImpl = lazy(() => import('./slide-view').then((m) => ({ default: m.SlideView })));
const PageEditorImpl = lazy(() => tools().then((m) => ({ default: m.PageEditorDialog })));
const SplitImpl = lazy(() => tools().then((m) => ({ default: m.SplitDialog })));
const MergeImpl = lazy(() => tools().then((m) => ({ default: m.MergeDialog })));

const spinner = <Box sx={{ display: 'flex', justifyContent: 'center', p: 6 }}><CircularProgress size={24} /></Box>;

export type { PdfController } from './pdf-preview';

export function PdfPreview(props: ComponentProps<typeof PdfPreviewImpl>) {
  return <Suspense fallback={spinner}><PdfPreviewImpl {...props} /></Suspense>;
}

/** 幻灯片（v1.9.7）：同样按需加载 pdf.js */
export function SlideView(props: ComponentProps<typeof SlideViewImpl>) {
  return <Suspense fallback={spinner}><SlideViewImpl {...props} /></Suspense>;
}

// 对话框没打开时不加载
export function PageEditorDialog(props: ComponentProps<typeof PageEditorImpl>) {
  return props.open ? <Suspense fallback={null}><PageEditorImpl {...props} /></Suspense> : null;
}
export function SplitDialog(props: ComponentProps<typeof SplitImpl>) {
  return props.open ? <Suspense fallback={null}><SplitImpl {...props} /></Suspense> : null;
}
export function MergeDialog(props: ComponentProps<typeof MergeImpl>) {
  return props.open ? <Suspense fallback={null}><MergeImpl {...props} /></Suspense> : null;
}

let warmed = false;
/** 空闲时预取 pdf.js 与 worker（浏览器缓存一年，之后打开 PDF 不再下载）。 */
export function warmPdf() {
  if (warmed || typeof window === 'undefined') return;
  warmed = true;
  const run = () => { void load().then((m) => m.prefetchWorker()).catch(() => { warmed = false; }); };
  const idle = (window as Window & { requestIdleCallback?: (cb: () => void, o?: { timeout: number }) => number }).requestIdleCallback;
  if (idle) idle(run, { timeout: 5000 }); else setTimeout(run, 2000);
}

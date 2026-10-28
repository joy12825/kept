// 原件查看器（v1.9.1）：看照片和扫描图片的原件。缩放（按钮、+ − 0、按住 ⌘ 或 Ctrl 滚轮）、旋转、镜像、放大后拖动、全屏。
// 只改显示，不改文件；改文件仍用“编辑页面”。
import { useEffect, useRef, useState } from 'react';
import { Box, Skeleton } from '@mui/material';
import { FlipHorizontal2, Maximize, Minimize, Minus, MoveHorizontal, Plus, RotateCw } from 'lucide-react';
import { BarButton, BarDivider, BarText, FloatingBar, useFullscreen, usePreviewKeys } from './toolbar';
import { PreviewUnavailable } from './unavailable';
import { t } from '../../i18n';

/** 相对“适合窗口”的倍数 */
const ZOOMS = [0.25, 0.5, 0.75, 1, 1.5, 2, 3, 4, 6];
const FIT = ZOOMS.indexOf(1);

export function ImageViewer({ url, alt, shortcuts = false, onDownload, height }: {
  url: string; alt: string;
  /** 查看区的高度；默认占满详情页的预览区 */
  height?: string;
  /** 响应 + − 0 R F 快捷键（详情页） */
  shortcuts?: boolean;
  onDownload?: () => void;
}) {
  const box = useRef<HTMLDivElement>(null);
  const [natural, setNatural] = useState<{ w: number; h: number } | null>(null);
  const [error, setError] = useState(false);
  const [boxSize, setBoxSize] = useState({ w: 0, h: 0 });
  const [zoom, setZoom] = useState(FIT);
  const [rotate, setRotate] = useState(0);
  const [mirror, setMirror] = useState(false);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const drag = useRef<{ x: number; y: number; px: number; py: number } | null>(null);
  const [dragging, setDragging] = useState(false);
  const full = useFullscreen(box);

  useEffect(() => { setNatural(null); setError(false); setZoom(FIT); setRotate(0); setMirror(false); setPan({ x: 0, y: 0 }); }, [url]);
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    setBoxSize({ w: el.clientWidth, h: el.clientHeight });
    if (typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => setBoxSize({ w: el.clientWidth, h: el.clientHeight }));
    ro.observe(el);
    setBoxSize({ w: el.clientWidth, h: el.clientHeight });
    return () => ro.disconnect();
  }, []);

  const turned = rotate % 180 !== 0;
  // 适合窗口：转过之后的宽高放进预览区，四周留 24px；小图不放大
  const fit = natural && boxSize.w ? Math.min(1, (boxSize.w - 48) / (turned ? natural.h : natural.w), (boxSize.h - 48) / (turned ? natural.w : natural.h)) : 1;
  const scale = fit * ZOOMS[zoom];
  const zoomTo = (z: number) => { const n = Math.max(0, Math.min(ZOOMS.length - 1, z)); setZoom(n); if (ZOOMS[n] <= 1) setPan({ x: 0, y: 0 }); };
  const reset = () => { setZoom(FIT); setPan({ x: 0, y: 0 }); };
  const rotateOnce = () => { setRotate((r) => (r + 90) % 360); setPan({ x: 0, y: 0 }); };

  usePreviewKeys(shortcuts, { '+': () => zoomTo(zoom + 1), '-': () => zoomTo(zoom - 1), '0': reset, r: rotateOnce, f: full.toggle });

  // 按住 ⌘ 或 Ctrl 滚轮缩放；普通滚轮留给页面滚动
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const h = (e: WheelEvent) => {
      if (!e.metaKey && !e.ctrlKey) return;
      e.preventDefault();
      setZoom((z) => { const n = Math.max(0, Math.min(ZOOMS.length - 1, z + (e.deltaY < 0 ? 1 : -1))); if (ZOOMS[n] <= 1) setPan({ x: 0, y: 0 }); return n; });
    };
    el.addEventListener('wheel', h, { passive: false });
    return () => el.removeEventListener('wheel', h);
  }, []);

  const canPan = ZOOMS[zoom] > 1;
  const onPointerDown = (e: React.PointerEvent) => {
    if (!canPan || e.button !== 0) return;
    drag.current = { x: e.clientX, y: e.clientY, px: pan.x, py: pan.y };
    setDragging(true);
    (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
  };
  const onPointerMove = (e: React.PointerEvent) => {
    const d = drag.current;
    if (d) setPan({ x: d.px + e.clientX - d.x, y: d.py + e.clientY - d.y });
  };
  const onPointerUp = () => { drag.current = null; setDragging(false); };

  if (error) return <PreviewUnavailable reason={t('图片读不出来，可能已损坏。')} onDownload={onDownload} />;
  return (
    <Box ref={box} sx={{ position: 'relative', height: height ?? { xs: 480, md: 'calc(100vh - 56px - 48px)' }, minHeight: 360, overflow: 'hidden', bgcolor: 'surface.containerLow',
      '&:fullscreen': { height: '100vh' } }}>
      {!natural && <Skeleton variant="rounded" sx={{ position: 'absolute', inset: 24 }} />}
      <Box component="img" src={url} alt={alt} draggable={false}
        onLoad={(e: React.SyntheticEvent<HTMLImageElement>) => setNatural({ w: e.currentTarget.naturalWidth, h: e.currentTarget.naturalHeight })}
        onError={() => setError(true)}
        onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={onPointerUp}
        // 尺寸和变换随时在变，用行内样式，不为每个值生成一个类
        style={{
          width: natural ? natural.w * scale : undefined, height: natural ? natural.h * scale : undefined, opacity: natural ? 1 : 0,
          transform: `translate(-50%, -50%) translate(${pan.x}px, ${pan.y}px) rotate(${rotate}deg) scaleX(${mirror ? -1 : 1})`,
        }}
        sx={{
          position: 'absolute', left: '50%', top: '50%', maxWidth: 'none', userSelect: 'none', touchAction: canPan ? 'none' : 'auto',
          transition: dragging ? 'none' : 'transform 150ms ease-out, width 150ms ease-out, height 150ms ease-out',
          cursor: canPan ? (dragging ? 'grabbing' : 'grab') : 'default', boxShadow: 1, bgcolor: '#fff',
          '@media (prefers-reduced-motion: reduce)': { transition: 'none' },
        }} />
      <Box sx={{ position: 'absolute', left: 0, right: 0, bottom: 16 }}>
        <FloatingBar>
          <BarButton icon={Minus} label={t('缩小')} keys="−" disabled={zoom === 0} onClick={() => zoomTo(zoom - 1)} />
          <BarText width={44}>{Math.round(scale * 100)}%</BarText>
          <BarButton icon={Plus} label={t('放大')} keys="+" disabled={zoom === ZOOMS.length - 1} onClick={() => zoomTo(zoom + 1)} />
          <BarButton icon={MoveHorizontal} label={t('适合窗口')} keys="0" disabled={zoom === FIT && !pan.x && !pan.y} onClick={reset} />
          <BarDivider />
          <BarButton icon={RotateCw} label={t('旋转')} keys="R" onClick={rotateOnce} />
          <BarButton icon={FlipHorizontal2} label={t('镜像')} active={mirror} onClick={() => setMirror((m) => !m)} />
          {full.supported && <BarButton icon={full.on ? Minimize : Maximize} label={full.on ? t('退出全屏') : t('全屏')} keys="F" onClick={full.toggle} />}
        </FloatingBar>
      </Box>
    </Box>
  );
}

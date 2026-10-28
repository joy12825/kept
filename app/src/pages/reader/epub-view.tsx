// EPUB 的分页阅读（epub.js）：整本书带 Cookie 取回后交给 epub.js；书里的脚本不执行（allowScriptedContent 关闭）。
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react';
import { Box, CircularProgress, Typography } from '@mui/material';
import ePub, { type Book, type Contents, type NavItem, type Rendition } from 'epubjs';
import { colorCss, type Highlight } from '../../api/reading';
import { t } from '../../i18n';

export type ReaderTheme = 'light' | 'sepia' | 'dark';

export const THEME_COLORS: Record<ReaderTheme, { fg: string; bg: string }> = {
  light: { fg: '#1f2328', bg: '#ffffff' },
  sepia: { fg: '#5b4636', bg: '#f4ecd8' },
  dark: { fg: '#d6d6d6', bg: '#1e1f22' },
};

export interface EpubHandle { next: () => void; prev: () => void; goto: (target: string) => void; clearSelection: () => void }

export interface TocItem { label: string; href: string; depth: number }

/** 屏幕坐标（弹出框的位置）。 */
export interface Point { x: number; y: number }

/** 书页里一个选区或元素的屏幕坐标：iframe 里的坐标加上 iframe 自己的位置。 */
function screenPoint(contents: Contents, rect: DOMRect): Point {
  const frame = contents.window.frameElement?.getBoundingClientRect();
  return { x: (frame?.left ?? 0) + rect.left + rect.width / 2, y: (frame?.top ?? 0) + rect.bottom };
}

function flatten(items: NavItem[], depth = 0): TocItem[] {
  return items.flatMap((i) => [{ label: i.label.trim(), href: i.href, depth }, ...flatten(i.subitems ?? [], depth + 1)]);
}

export const EpubView = forwardRef<EpubHandle, {
  url: string;
  initial?: string;
  theme: ReaderTheme;
  fontSize: number; // 百分比
  onLocation: (cfi: string, percent: number, label: string) => void;
  onToc: (toc: TocItem[]) => void;
  highlights?: Highlight[];
  /** 选中了一段文字 */
  onSelect?: (cfiRange: string, text: string, at: Point) => void;
  /** 点了一条已有的高亮 */
  onHighlight?: (id: number, at: Point) => void;
}>(function EpubView({ url, initial, theme, fontSize, onLocation, onToc, highlights, onSelect, onHighlight }, ref) {
  const el = useRef<HTMLDivElement>(null);
  const rendition = useRef<Rendition | null>(null);
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
  // 回调放进 ref：父组件每次渲染都是新函数，不能因此重建书
  const cb = useRef({ onLocation, onToc, onSelect, onHighlight });
  cb.current = { onLocation, onToc, onSelect, onHighlight };
  const bookRef = useRef<Book | null>(null);
  // 已画上的高亮：id → 位置与颜色，变了才重画
  const drawn = useRef(new Map<number, { cfi: string; color: string }>());

  useImperativeHandle(ref, () => ({
    next: () => void rendition.current?.next(),
    prev: () => void rendition.current?.prev(),
    goto: (target: string) => void rendition.current?.display(target),
    clearSelection: () => {
      for (const c of (rendition.current?.getContents() as unknown as Contents[] | undefined) ?? []) c.window.getSelection()?.removeAllRanges();
    },
  }), []);

  useEffect(() => {
    let cancelled = false;
    let book: Book | null = null;
    setState('loading');
    fetch(url, { credentials: 'include' })
      .then((r) => { if (!r.ok) throw new Error(String(r.status)); return r.arrayBuffer(); })
      .then(async (buf) => {
        if (cancelled || !el.current) return;
        book = ePub(buf);
        bookRef.current = book;
        drawn.current.clear();
        const r = book.renderTo(el.current, { width: '100%', height: '100%', flow: 'paginated', spread: 'none', allowScriptedContent: false });
        rendition.current = r;
        for (const [name, c] of Object.entries(THEME_COLORS)) {
          r.themes.register(name, { body: { color: `${c.fg} !important`, background: `${c.bg} !important` }, 'a, a:link': { color: 'inherit !important' } });
        }
        const b = book;
        let total = 0;
        r.on('relocated', (loc: { start: { cfi: string; percentage: number; href: string } }) => {
          const pct = total ? b.locations.percentageFromCfi(loc.start.cfi) : loc.start.percentage;
          const chapter = b.navigation?.get(loc.start.href)?.label?.trim() ?? '';
          cb.current.onLocation(loc.start.cfi, pct ?? 0, chapter);
        });
        r.on('selected', (cfiRange: string, contents: Contents) => {
          const sel = contents.window.getSelection();
          const text = sel?.toString().trim() ?? '';
          if (!text || !sel?.rangeCount) return;
          cb.current.onSelect?.(cfiRange, text, screenPoint(contents, sel.getRangeAt(0).getBoundingClientRect()));
        });
        await r.display(initial || undefined).catch(() => r.display());
        if (cancelled) return;
        setState('ready');
        void b.loaded.navigation.then((nav) => { if (!cancelled) cb.current.onToc(flatten(nav.toc)); });
        // 百分比按“位置”算才准；整本书分好位置要一会儿，分好后再报一次
        void b.locations.generate(1600).then(() => {
          total = b.locations.length();
          const cur = r.currentLocation() as unknown as { start?: { cfi: string; href: string } };
          if (!cancelled && cur?.start) cb.current.onLocation(cur.start.cfi, b.locations.percentageFromCfi(cur.start.cfi), b.navigation?.get(cur.start.href)?.label?.trim() ?? '');
        });
      })
      .catch(() => { if (!cancelled) setState('error'); });
    return () => { cancelled = true; rendition.current = null; bookRef.current = null; book?.destroy(); };
  }, [url]); // eslint-disable-line react-hooks/exhaustive-deps

  // 高亮：新的画上，删掉的擦掉，换了颜色的重画
  useEffect(() => {
    const r = rendition.current;
    if (!r || state !== 'ready') return;
    const want = new Map((highlights ?? []).filter((h) => h.locator.startsWith('epubcfi(')).map((h) => [h.id, { cfi: h.locator, color: h.color }]));
    for (const [id, d] of drawn.current) {
      const w = want.get(id);
      if (!w || w.color !== d.color) { try { r.annotations.remove(d.cfi, 'highlight'); } catch { /* 已不在 */ } drawn.current.delete(id); }
    }
    for (const [id, w] of want) {
      if (drawn.current.has(id)) continue;
      try {
        r.annotations.highlight(w.cfi, { id }, (e: MouseEvent) => {
          // 高亮画在书页外面的 SVG 上，坐标已经是本页的，不用加 iframe 的位置
          const b = (e.target as Element).getBoundingClientRect();
          cb.current.onHighlight?.(id, { x: b.left + b.width / 2, y: b.bottom });
        }, 'kept-highlight', { fill: colorCss(w.color as Highlight['color']), 'fill-opacity': '1', 'mix-blend-mode': 'multiply' });
        drawn.current.set(id, w);
      } catch { /* 位置在这本书里找不到：跳过 */ }
    }
  }, [highlights, state]);

  useEffect(() => { rendition.current?.themes.select(theme); }, [theme, state]);
  useEffect(() => { rendition.current?.themes.fontSize(`${fontSize}%`); }, [fontSize, state]);

  return (
    <Box sx={{ position: 'relative', width: '100%', height: '100%' }}>
      <Box ref={el} data-testid="epub-area" sx={{ width: '100%', height: '100%' }} />
      {state === 'loading' && <Box sx={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center' }}><CircularProgress size={24} /></Box>}
      {state === 'error' && <Typography sx={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'text.secondary' }}>{t('这本书无法打开，可以下载原件用其他阅读器查看。')}</Typography>}
    </Box>
  );
});

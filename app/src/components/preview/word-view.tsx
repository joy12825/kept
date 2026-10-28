// Word 阅读视图（v1.9.8）：服务器把 docx 读成段落、表格、文本框，这里按屏幕宽度排版，不分页。
// 只生成 React 元素，不写 HTML；链接只认 http、https、mailto（服务器已经筛过，这里再核对一次）。版式以可搜索版为准。
import { useMemo } from 'react';
import { Alert, Box, Link, Skeleton, Stack, Typography, useTheme } from '@mui/material';
import { alpha } from '@mui/material/styles';
import { useWord, wordImageUrl, type WordBlock, type WordDocument, type WordRun } from '../../api/formats';
import type { FindState } from '../find-bar';
import { PreviewUnavailable } from './unavailable';
import { Marked, useSegmentFind, type Hit } from './highlight';
import { t } from '../../i18n';

const HEADING = [
  { fontSize: 26, lineHeight: '36px', mt: 4 }, { fontSize: 21, lineHeight: '30px', mt: 3.5 }, { fontSize: 18, lineHeight: '27px', mt: 3 },
  { fontSize: 16, lineHeight: '25px', mt: 2.5 }, { fontSize: 16, lineHeight: '25px', mt: 2 }, { fontSize: 15, lineHeight: '24px', mt: 2 },
];

const safe = (u?: string) => !!u && /^(https?:|mailto:)/i.test(u);

/** 每段文字的编号（查找按它分段）：按显示的顺序，正文在前、脚注在后。 */
function indexTexts(doc: WordDocument) {
  const ids = new Map<WordRun, number>();
  const texts: string[] = [];
  const walk = (blocks: WordBlock[]) => {
    for (const b of blocks) {
      for (const r of b.runs ?? []) if (r.text) { ids.set(r, texts.length); texts.push(r.text); }
      for (const row of b.rows ?? []) for (const c of row) walk(c.blocks);
      if (b.blocks) walk(b.blocks);
    }
  };
  walk(doc.blocks);
  for (const n of doc.notes) walk(n.blocks);
  return { ids, texts };
}

interface Ctx {
  docId: number;
  doc: WordDocument;
  mark: (r: WordRun) => React.ReactNode;
  dark: boolean;
  noteRefs: Set<number>;
}

export function WordView({ docId, find, onDownload }: { docId: number; find?: FindState; onDownload: () => void }) {
  const q = useWord(docId);
  if (q.isError) return <PreviewUnavailable reason={q.error.message} onDownload={onDownload} />;
  if (!q.data) {
    return (
      <Stack spacing={1.5} sx={{ maxWidth: 760, mx: 'auto', px: { xs: 2.5, md: 4 }, py: 3 }} aria-label={t('正在读取')}>
        <Skeleton variant="text" width="60%" height={40} />
        {[0, 1, 2, 3, 4].map((i) => <Skeleton key={i} variant="text" />)}
      </Stack>
    );
  }
  return <WordBody docId={docId} doc={q.data} find={find} />;
}

function WordBody({ docId, doc, find }: { docId: number; doc: WordDocument; find?: FindState }) {
  const { ids, texts } = useMemo(() => indexTexts(doc), [doc]);
  const { box, hitsOf, current } = useSegmentFind(texts, find);
  const dark = useTheme().palette.mode === 'dark';
  const ctx: Ctx = {
    docId, doc, dark, noteRefs: new Set(),
    mark: (r) => {
      const id = ids.get(r);
      return id === undefined ? r.text : <Marked text={r.text ?? ''} hits={hitsOf(id) as Hit[]} current={current} />;
    },
  };
  const empty = !doc.blocks.length;
  return (
    <Box ref={box} data-testid="word" sx={{ maxWidth: 760, mx: 'auto', px: { xs: 2.5, md: 4 }, py: 3, fontSize: 16, lineHeight: 1.75, overflowWrap: 'anywhere' }}>
      <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary', mb: 2 }}>{t('阅读视图：按屏幕宽度排版，版式以可搜索版为准')}</Typography>
      {doc.truncated && <Alert severity="info" sx={{ mb: 2 }}>{t('文档太长，只显示了前面的部分；全文请看可搜索版。')}</Alert>}
      {empty ? <Typography sx={{ color: 'text.secondary' }}>{t('这份文档没有文字。')}</Typography> : <Blocks blocks={doc.blocks} ctx={ctx} />}
      {doc.notes.length > 0 && (
        <Box component="section" aria-label={t('脚注')} sx={{ mt: 4, pt: 2, borderTop: 1, borderColor: 'divider', fontSize: 14, lineHeight: 1.7, color: 'text.secondary' }}>
          {doc.notes.map((n) => (
            <Stack key={n.n} id={`word-note-${n.n}`} direction="row" spacing={1} sx={{ mb: 1, scrollMarginTop: 80 }}>
              <Link component="button" type="button" underline="hover" aria-label={t('回到正文的第 {n} 处引用', { n: n.n })}
                onClick={() => document.getElementById(`word-ref-${n.n}`)?.scrollIntoView({ block: 'center' })}
                sx={{ flex: 'none', fontSize: 13, fontVariantNumeric: 'tabular-nums', alignSelf: 'flex-start', mt: '2px' }}>{n.n}</Link>
              <Box sx={{ minWidth: 0, '& p': { mb: 0.5 } }}><Blocks blocks={n.blocks} ctx={ctx} tight /></Box>
            </Stack>
          ))}
        </Box>
      )}
    </Box>
  );
}

function Blocks({ blocks, ctx, tight }: { blocks: WordBlock[]; ctx: Ctx; tight?: boolean }) {
  return <>{blocks.map((b, i) => <Block key={i} b={b} ctx={ctx} tight={tight} first={i === 0} />)}</>;
}

function Block({ b, ctx, tight, first }: { b: WordBlock; ctx: Ctx; tight?: boolean; first?: boolean }) {
  if (b.kind === 'table') return <Table b={b} ctx={ctx} />;
  if (b.kind === 'box') {
    return (
      <Box sx={{ my: 2, px: 2, py: 1.5, borderRadius: 1.5, border: 1, borderColor: 'divider', bgcolor: 'surface.containerLow', '& > :last-child': { mb: 0 } }}>
        <Blocks blocks={b.blocks ?? []} ctx={ctx} tight />
      </Box>
    );
  }
  const runs = <Runs runs={b.runs ?? []} ctx={ctx} heading={!!b.heading} />;
  const align = b.align === 'justify' ? 'justify' : b.align;
  if (b.heading) {
    const h = HEADING[b.heading - 1];
    return (
      <Typography component={`h${b.heading}` as 'h1'} sx={{ fontSize: h.fontSize, lineHeight: h.lineHeight, fontWeight: 600, mt: first ? 0 : h.mt, mb: 1.5, textAlign: align, whiteSpace: 'pre-wrap' }}>
        {runs}
      </Typography>
    );
  }
  if (b.list) {
    return (
      <Box sx={{ display: 'flex', gap: 1, pl: b.list.level * 3, mb: tight ? 0.25 : 0.75 }}>
        {b.list.marker && <Box component="span" aria-hidden sx={{ flex: 'none', minWidth: '1.5em', fontVariantNumeric: 'tabular-nums' }}>{b.list.marker}</Box>}
        <Box component="p" sx={{ m: 0, minWidth: 0, flex: 1, whiteSpace: 'pre-wrap', textAlign: align }}>{runs}</Box>
      </Box>
    );
  }
  if (!(b.runs ?? []).length) return <Box aria-hidden sx={{ height: tight ? 4 : 12 }} />;
  return <Box component="p" sx={{ m: 0, mb: tight ? 0.5 : 1.5, whiteSpace: 'pre-wrap', textAlign: align }}>{runs}</Box>;
}

/** 文档里写的颜色：深色模式下太暗的字色不用（会看不见），突出显示改成半透明底。 */
function luminance(hex: string) {
  const n = parseInt(hex, 16);
  const c = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => { const x = v / 255; return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4; });
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
}

function Runs({ runs, ctx, heading }: { runs: WordRun[]; ctx: Ctx; heading: boolean }) {
  return (
    <>
      {runs.map((r, i) => {
        if (r.image) {
          const im = ctx.doc.images[r.image - 1];
          return (
            <Box key={i} component="img" src={wordImageUrl(ctx.docId, r.image)} alt={t('图片 {n}', { n: r.image })} loading="lazy"
              width={im?.width || undefined} height={im?.height || undefined}
              sx={{ maxWidth: '100%', height: 'auto', verticalAlign: 'middle', borderRadius: 0.5, my: 0.5 }} />
          );
        }
        if (r.note) {
          const first = !ctx.noteRefs.has(r.note);
          ctx.noteRefs.add(r.note);
          return (
            <Box key={i} component="sup" sx={{ lineHeight: 0 }}>
              <Link component="button" type="button" id={first ? `word-ref-${r.note}` : undefined} underline="hover" aria-label={t('脚注 {n}', { n: r.note })}
                onClick={() => document.getElementById(`word-note-${r.note}`)?.scrollIntoView({ block: 'center' })}
                sx={{ fontSize: '0.75em', verticalAlign: 'baseline', px: 0.25, scrollMarginTop: 80 }}>[{r.note}]</Link>
            </Box>
          );
        }
        const color = r.color && !(ctx.dark && luminance(r.color) < 0.2) ? `#${r.color}` : undefined;
        const sx = {
          fontWeight: r.bold ? 600 : undefined,
          fontStyle: r.italic ? 'italic' : undefined,
          textDecoration: [r.underline && 'underline', r.strike && 'line-through'].filter(Boolean).join(' ') || undefined,
          color,
          bgcolor: r.highlight ? (ctx.dark ? alpha(`#${r.highlight}`, 0.35) : `#${r.highlight}`) : undefined,
          fontSize: r.size && !heading ? `${Math.min(Math.max(r.size / ctx.doc.baseSize, 0.7), 2.2)}em` : undefined,
        };
        const body = ctx.mark(r);
        const inner = r.vert ? <Box component={r.vert === 'super' ? 'sup' : 'sub'} sx={{ lineHeight: 0 }}>{body}</Box> : body;
        if (safe(r.link)) {
          return <Link key={i} href={r.link} target="_blank" rel="noopener noreferrer" sx={{ ...sx, color: undefined }}>{inner}</Link>;
        }
        return <Box key={i} component="span" sx={sx}>{inner}</Box>;
      })}
    </>
  );
}

function Table({ b, ctx }: { b: WordBlock; ctx: Ctx }) {
  const header = b.headerRows ?? 0;
  return (
    <Box sx={{ my: 2, overflowX: 'auto' }}>
      <Box component="table" sx={{ borderCollapse: 'collapse', minWidth: '60%', fontSize: 15, lineHeight: 1.6 }}>
        <tbody>
          {(b.rows ?? []).map((row, i) => (
            <tr key={i}>
              {row.map((c, j) => (
                <Box key={j} component={i < header ? 'th' : 'td'} colSpan={c.colSpan > 1 ? c.colSpan : undefined} rowSpan={c.rowSpan > 1 ? c.rowSpan : undefined}
                  sx={{ border: 1, borderColor: 'divider', px: 1.25, py: 0.75, verticalAlign: 'top', textAlign: 'left', fontWeight: i < header ? 600 : undefined,
                    bgcolor: i < header ? 'surface.containerLow' : undefined, '& p:last-child': { mb: 0 } }}>
                  <Blocks blocks={c.blocks} ctx={ctx} tight />
                </Box>
              ))}
            </tr>
          ))}
        </tbody>
      </Box>
    </Box>
  );
}

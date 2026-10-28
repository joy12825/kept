// Markdown 显示（v1.9.1）：把 markdown.ts 的语法树画成 React 元素。链接在新窗口打开；图片不加载外部地址，显示为链接。
import { Fragment, useMemo } from 'react';
import { Box, Link, Typography } from '@mui/material';
import { Image as ImageIcon, Square, SquareCheck } from 'lucide-react';
import type { FindState } from '../find-bar';
import { SW } from '../icons';
import { useSegmentFind, Marked, type Hit } from './highlight';
import { parseMarkdown, type Block, type Inline } from './markdown';
import { t } from '../../i18n';

const mono = 'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace';
const HEADING = [
  { fontSize: 24, lineHeight: '32px', mt: 4 }, { fontSize: 20, lineHeight: '28px', mt: 3.5 }, { fontSize: 18, lineHeight: '26px', mt: 3 },
  { fontSize: 16, lineHeight: '24px', mt: 2.5 }, { fontSize: 15, lineHeight: '24px', mt: 2 }, { fontSize: 14, lineHeight: '22px', mt: 2 },
];

export function MarkdownView({ source, find }: { source: string; find?: FindState }) {
  const doc = useMemo(() => parseMarkdown(source), [source]);
  const { box, hitsOf, current } = useSegmentFind(doc.texts, find);
  const mark = (id: number, text: string) => <Marked text={text} hits={hitsOf(id)} current={current} />;
  return (
    <Box ref={box} data-testid="markdown" sx={{ maxWidth: 760, mx: 'auto', px: { xs: 2.5, md: 4 }, py: 3, fontSize: 15, lineHeight: '26px', overflowWrap: 'anywhere',
      '& > :first-of-type': { mt: 0 } }}>
      <Blocks blocks={doc.blocks} mark={mark} />
    </Box>
  );
}

type MarkFn = (id: number, text: string) => React.ReactNode;

function Blocks({ blocks, mark, tight }: { blocks: Block[]; mark: MarkFn; tight?: boolean }) {
  return <>{blocks.map((b, i) => <BlockView key={i} b={b} mark={mark} tight={tight} />)}</>;
}

function BlockView({ b, mark, tight }: { b: Block; mark: MarkFn; tight?: boolean }) {
  switch (b.t) {
    case 'heading': {
      const h = HEADING[b.level - 1];
      return (
        <Typography component={`h${b.level}` as 'h1'} sx={{ fontSize: h.fontSize, lineHeight: h.lineHeight, fontWeight: 600, mt: h.mt, mb: 1.5,
          ...(b.level <= 2 ? { pb: 1, borderBottom: 1, borderColor: 'divider' } : {}) }}>
          <Inlines items={b.children} mark={mark} />
        </Typography>
      );
    }
    case 'paragraph':
      return <Box component="p" sx={{ m: 0, mb: tight ? 0.5 : 2, whiteSpace: 'pre-line' }}><Inlines items={b.children} mark={mark} /></Box>;
    case 'code':
      return (
        <Box component="pre" sx={{ m: 0, mb: 2, p: 2, borderRadius: 1.5, bgcolor: 'surface.containerLow', border: 1, borderColor: 'divider', overflowX: 'auto',
          fontFamily: mono, fontSize: 13, lineHeight: '20px' }}>
          <code>{mark(b.id, b.text)}</code>
        </Box>
      );
    case 'quote':
      return <Box component="blockquote" sx={{ m: 0, mb: 2, pl: 2, borderLeft: 3, borderColor: 'divider', color: 'text.secondary' }}><Blocks blocks={b.children} mark={mark} /></Box>;
    case 'hr':
      return <Box component="hr" sx={{ border: 0, borderTop: 1, borderColor: 'divider', my: 3 }} />;
    case 'list': {
      const task = b.items.some((it) => it.checked !== null);
      return (
        <Box component={b.ordered ? 'ol' : 'ul'} start={b.ordered && b.start !== 1 ? b.start : undefined}
          sx={{ m: 0, mb: 2, pl: task ? 0.5 : 3, listStyle: task ? 'none' : undefined, '& > li': { mb: 0.5 }, '& ul, & ol': { mb: 0 } }}>
          {b.items.map((it, i) => (
            <li key={i}>
              {it.checked !== null
                ? <Box sx={{ display: 'flex', gap: 1, alignItems: 'flex-start' }}>
                  <Box component="span" role="img" aria-label={it.checked ? t('已完成') : t('未完成')} sx={{ mt: '5px', color: it.checked ? 'primary.main' : 'text.secondary', display: 'flex' }}>
                    {it.checked ? <SquareCheck size={16} strokeWidth={SW} /> : <Square size={16} strokeWidth={SW} />}
                  </Box>
                  <Box sx={{ minWidth: 0 }}><Blocks blocks={it.children} mark={mark} tight /></Box>
                </Box>
                : <Blocks blocks={it.children} mark={mark} tight />}
            </li>
          ))}
        </Box>
      );
    }
    case 'table':
      return (
        <Box sx={{ mb: 2, overflowX: 'auto' }}>
          <Box component="table" sx={{ borderCollapse: 'collapse', fontSize: 14, lineHeight: '22px', '& th, & td': { border: 1, borderColor: 'divider', px: 1.5, py: 0.75 }, '& th': { fontWeight: 600, bgcolor: 'surface.containerLow' } }}>
            <thead><tr>{b.head.map((c, i) => <th key={i} style={{ textAlign: b.align[i] ?? 'left' }}><Inlines items={c} mark={mark} /></th>)}</tr></thead>
            <tbody>{b.rows.map((r, i) => <tr key={i}>{r.map((c, k) => <td key={k} style={{ textAlign: b.align[k] ?? 'left' }}><Inlines items={c} mark={mark} /></td>)}</tr>)}</tbody>
          </Box>
        </Box>
      );
  }
}

function Inlines({ items, mark }: { items: Inline[]; mark: MarkFn }) {
  return <>{items.map((x, i) => <Fragment key={i}><InlineView x={x} mark={mark} /></Fragment>)}</>;
}

function InlineView({ x, mark }: { x: Inline; mark: MarkFn }) {
  switch (x.t) {
    case 'text': return <>{mark(x.id, x.text)}</>;
    case 'code':
      return <Box component="code" sx={{ fontFamily: mono, fontSize: '0.875em', px: 0.5, py: '1px', borderRadius: 0.75, bgcolor: 'surface.containerLow', border: 1, borderColor: 'divider' }}>{mark(x.id, x.text)}</Box>;
    case 'strong': return <Box component="strong" sx={{ fontWeight: 600 }}><Inlines items={x.children} mark={mark} /></Box>;
    case 'em': return <em><Inlines items={x.children} mark={mark} /></em>;
    case 'del': return <del><Inlines items={x.children} mark={mark} /></del>;
    case 'br': return <br />;
    case 'link':
      return x.href
        ? <Link href={x.href} target="_blank" rel="noopener noreferrer nofollow"><Inlines items={x.children} mark={mark} /></Link>
        : <Inlines items={x.children} mark={mark} />;
    case 'image': {
      const label = <Box component="span" sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5 }}><ImageIcon size={14} strokeWidth={SW} />
        {t('图片：')}{mark(x.id, x.alt || t('未命名'))}</Box>;
      return x.href ? <Link href={x.href} target="_blank" rel="noopener noreferrer nofollow">{label}</Link> : label;
    }
  }
}

export type { Hit };

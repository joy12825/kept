// 分成多段显示的文字（Markdown 的各个文字节点、代码的各行）里查找（v1.9.1）：
// 每段各自找出结果，按出现顺序统一编号，报回总数，当前那一处滚到中间。
import { Fragment, useEffect, useMemo, useRef } from 'react';
import { Box } from '@mui/material';
import { findMatches } from '../../utils/find';
import type { FindState } from '../find-bar';
import { HIT, HIT_CURRENT } from '../find-text';

export interface Hit { start: number; end: number; n: number }

export function useSegmentFind(segments: string[], find?: FindState) {
  const terms = find?.terms;
  const bySegment = useMemo(() => {
    const out: Hit[][] = [];
    let n = 0;
    for (const s of segments) {
      const ms = terms?.length ? findMatches(s, terms) : [];
      out.push(ms.map((m) => ({ ...m, n: n++ })));
    }
    return { hits: out, total: n };
  }, [segments, terms]);
  const box = useRef<HTMLDivElement>(null);
  const onCount = find?.onCount;
  useEffect(() => { if (terms?.length) onCount?.(bySegment.total); }, [bySegment, onCount, terms]);
  useEffect(() => {
    if (!find || !bySegment.total) return;
    box.current?.querySelector('[data-find-current]')?.scrollIntoView?.({ block: 'center' });
  }, [find?.seq, bySegment]); // eslint-disable-line react-hooks/exhaustive-deps
  const current = find && bySegment.total ? Math.min(find.current, bySegment.total - 1) : -1;
  return { box, hitsOf: (i: number) => bySegment.hits[i] ?? [], current };
}

/** 一段文字，标出其中的查找结果 */
export function Marked({ text, hits, current }: { text: string; hits: Hit[]; current: number }) {
  if (!hits.length) return <>{text}</>;
  const parts: React.ReactNode[] = [];
  let at = 0;
  for (const h of hits) {
    parts.push(<Fragment key={`t${h.n}`}>{text.slice(at, h.start)}</Fragment>);
    const cur = h.n === current;
    parts.push(
      <Box component="mark" key={`m${h.n}`} data-find-current={cur || undefined}
        sx={{ bgcolor: cur ? HIT_CURRENT : HIT, color: 'inherit', borderRadius: '2px' }}>{text.slice(h.start, h.end)}</Box>,
    );
    at = h.end;
  }
  parts.push(<Fragment key="end">{text.slice(at)}</Fragment>);
  return <>{parts}</>;
}

// 文字里的查找结果（v1.5.4）：识别文字和纯文本文档按查找框标出每一处，当前那一处颜色更深并滚到中间。
import { Fragment, useEffect, useMemo, useRef } from 'react';
import { Box } from '@mui/material';
import { findMatches } from '../utils/find';
import type { FindState } from './find-bar';

export const HIT = 'rgba(255, 200, 0, 0.45)';
export const HIT_CURRENT = 'rgba(255, 120, 0, 0.6)';

export function FindText({ text, find }: { text: string; find?: FindState }) {
  const matches = useMemo(() => (find ? findMatches(text, find.terms) : []), [text, find?.terms]); // eslint-disable-line react-hooks/exhaustive-deps
  const box = useRef<HTMLSpanElement>(null);
  const onCount = find?.onCount;
  useEffect(() => { onCount?.(matches.length); }, [matches, onCount]);
  useEffect(() => {
    if (!find || !matches.length) return;
    box.current?.querySelector('[data-find-current]')?.scrollIntoView?.({ block: 'center' });
  }, [find?.seq, matches]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!matches.length) return <>{text}</>;
  const parts: React.ReactNode[] = [];
  let at = 0;
  matches.forEach((m, i) => {
    parts.push(<Fragment key={`t${i}`}>{text.slice(at, m.start)}</Fragment>);
    const cur = i === find!.current;
    parts.push(
      <Box component="mark" key={`m${i}`} data-find-current={cur || undefined}
        sx={{ bgcolor: cur ? HIT_CURRENT : HIT, color: 'inherit', borderRadius: '2px' }}>{text.slice(m.start, m.end)}</Box>,
    );
    at = m.end;
  });
  parts.push(<Fragment key="end">{text.slice(at)}</Fragment>);
  return <span ref={box}>{parts}</span>;
}

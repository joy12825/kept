// 代码和数据文件（v1.9.1）：等宽字体，左侧行号；长行横向滚动。查找照常。
import { useMemo } from 'react';
import { Box } from '@mui/material';
import type { FindState } from '../find-bar';
import { Marked, useSegmentFind } from './highlight';

const mono = 'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace';

export function CodeView({ source, find }: { source: string; find?: FindState }) {
  const lines = useMemo(() => source.replace(/\r\n?/g, '\n').replace(/\n$/, '').split('\n'), [source]);
  const { box, hitsOf, current } = useSegmentFind(lines, find);
  const digits = String(lines.length).length;
  return (
    <Box ref={box} data-testid="code" sx={{ py: 2, overflowX: 'auto', fontFamily: mono, fontSize: 13, lineHeight: '20px' }}>
      <Box component="table" sx={{ borderCollapse: 'collapse', minWidth: '100%' }}>
        <tbody>
          {lines.map((l, i) => (
            <tr key={i}>
              <Box component="td" aria-hidden sx={{ userSelect: 'none', textAlign: 'right', color: 'text.disabled', pl: 2, pr: 2, verticalAlign: 'top', width: `${digits + 4}ch`, position: 'sticky', left: 0, bgcolor: 'surface.containerLow' }}>{i + 1}</Box>
              <Box component="td" sx={{ whiteSpace: 'pre', pr: 3 }}>{l ? <Marked text={l} hits={hitsOf(i)} current={current} /> : '​'}</Box>
            </tr>
          ))}
        </tbody>
      </Box>
    </Box>
  );
}

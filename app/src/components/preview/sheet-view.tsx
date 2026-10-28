// 表格视图（v1.9.3）：Excel 和 CSV 的单元格。像表格软件一样，上面是列号、左边是行号，都固定不动；
// 只画看得见的行（最多 10000 行）；合并的单元格照样合并；多张表在底部切换。
// v1.11.4：排序和筛选（sheet-sort.ts）：点列头排序、勾选要看的值，搜索框找含某段文字的行；标题行固定在上面。
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  Alert, Box, Button, Checkbox, Divider, FormControlLabel, InputAdornment, List, ListItemButton, ListItemText, MenuItem, Popover, Skeleton, Stack, Tab, Tabs, TextField, Typography,
} from '@mui/material';
import { ArrowDownNarrowWide, ArrowUpNarrowWide, Filter, Search } from 'lucide-react';
import { useSheets, type Sheet } from '../../api/formats';
import { PreviewUnavailable } from './unavailable';
import { columnValues, findHeader, isActive, viewRows, type SheetViewState } from './sheet-sort';
import { t } from '../../i18n';

const ROW_H = 28;
const OVERSCAN = 12;
const HEAD_H = 28;
const MAX_VALUES = 500;

/** 列名：A…Z、AA… */
export function colName(i: number): string {
  let s = '';
  for (let n = i + 1; n > 0; n = Math.floor((n - 1) / 26)) s = String.fromCharCode(65 + ((n - 1) % 26)) + s;
  return s;
}

/** 按前 200 行的内容估算列宽：中文字算两个 */
function widths(rows: string[][], cols: number): number[] {
  const out = Array.from({ length: cols }, () => 64);
  for (const r of rows.slice(0, 200)) {
    r.forEach((v, c) => {
      let w = 0;
      for (const ch of v.split('\n')[0] ?? '') w += ch.charCodeAt(0) > 0x2e80 ? 2 : 1;
      out[c] = Math.max(out[c], Math.min(320, w * 7 + 20));
    });
  }
  return out;
}

const columns = (s: Sheet) => Math.max(1, ...s.rows.map((r) => r.length), ...s.merges.map((m) => m[3] + 1));

const fresh = (s: Sheet): SheetViewState => ({ header: findHeader(s.rows, s.merges, columns(s)), sort: null, filters: {}, query: '' });

export function SheetView({ docId, onDownload, height = 'calc(100vh - 56px - 48px)' }: { docId: number; onDownload?: () => void; height?: string }) {
  const q = useSheets(docId);
  const [tab, setTab] = useState(0);
  // 每张表各自的排序和筛选
  const [views, setViews] = useState<Record<number, SheetViewState>>({});
  if (q.isPending) return <Stack spacing={1} sx={{ p: 3 }}>{[0, 1, 2, 3, 4].map((i) => <Skeleton key={i} variant="rounded" height={22} />)}</Stack>;
  if (q.error) return <PreviewUnavailable reason={q.error.message} onDownload={onDownload} />;
  const sheets = q.data.sheets;
  const at = Math.min(tab, sheets.length - 1);
  const sheet = sheets[at];
  const view = views[at] ?? fresh(sheet);
  const setView = (v: SheetViewState) => setViews((m) => ({ ...m, [at]: v }));
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height, minHeight: 360 }}>
      {sheet.truncated && <Alert severity="info" sx={{ borderRadius: 0 }}>{t('表格太大，只显示前 10000 行、200 列。完整内容请下载原件。')}</Alert>}
      {sheet.rows.length > 1 && <Toolbar sheet={sheet} view={view} setView={setView} />}
      <Grid key={at} sheet={sheet} view={view} setView={setView} />
      {sheets.length > 1 && (
        <Tabs value={at} onChange={(_, v) => setTab(v)} variant="scrollable" aria-label={t('工作表')}
          sx={{ minHeight: 36, borderTop: 1, borderColor: 'divider', bgcolor: 'background.paper', '& .MuiTab-root': { minHeight: 36, py: 0, fontSize: 13 } }}>
          {sheets.map((s, i) => <Tab key={i} label={s.name || t('表 {n}', { n: i + 1 })} />)}
        </Tabs>
      )}
    </Box>
  );
}

/** 搜索框、标题行、显示几行、清除 */
function Toolbar({ sheet, view, setView }: { sheet: Sheet; view: SheetViewState; setView: (v: SheetViewState) => void }) {
  const { data, total } = useMemo(() => viewRows(sheet.rows, view), [sheet.rows, view]);
  const active = isActive(view);
  return (
    <Stack direction="row" spacing={1.5} useFlexGap sx={{ px: 1.5, py: 1, alignItems: 'center', flexWrap: 'wrap', borderBottom: 1, borderColor: 'divider', bgcolor: 'background.paper' }}>
      <TextField size="small" placeholder={t('在表里找')} value={view.query} onChange={(e) => setView({ ...view, query: e.target.value })}
        slotProps={{ htmlInput: { 'aria-label': t('在表里找') }, input: { startAdornment: <InputAdornment position="start"><Search size={16} /></InputAdornment> } }} sx={{ width: 200 }} />
      <TextField select size="small" label={t('标题行')} value={view.header} sx={{ width: 120 }}
        onChange={(e) => setView({ ...view, header: Number(e.target.value), sort: null, filters: {} })}>
        <MenuItem value={-1}>{t('无')}</MenuItem>
        {Array.from({ length: Math.min(5, sheet.rows.length - 1) }, (_, i) => <MenuItem key={i} value={i}>{t('第 {n} 行', { n: i + 1 })}</MenuItem>)}
      </TextField>
      <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('显示 {shown} / {total} 行', { shown: data.length, total })}</Typography>
      {active && <Button size="small" onClick={() => setView({ ...view, sort: null, filters: {}, query: '' })}>{t('清除排序和筛选')}</Button>}
      {active && sheet.merges.length > 0 && <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('排序或筛选时，合并的单元格不再合并')}</Typography>}
    </Stack>
  );
}

function Grid({ sheet, view, setView }: { sheet: Sheet; view: SheetViewState; setView: (v: SheetViewState) => void }) {
  const box = useRef<HTMLDivElement>(null);
  const [top, setTop] = useState(0);
  const [viewH, setViewH] = useState(600);
  const [menu, setMenu] = useState<{ col: number; el: HTMLElement } | null>(null);
  const rows = sheet.rows;
  const cols = columns(sheet);
  const w = useMemo(() => widths(rows, cols), [rows, cols]);
  const active = isActive(view);
  const { fixed, data: rest, footer } = useMemo(() => viewRows(rows, view), [rows, view]);
  // 合计行跟在数据行后面
  const data = useMemo(() => (rest.length ? [...rest, ...footer] : rest), [rest, footer]);
  // 合并：左上角那格带上跨几行几列，被盖住的格子不画内容；排序、筛选时不合并
  const { spans, covered } = useMemo(() => {
    const spans = new Map<string, [number, number]>();
    const covered = new Set<string>();
    if (active) return { spans, covered };
    for (const [r1, c1, r2, c2] of sheet.merges) {
      spans.set(`${r1}:${c1}`, [r2 - r1 + 1, c2 - c1 + 1]);
      for (let r = r1; r <= r2; r++) for (let c = c1; c <= c2; c++) if (r !== r1 || c !== c1) covered.add(`${r}:${c}`);
    }
    return { spans, covered };
  }, [sheet.merges, active]);

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    setViewH(el.clientHeight || 600);
    if (typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => setViewH(el.clientHeight));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  if (!rows.length) return <Box sx={{ p: 4, color: 'text.secondary', textAlign: 'center', flex: 1 }}>{t('这张表是空的。')}</Box>;
  const fixedH = fixed.length * ROW_H;
  const first = Math.max(0, Math.floor(Math.max(0, top - fixedH) / ROW_H) - OVERSCAN);
  const last = Math.min(data.length, Math.ceil((top + viewH) / ROW_H) + OVERSCAN);
  const rowNoW = Math.max(40, String(rows.length).length * 8 + 20);
  const cellSx = { height: ROW_H, px: 1, borderRight: 1, borderBottom: 1, borderColor: 'divider', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' } as const;
  const headSx = { position: 'sticky', top: 0, zIndex: 2, height: HEAD_H, bgcolor: 'surface.containerLow', color: 'text.secondary', fontWeight: 400, fontSize: 12, textAlign: 'center', borderRight: 1, borderBottom: 1, borderColor: 'divider' } as const;
  const sortable = rows.length > view.header + 2; // 至少两行数据
  const headerText = (c: number) => (view.header >= 0 ? rows[view.header][c] ?? '' : '');

  const row = (ri: number, stickyAt?: number) => {
    const r = rows[ri];
    const isHeader = ri === view.header;
    return (
      <Box component="tr" key={ri} aria-rowindex={ri + 1}
        sx={stickyAt !== undefined ? { '& > *': { position: 'sticky', top: HEAD_H + stickyAt * ROW_H, zIndex: 1, bgcolor: 'background.paper' } } : undefined}>
        <Box component="th" scope="row" sx={{ ...headSx, top: stickyAt !== undefined ? HEAD_H + stickyAt * ROW_H : 'auto', position: 'sticky', left: 0, zIndex: stickyAt !== undefined ? 3 : 1, height: ROW_H }}>{ri + 1}</Box>
        {Array.from({ length: cols }, (_, c) => {
          const key = `${ri}:${c}`;
          if (covered.has(key)) {
            // 被合并盖住的格子：只在横向合并时省掉，竖向合并留空格并去掉上边线
            const sameRowOrigin = sheet.merges.some(([r1, c1, , c2]) => r1 === ri && c > c1 && c <= c2);
            if (sameRowOrigin) return null;
            return <Box component="td" key={c} sx={{ ...cellSx, borderTopColor: 'transparent' }} />;
          }
          const span = spans.get(key);
          const v = r[c] ?? '';
          const num = /^-?[\d,]+(\.\d+)?%?$/.test(v);
          return (
            <Box component="td" key={c} colSpan={span?.[1]} title={v.length > 20 ? v : undefined}
              sx={{ ...cellSx, textAlign: span ? 'center' : num ? 'right' : 'left', fontWeight: isHeader ? 500 : undefined }}>{v}</Box>
          );
        })}
      </Box>
    );
  };

  return (
    <Box ref={box} onScroll={(e) => setTop((e.target as HTMLDivElement).scrollTop)} sx={{ flex: 1, overflow: 'auto', bgcolor: 'background.paper', minHeight: 0 }}>
      <Box component="table" role="grid" aria-rowcount={rows.length} sx={{ borderCollapse: 'separate', borderSpacing: 0, tableLayout: 'fixed', fontSize: 13, lineHeight: `${ROW_H - 1}px`, fontVariantNumeric: 'tabular-nums' }}>
        <colgroup>
          <col style={{ width: rowNoW }} />
          {w.map((x, i) => <col key={i} style={{ width: x }} />)}
        </colgroup>
        <thead>
          <tr>
            <Box component="th" sx={{ ...headSx, left: 0, zIndex: 3 }} />
            {w.map((_, c) => {
              const sorted = view.sort?.col === c ? view.sort.dir : null;
              const filtered = view.filters[c] !== undefined;
              const name = headerText(c) || colName(c);
              return (
                <Box component="th" key={c} scope="col" sx={{ ...headSx, p: 0 }}>
                  {sortable ? (
                    <Box component="button" type="button" onClick={(e: React.MouseEvent<HTMLElement>) => setMenu({ col: c, el: e.currentTarget })}
                      aria-label={t('{name} 列：排序和筛选', { name })}
                      sx={{ all: 'unset', boxSizing: 'border-box', width: '100%', height: HEAD_H, display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 0.5, cursor: 'pointer',
                        color: sorted || filtered ? 'primary.main' : 'inherit', '&:hover, &:focus-visible': { bgcolor: 'action.hover' } }}>
                      {colName(c)}
                      {sorted === 'asc' && <ArrowUpNarrowWide size={13} aria-label={t('升序')} />}
                      {sorted === 'desc' && <ArrowDownNarrowWide size={13} aria-label={t('降序')} />}
                      {filtered && <Filter size={12} aria-label={t('已筛选')} />}
                    </Box>
                  ) : colName(c)}
                </Box>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {fixed.map((ri, i) => row(ri, ri === view.header || i < 3 ? i : undefined))}
          {first > 0 && <tr style={{ height: first * ROW_H }} aria-hidden><td colSpan={cols + 1} /></tr>}
          {data.slice(first, last).map((ri) => row(ri))}
          {last < data.length && <tr style={{ height: (data.length - last) * ROW_H }} aria-hidden><td colSpan={cols + 1} /></tr>}
          {data.length === 0 && (
            <tr><Box component="td" colSpan={cols + 1} sx={{ p: 3, textAlign: 'center', color: 'text.secondary' }}>{t('没有符合筛选的行')}</Box></tr>
          )}
        </tbody>
      </Box>
      {menu && <ColumnMenu sheet={sheet} col={menu.col} anchor={menu.el} name={headerText(menu.col) || colName(menu.col)} view={view}
        setView={setView} onClose={() => setMenu(null)} />}
    </Box>
  );
}

/** 列菜单：排序，和按值筛选 */
function ColumnMenu({ sheet, col, anchor, name, view, setView, onClose }: {
  sheet: Sheet; col: number; anchor: HTMLElement; name: string; view: SheetViewState; setView: (v: SheetViewState) => void; onClose: () => void;
}) {
  const values = useMemo(() => columnValues(sheet.rows, view.header, col), [sheet.rows, view.header, col]);
  const [picked, setPicked] = useState<Set<string>>(() => new Set(view.filters[col] ?? values.map((v) => v.value)));
  const [find, setFind] = useState('');
  const shown = values.filter((v) => !find || v.value.toLowerCase().includes(find.toLowerCase()));
  const all = shown.every((v) => picked.has(v.value));
  const sortBy = (dir: 'asc' | 'desc' | null) => { setView({ ...view, sort: dir ? { col, dir } : null }); onClose(); };
  const apply = () => {
    const filters = { ...view.filters };
    if (picked.size === values.length) delete filters[col];
    else filters[col] = [...picked];
    setView({ ...view, filters });
    onClose();
  };
  return (
    <Popover open anchorEl={anchor} onClose={onClose} anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
      slotProps={{ paper: { sx: { width: 280, maxHeight: 520, display: 'flex', flexDirection: 'column' } } }}>
      <Typography variant="subtitle2" sx={{ px: 2, pt: 1.5, pb: 0.5 }} noWrap>{name}</Typography>
      <List dense disablePadding>
        <ListItemButton onClick={() => sortBy('asc')} selected={view.sort?.col === col && view.sort.dir === 'asc'}>
          <ArrowUpNarrowWide size={16} style={{ marginRight: 8 }} /><ListItemText primary={t('升序')} />
        </ListItemButton>
        <ListItemButton onClick={() => sortBy('desc')} selected={view.sort?.col === col && view.sort.dir === 'desc'}>
          <ArrowDownNarrowWide size={16} style={{ marginRight: 8 }} /><ListItemText primary={t('降序')} />
        </ListItemButton>
        {view.sort?.col === col && <ListItemButton onClick={() => sortBy(null)}><ListItemText primary={t('取消排序')} sx={{ pl: 3 }} /></ListItemButton>}
      </List>
      <Divider />
      <Box sx={{ px: 2, pt: 1.5 }}>
        <TextField size="small" fullWidth label={t('搜索值')} value={find} onChange={(e) => setFind(e.target.value)} />
        <FormControlLabel sx={{ mt: 0.5 }} label={t('全选')}
          control={<Checkbox size="small" checked={all} indeterminate={!all && shown.some((v) => picked.has(v.value))}
            onChange={(e) => setPicked((p) => { const n = new Set(p); for (const v of shown) { if (e.target.checked) n.add(v.value); else n.delete(v.value); } return n; })} />} />
      </Box>
      <Box sx={{ flex: 1, overflow: 'auto', px: 2, minHeight: 0 }}>
        {shown.slice(0, MAX_VALUES).map((v) => (
          <FormControlLabel key={v.value} sx={{ display: 'flex', mr: 0 }}
            label={<Box component="span" sx={{ display: 'flex', gap: 1, minWidth: 0 }}>
              <Box component="span" sx={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>{v.value.trim() ? v.value : t('（空白）')}</Box>
              <Box component="span" sx={{ color: 'text.secondary' }}>{v.count}</Box>
            </Box>}
            slotProps={{ typography: { sx: { minWidth: 0, flex: 1, fontSize: 13 } } }}
            control={<Checkbox size="small" checked={picked.has(v.value)}
              onChange={(e) => setPicked((p) => { const n = new Set(p); if (e.target.checked) n.add(v.value); else n.delete(v.value); return n; })} />} />
        ))}
        {shown.length > MAX_VALUES && <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', py: 0.5 }}>{t('还有 {n} 个值没有列出，可以搜索', { n: shown.length - MAX_VALUES })}</Typography>}
      </Box>
      <Stack direction="row" spacing={1} sx={{ p: 1.5, justifyContent: 'flex-end', borderTop: 1, borderColor: 'divider' }}>
        <Button size="small" onClick={onClose}>{t('取消')}</Button>
        <Button size="small" variant="contained" disabled={picked.size === 0} onClick={apply}>{t('确定')}</Button>
      </Stack>
    </Popover>
  );
}

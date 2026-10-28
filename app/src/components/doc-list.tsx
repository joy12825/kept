// 文档列表（全部文件、目录条目页共用）：搜索、筛选条（选项旁是数量）、条件胶囊、列表 / 网格、排序、
// 按月分组、滚动加载、多选与批量操作（含放入抽屉）。
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Link as RouterLink, useLocation, useNavigate } from 'react-router-dom';
import { Alert, Box, Button, Checkbox, Chip, IconButton, InputBase, ListItemIcon, ListItemText, ListSubheader, Menu, MenuItem, Paper, Popover, Popper, Skeleton, Stack, TextField, ToggleButton, ToggleButtonGroup, Tooltip, Typography } from '@mui/material';
import { ArrowUpDown, Check, ChevronDown, CircleHelp, Columns3, EyeOff, LayoutGrid, List as ListIcon, Lock, MessageSquare, Search, Square, SquareCheck, X } from 'lucide-react';
import { downloadUrl, fileUrl, isPdfDocument, startDownload, useBulk, useDocumentList, useFacets, useRestoreDocument, type DocumentSort, type DocumentSummary } from '../api/documents';
import { defaultDocList, describeAsn, describeDate, fieldIdOf, FILE_KINDS, PERIODS, toFilter, type ColumnKey, type ListFilters, type Cond, type DateFilter, type DocListState, type FilterGroup } from '../api/filters';
import { useRole } from '../api/workspaces';
import { useApp } from '../store/app-store';
import { EmptyState, ErrorState } from './common';
import { DrawerIcon, SW, TagDot, Thumb } from './icons';
import { correspondentOptions, drawerOptions, locationOptions, personOptions, Picker, tagOptions, typeOptions, useTaxonomyIndex, type PickerOption, type TaxonomyIndex } from './pickers';
import { SaveViewDialog } from './save-view-dialog';
import { MergeDialog } from './pdf-lazy';
import { useMe } from '../api/session';
import { EmailDialog } from './email-dialog';
import { useAgentSettings, useOrganizeDocuments } from '../api/agents';
import { ShareBundleDialog, eeBuild, CollectionDialog } from '@ee';
import { BulkFieldDialog } from './bulk-field-dialog';
import { docHref, docLinkClick } from './doc-link';
import { FieldCell, fieldColumnKey, fieldSortDir, listableField } from './field-columns';
import { FieldCondEditor, startCond } from './field-cond-editor';
import { describeFieldCond, fieldCondToCond, type FieldCond } from '../utils/field-conds';
import { TagBatchDialog } from './tag-batch-dialog';
import { useTaxonomy, type CustomField } from '../api/taxonomy';
import { useUnlocked } from '../api/sensitive';
import { VerifyDialog } from './verify-dialog';
import { t, tc, currentLang } from '../i18n';

const LABEL: Record<FilterGroup, string> = { drawer: t('抽屉'), type: t('类型'), correspondent: t('往来方'), person: t('家人'), location: t('存放位置'), tag: t('标签'), year: t('日期') };
// 家人、存放位置（v1.6.3）在空间里有了才显示
const GROUPS: FilterGroup[] = ['drawer', 'type', 'correspondent', 'person', 'tag', 'location', 'year'];
const NONE_LABEL: Partial<Record<FilterGroup, string>> = { drawer: t('未入抽屉'), type: t('没有类型'), correspondent: t('没有往来方'), person: t('没有家人'), location: t('没有记录位置') };
const SORTS: [DocumentSort, 'asc' | 'desc', string][] = [
  ['created', 'desc', t('日期（新到旧）')], ['created', 'asc', t('日期（旧到新）')], ['added', 'desc', t('添加时间（新到旧）')],
  ['title', 'asc', t('标题')], ['correspondent', 'asc', t('往来方')],
  // v1.4.3：没有这一项的排在最后
  ['due', 'asc', t('到期日（近的在前）')], ['amount', 'desc', t('金额（大到小）')], ['asn', 'asc', t('原件编号')], ['modified', 'desc', t('修改时间（新到旧）')],
];

export interface DocListProps {
  /** 列表状态在 store 里的键 */
  listKey: string;
  /** 页面固定的条件（不显示为胶囊、不能删除），例如条目页的“往来方是 X” */
  base?: Cond[];
  /** 不显示的筛选按钮（条目页隐藏自己那一组） */
  hideGroups?: FilterGroup[];
  placeholder: string;
  /** 返回按钮上显示的来源名称 */
  backLabel: string;
  showSave?: boolean;
  /** 批量操作栏里额外的按钮（例如待整理的“完成整理”） */
  batchExtra?: (sel: { ids: number[] } | { filter: string }, clear: () => void) => React.ReactNode;
  emptyTitle?: string;
  emptyText?: string;
}

/** 文档日期所在的月份：“2024-03” → “2024 年 3 月”。 */
// 英文用月份名称（October 2026）；中文照旧“2026 年 10 月”
const monthLabel = (m: string) => currentLang() === 'en'
  ? new Intl.DateTimeFormat('en', { year: 'numeric', month: 'long' }).format(new Date(Number(m.slice(0, 4)), Number(m.slice(5, 7)) - 1, 1))
  : t('{y} 年 {m} 月', { y: m.slice(0, 4), m: Number(m.slice(5, 7)) });

export default function DocList({ listKey, base = [], hideGroups = [], placeholder, backLabel, showSave, batchExtra, emptyTitle, emptyText }: DocListProps) {
  const app = useApp();
  const nav = useNavigate();
  const loc = useLocation();
  const role = useRole();
  const ix = useTaxonomyIndex();
  const L: DocListState = app.s.docLists[listKey] ?? defaultDocList();
  const set = (p: Partial<DocListState> | ((l: DocListState) => Partial<DocListState>)) => app.setDocList(listKey, p);

  // 搜索框输入防抖 250ms 再查询
  const [qInput, setQInput] = useState(L.q);
  useEffect(() => { setQInput(L.q); }, [L.q]);
  useEffect(() => {
    if (qInput === L.q) return;
    const t = window.setTimeout(() => set((l) => searchPatch(l, qInput)), 250);
    return () => window.clearTimeout(t);
  }, [qInput]); // eslint-disable-line react-hooks/exhaustive-deps

  const filter = useMemo(() => toFilter(L, base), [L.q, L.f, L.extra, JSON.stringify(base)]); // eslint-disable-line react-hooks/exhaustive-deps
  // 自定义字段的列与排序（v1.5.6）：字段删掉后它的列不再显示，按它排序改回按日期
  const fieldList = useTaxonomy('custom-fields').data;
  const fieldDefs = useMemo(() => new Map((fieldList ?? []).map((f) => [f.id, f])), [fieldList]);
  const colOf = (k: ColumnKey): Column | undefined => {
    const id = fieldIdOf(k);
    if (id === null) return COLUMNS[k as BuiltInColumn];
    const f = fieldDefs.get(id);
    return f && listableField(f) ? fieldColumn(f) : undefined;
  };
  const sortFieldId = fieldIdOf(L.sort);
  const sortGone = sortFieldId !== null && !!fieldList && !fieldDefs.has(sortFieldId);
  const sort: DocumentSort = sortGone ? 'created' : L.sort;
  const showDrawer = !hideGroups.includes('drawer');
  const defaultCols: ColumnKey[] = showDrawer ? ['correspondent', 'type', 'drawer', 'date', 'tags'] : ['correspondent', 'type', 'date', 'tags'];
  const cols = (L.columns ?? defaultCols).filter((k) => fieldIdOf(k) === null || (fieldList ? !!colOf(k) : true));
  const fields = cols.map(fieldIdOf).filter((x) => x !== null).join(',');
  const query = { filter, sort, dir: sortGone ? 'desc' as const : L.dir, fields: fields || undefined };
  const list = useDocumentList(query);
  const grouped = sort === 'created' && query.dir === 'desc' && !L.q.trim();
  const shownGroups = GROUPS.filter((g) => !hideGroups.includes(g)
    && (g !== 'person' || ix.personList.length > 0 || !!L.f.person?.length) && (g !== 'location' || ix.locationList.length > 0 || !!L.f.location?.length));
  const facets = useFacets(filter, grouped ? [...shownGroups, 'month'] : shownGroups);
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const truncated = !!list.data?.pages[0]?.truncated;
  // 索引正在准备：过几秒自动重试
  useEffect(() => {
    if (list.error?.code !== 'search_unavailable') return;
    const t = window.setTimeout(() => void list.refetch(), 5000);
    return () => window.clearTimeout(t);
  }, [list.error, list.errorUpdatedAt]); // eslint-disable-line react-hooks/exhaustive-deps
  const total = list.data?.pages[0]?.total ?? 0;

  // 选择：默认是已加载的若干份；全选后可以扩大到“全部符合条件的 N 份”（按筛选条件批量操作）
  const [selAll, setSelAll] = useState(false);
  useEffect(() => setSelAll(false), [filter]);
  const sel = L.sel;
  const selCount = selAll ? total : sel.length;
  const clearSel = () => { set({ sel: [] }); setSelAll(false); };
  const selection = selAll ? { filter } : { ids: sel };
  const allLoadedSelected = items.length > 0 && items.every((d) => sel.includes(d.id));
  // Shift 点选：选中上次点的那份到这一份之间的所有文档（v1.5.6）
  const anchor = useRef<number | null>(null);
  const toggle = (id: number, range = false) => {
    setSelAll(false);
    const shown = groups.flatMap((g) => g.docs); // 按月分组时显示的顺序
    const from = anchor.current === null ? -1 : shown.findIndex((d) => d.id === anchor.current);
    const to = shown.findIndex((d) => d.id === id);
    anchor.current = id;
    if (range && from >= 0 && to >= 0) {
      const span = shown.slice(Math.min(from, to), Math.max(from, to) + 1).map((d) => d.id);
      set((l) => ({ sel: [...l.sel, ...span.filter((x) => !l.sel.includes(x))] }));
      return;
    }
    set((l) => ({ sel: l.sel.includes(id) ? l.sel.filter((x) => x !== id) : [...l.sel, id] }));
  };

  // 滚到底自动加载下一页
  const sentinel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = sentinel.current;
    if (!el || typeof IntersectionObserver === 'undefined') return;
    const io = new IntersectionObserver((es) => {
      if (es.some((e) => e.isIntersecting) && list.hasNextPage && !list.isFetchingNextPage) void list.fetchNextPage();
    }, { rootMargin: '400px' });
    io.observe(el);
    return () => io.disconnect();
  }, [list.hasNextPage, list.isFetchingNextPage, list.fetchNextPage, items.length]); // eslint-disable-line react-hooks/exhaustive-deps

  // 从详情页返回时恢复滚动位置
  const restored = useRef(false);
  useEffect(() => {
    if (restored.current || !items.length) return;
    restored.current = true;
    const y = app.s.scroll[loc.pathname + loc.search];
    if (y) requestAnimationFrame(() => window.scrollTo(0, y));
  }, [items.length]); // eslint-disable-line react-hooks/exhaustive-deps

  // 点列表里的标签、往来方、类型：加进筛选条件（已经在里面时不变）
  // 条目页隐藏了自己那一组：那一组本来就只有它，点了不变
  const addFilter: Filterer = (group, id) => {
    if (hideGroups.includes(group)) return;
    set((l) => (((l.f[group] ?? []) as number[]).includes(id) ? {} : toggleIn(l, group, id)));
  };

  const open = (id: number) => {
    const from = loc.pathname + loc.search;
    app.set((p) => ({ docNav: { ids: items.map((d) => d.id), from, label: backLabel, query }, scroll: { ...p.scroll, [from]: window.scrollY } }));
    nav(`/docs/${id}`);
  };

  // 键盘：/ 聚焦搜索，J / K 移动，X 选中，Enter 打开，Esc 清除选择
  const searchRef = useRef<HTMLInputElement>(null);
  const [cur, setCur] = useState(-1);
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName) || t.isContentEditable || e.metaKey || e.ctrlKey || e.altKey) return;
      if (app.s.cmdOpen || app.s.uploadOpen || app.s.confirm) return;
      if (e.key === '/') { e.preventDefault(); searchRef.current?.focus(); return; }
      if (e.key === 'Escape' && (sel.length || selAll)) { clearSel(); return; }
      if (!items.length) return;
      const k = e.key.toLowerCase();
      if (k === 'j' || k === 'k') {
        const next = Math.min(Math.max(0, cur + (k === 'j' ? 1 : -1)), items.length - 1);
        setCur(next);
        document.getElementById(`doc-row-${items[next].id}`)?.scrollIntoView({ block: 'nearest' });
      } else if (k === 'x' && items[cur]) toggle(items[cur].id);
      else if (e.key === 'Enter' && items[cur]) open(items[cur].id);
    };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  });

  const months = useMemo(() => new Map((facets.data?.month ?? []).map((b) => [b.key, b.count])), [facets.data]);
  const groups = useMemo(() => {
    if (!grouped) return [{ key: 'flat', label: '', docs: items }];
    const m = new Map<string, DocumentSummary[]>();
    for (const d of items) {
      const k = d.createdDate.slice(0, 7);
      m.set(k, [...(m.get(k) ?? []), d]);
    }
    return [...m].map(([k, docs]) => ({ key: k, label: t('{month} · {n} 份', { month: monthLabel(k), n: months.get(k) ?? docs.length }), docs }));
  }, [items, grouped, months]);

  const setSort = (sort: DocumentSort, dir?: 'asc' | 'desc') => set((l) => {
    const fid = fieldIdOf(sort);
    const first = fid !== null ? fieldSortDir(fieldDefs.get(fid)) : sort === 'created' || sort === 'added' || sort === 'modified' || sort === 'amount' ? 'desc' : 'asc';
    return { sort, dir: dir ?? (l.sort === sort ? (l.dir === 'asc' ? 'desc' : 'asc') : first), sel: [] };
  });
  const [sortEl, setSortEl] = useState<HTMLElement | null>(null);
  const [saveOpen, setSaveOpen] = useState(false);

  const chips = filterChips(L, ix, fieldDefs);
  const compact = app.s.density === 'compact';
  const [colsEl, setColsEl] = useState<HTMLElement | null>(null);
  // 可以作为列的自定义字段，按字段的顺序
  const fieldCols = (fieldList ?? []).filter(listableField);
  // 勾选的列按固定的顺序排列：内置的列在前，自定义字段在后
  const toggleColumn = (k: ColumnKey) => {
    const next = cols.includes(k) ? cols.filter((c) => c !== k) : [...cols, k];
    set({ columns: [...COLUMN_ORDER, ...fieldCols.map(fieldColumnKey)].filter((c) => next.includes(c)) });
  };
  const resolved = cols.map((k) => [k, colOf(k)] as const).filter((x): x is readonly [ColumnKey, Column] => !!x[1]);
  // 排序菜单里加上显示着的自定义字段列
  const sorts: [DocumentSort, 'asc' | 'desc', string][] = [
    ...(L.q.trim() ? [['relevance', 'desc', t('相关度')] as [DocumentSort, 'asc' | 'desc', string]] : []), ...SORTS,
    ...resolved.filter(([k]) => fieldIdOf(k) !== null).map(([k, c]) => [k as DocumentSort, fieldSortDir(fieldDefs.get(fieldIdOf(k)!)), c.label] as [DocumentSort, 'asc' | 'desc', string]),
  ];

  return (
    <Box sx={{ pb: 15 }}>
      <Paper elevation={0} sx={{ display: 'flex', alignItems: 'center', gap: 1.5, height: 48, px: 2, borderRadius: 7, bgcolor: 'surface.containerHigh', color: 'text.secondary' }}>
        <Search size={18} strokeWidth={SW} />
        <InputBase inputRef={searchRef} value={qInput} onChange={(e) => setQInput(e.target.value)} placeholder={placeholder}
          inputProps={{ 'aria-label': t('搜索文档') }} sx={{ flex: 1, fontSize: 15 }} />
        {qInput ? <IconButton size="small" aria-label={t('清除搜索')} onClick={() => { setQInput(''); set((l) => searchPatch(l, '')); }}><X size={16} strokeWidth={SW} /></IconButton>
          : <Box sx={{ fontSize: 11, px: 0.75, border: 1, borderColor: 'divider', borderRadius: 1 }}>/</Box>}
        <SearchHelp />
      </Paper>

      <Stack direction="row" spacing={1} useFlexGap sx={{ alignItems: 'center', flexWrap: 'wrap', mt: 1.5 }}>
        {shownGroups.map((g) => (g === 'year'
          ? <DateFilterButton key={g} state={L} counts={facets.data?.year} onYear={(y) => set((l) => ({ ...toggleIn(l, 'year', y), f: { ...(toggleIn(l, 'year', y).f as DocListState['f']), date: undefined } }))}
            onDate={(date) => set((l) => ({ f: { ...l.f, year: [], date }, sel: [] }))} />
          : <FilterButton key={g} group={g} state={L} ix={ix} counts={facets.data?.[g]} onToggle={(v) => set((l) => toggleIn(l, g, v))}
            onFilters={(p) => set((l) => ({ f: { ...l.f, ...p }, sel: [] }))} />))}
        {fieldList && fieldList.length > 0 && <FieldFilterButton fields={fieldList} onAdd={(x) => set((l) => ({ f: { ...l.f, fields: [...(l.f.fields ?? []), x] }, sel: [] }))} />}
        <MoreFilterButton state={L} onFilters={(p) => set((l) => ({ f: { ...l.f, ...p }, sel: [] }))} />
        {showSave && role.canWrite && (L.q || chips.length > 0) && <Button size="small" onClick={() => setSaveOpen(true)}>{t('保存筛选')}</Button>}
        <Box sx={{ flex: 1 }} />
        <ToggleButtonGroup size="small" exclusive value={L.view} onChange={(_, v) => v && set({ view: v })}>
          <ToggleButton value="list"><ListIcon size={16} strokeWidth={SW} />{t('列表')}</ToggleButton>
          <ToggleButton value="grid"><LayoutGrid size={16} strokeWidth={SW} />{t('网格')}</ToggleButton>
        </ToggleButtonGroup>
        <Button variant="outlined" size="small" startIcon={<ArrowUpDown size={16} strokeWidth={SW} />} onClick={(e) => setSortEl(e.currentTarget)}>{t('排序')}</Button>
        <Menu anchorEl={sortEl} open={!!sortEl} onClose={() => setSortEl(null)} anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }} transformOrigin={{ vertical: 'top', horizontal: 'right' }}>
          {sorts.map(([k, d, l]) => (
            <MenuItem key={l} onClick={() => { setSort(k, d); setSortEl(null); }} sx={{ minWidth: 180 }}>
              <Typography sx={{ flex: 1 }}>{l}</Typography>
              {L.sort === k && (k === 'relevance' || L.dir === d) && <ListItemIcon sx={{ color: 'primary.main', justifyContent: 'flex-end' }}><Check size={16} strokeWidth={SW} /></ListItemIcon>}
            </MenuItem>
          ))}
        </Menu>
      </Stack>

      {chips.length > 0 && (
        <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', alignItems: 'center', mt: 1.5 }}>
          {chips.map((c) => <Chip key={c.key} label={c.text} onDelete={() => set((l) => c.remove(l))} sx={{ bgcolor: 'surface.containerHigh', color: 'text.primary' }} />)}
          <Button size="small" onClick={() => { setQInput(''); set((l) => ({ ...searchPatch(l, ''), f: defaultDocList().f, extra: undefined, extraLabel: undefined })); }}>{t('清除全部')}</Button>
        </Stack>
      )}

      {list.isPending ? <ListSkeleton compact={compact} />
        : list.isError ? (list.error.code === 'search_unavailable'
          ? <Alert severity="info" sx={{ mt: 2 }}>{t('{message}。几秒后会自动重试。', { message: list.error.message })}</Alert>
          : <ErrorState text={list.error.status ? list.error.message : undefined} onRetry={() => void list.refetch()} />)
          : items.length === 0 ? (
            L.q || chips.length ? <EmptyState icon={<Search size={40} strokeWidth={SW} />} title={t('没有符合条件的文档')} text={t('换个关键词，或减少筛选条件。')} />
              : <EmptyState title={emptyTitle ?? t('还没有文档')} text={emptyText ?? t('拖入文件开始整理，支持 PDF、图片和文本文件。')} action={role.canWrite ? t('上传文档') : undefined} onAction={() => app.set({ uploadOpen: true })} />
          ) : (
            <>
              {!grouped && (
                <Typography variant="body2" sx={{ color: 'text.secondary', mt: 1.5 }}>{L.q.trim() ? t('搜索结果 · {n} 份', { n: total }) : t('共 {n} 份', { n: total })}</Typography>
              )}
              {truncated && <Alert severity="info" sx={{ mt: 1 }}>{t('命中的文档较多，只显示相关度最高的一部分。加上更多关键词或筛选条件可以缩小范围。')}</Alert>}
              {L.view === 'list' && (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, height: 40, mt: 1, px: 1, borderBottom: 1, borderColor: 'divider', color: 'text.secondary', fontSize: 12 }}>
                  <Checkbox size="small" checked={allLoadedSelected || selAll} indeterminate={!allLoadedSelected && !selAll && sel.length > 0}
                    slotProps={{ input: { 'aria-label': t('全选') } }} onChange={() => { setSelAll(false); set({ sel: allLoadedSelected ? [] : items.map((d) => d.id) }); }} sx={{ p: 0.25 }} />
                  <Box sx={{ width: 32, display: compact ? 'none' : 'block' }} />
                  <HeaderCell label={t('标题')} active={L.sort === 'title'} dir={L.dir} onClick={() => setSort('title')} flex="1" minWidth={TITLE_MIN} />
                  {resolved.map(([k, c]) => {
                    return c.sort
                      ? <HeaderCell key={k} label={c.label} active={L.sort === c.sort && (c.sort !== 'created' || L.dir === 'asc')} dir={L.dir} onClick={() => setSort(c.sort!)} flex={c.flex} wide={c.wide} />
                      : <Box key={k} sx={{ flex: c.flex, minWidth: 0, fontWeight: 500, display: c.wide ? WIDE : 'block', textAlign: c.align, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{c.label}</Box>;
                  })}
                  <Tooltip title={t('显示的列')}><IconButton size="small" aria-label={t('显示的列')} onClick={(e) => setColsEl(e.currentTarget)} sx={{ display: WIDE }}><Columns3 size={16} strokeWidth={SW} /></IconButton></Tooltip>
                  <Menu anchorEl={colsEl} open={!!colsEl} onClose={() => setColsEl(null)} anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }} transformOrigin={{ vertical: 'top', horizontal: 'right' }}>
                    {COLUMN_ORDER.map((k) => (
                      <MenuItem key={k} dense onClick={() => toggleColumn(k)} sx={{ minWidth: 160 }}>
                        <Checkbox size="small" checked={cols.includes(k)} sx={{ p: 0, mr: 1 }} />{COLUMNS[k].label}
                      </MenuItem>
                    ))}
                    {fieldCols.length > 0 && <ListSubheader sx={{ lineHeight: '32px', fontSize: 12 }}>{tc('复数', '自定义字段')}</ListSubheader>}
                    {fieldCols.map((f) => (
                      <MenuItem key={f.id} dense onClick={() => toggleColumn(fieldColumnKey(f))} sx={{ minWidth: 160 }}>
                        <Checkbox size="small" checked={cols.includes(fieldColumnKey(f))} sx={{ p: 0, mr: 1 }} />{f.name}
                      </MenuItem>
                    ))}
                    <MenuItem dense disabled={!L.columns} onClick={() => { set({ columns: undefined }); setColsEl(null); }}>{t('恢复默认')}</MenuItem>
                  </Menu>
                </Box>
              )}
              {allLoadedSelected && !selAll && total > items.length && (
                <Box sx={{ py: 1, textAlign: 'center', fontSize: 13, color: 'text.secondary' }}>
                  {t('已选中本页加载的 {n} 份。', { n: items.length })}<Button size="small" onClick={() => setSelAll(true)}>{t('选择全部 {n} 份', { n: total })}</Button>
                </Box>
              )}
              {groups.map((gr) => (
                <Box key={gr.key} component="section" sx={{ mb: 1 }}>
                  {gr.label && <Typography variant="h6" sx={{ position: 'sticky', top: 64, zIndex: 3, py: 1.5, px: 1, bgcolor: 'background.default' }}>{gr.label}</Typography>}
                  {L.view === 'list'
                    ? gr.docs.map((d) => <Row key={d.id} d={d} ix={ix} compact={compact} cols={resolved} checked={selAll || sel.includes(d.id)} current={items[cur]?.id === d.id} onToggle={(range) => toggle(d.id, range)} onOpen={() => open(d.id)} onFilter={addFilter} />)
                    : (
                      <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(180px,1fr))', gap: '20px 16px', py: 1 }}>
                        {gr.docs.map((d) => <Card key={d.id} d={d} ix={ix} checked={selAll || sel.includes(d.id)} onToggle={(range) => toggle(d.id, range)} onOpen={() => open(d.id)} />)}
                      </Box>
                    )}
                </Box>
              ))}
              <Box ref={sentinel} sx={{ height: 1 }} />
              {list.isFetchingNextPage && <ListSkeleton compact={compact} rows={3} />}
            </>
          )}

      {selCount > 0 && <BatchBar count={selCount} selection={selection} items={items} ix={ix} canWrite={role.canWrite} onDone={clearSel} extra={batchExtra?.(selection, clearSel)} />}
      {saveOpen && <SaveViewDialog state={L} onClose={() => setSaveOpen(false)} />}
    </Box>
  );
}

/** 输入关键词时改为按相关度排序（记下原来的），清空后恢复。 */
export function searchPatch(l: DocListState, q: string): Partial<DocListState> {
  const had = !!l.q.trim();
  const has = !!q.trim();
  if (has && !had && l.sort !== 'relevance') return { q, sel: [], sort: 'relevance', dir: 'desc', sortBeforeSearch: { sort: l.sort, dir: l.dir } };
  if (!has && had) {
    const back = l.sortBeforeSearch ?? (l.sort === 'relevance' ? { sort: 'created' as const, dir: 'desc' as const } : { sort: l.sort, dir: l.dir });
    return { q, sel: [], ...back, sortBeforeSearch: undefined };
  }
  return { q, sel: [] };
}

/** 搜索框旁的语法说明（设计文档 5.4）。 */
function SearchHelp() {
  const [el, setEl] = useState<HTMLElement | null>(null);
  const rows: [string, string][] = [
    [t('电费 发票'), t('每个词都要出现')], [t('"专用发票"'), t('整个短语')], [t('合同 -草稿'), t('排除含“草稿”的')],
    [t('tag:税务  type:合同  from:国家电网'), t('只在标签、类型、往来方中找')], [t('person:乐乐'), t('只在家人中找')], ['created:2024  created:2024-01..2024-06', t('按文档日期')], ['asn:123', t('按原件编号')],
  ];
  return (
    <>
      <Tooltip title={t('搜索语法')}><IconButton size="small" aria-label={t('搜索语法')} onClick={(e) => setEl(e.currentTarget)}><CircleHelp size={16} strokeWidth={SW} /></IconButton></Tooltip>
      <Popover open={!!el} anchorEl={el} onClose={() => setEl(null)} anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }} transformOrigin={{ vertical: 'top', horizontal: 'right' }}
        slotProps={{ paper: { sx: { p: 2, maxWidth: 420 } } }}>
        <Typography variant="h6" sx={{ mb: 1 }}>{t('搜索语法')}</Typography>
        <Box component="dl" sx={{ m: 0, display: 'grid', gridTemplateColumns: 'auto 1fr', columnGap: 2, rowGap: 0.75 }}>
          {rows.map(([k, v]) => (
            <Box key={k} sx={{ display: 'contents' }}>
              <Box component="dt" sx={{ fontFamily: 'monospace', fontSize: 13 }}>{k}</Box>
              <Box component="dd" sx={{ m: 0, fontSize: 13, color: 'text.secondary' }}>{v}</Box>
            </Box>
          ))}
        </Box>
        <Typography variant="caption" sx={{ display: 'block', mt: 1.5, color: 'text.secondary' }}>{t('标题、正文、备注、标签、往来方、类型和文字类自定义字段都会被搜索；有关键词时默认按相关度排序。')}</Typography>
      </Popover>
    </>
  );
}

/** 日期筛选（v1.5.2）：时间段、自定义起止，或按年份（带份数）；按文档日期或添加时间。 */
function DateFilterButton({ state, counts, onYear, onDate }: { state: DocListState; counts?: { key: string; count: number }[]; onYear: (y: string) => void; onDate: (d: DateFilter | undefined) => void }) {
  const [el, setEl] = useState<HTMLElement | null>(null);
  const d = state.f.date;
  const [added, setAdded] = useState(!!d?.added);
  const [from, setFrom] = useState(d?.from ?? '');
  const [to, setTo] = useState(d?.to ?? '');
  useEffect(() => { if (el) { setAdded(!!state.f.date?.added); setFrom(state.f.date?.from ?? ''); setTo(state.f.date?.to ?? ''); } }, [el]); // eslint-disable-line react-hooks/exhaustive-deps
  const years = [...new Set([...(counts ?? []).map((b) => b.key), ...state.f.year])].sort().reverse();
  const countOf = new Map((counts ?? []).map((b) => [b.key, b.count]));
  const label = d ? describeDate(d).replace(new RegExp(`^(${t('日期')}|${t('添加时间')})[：:]\\s*`), '') : state.f.year.length ? `${state.f.year.length}` : '';
  const pick = (p: DateFilter['period']) => { onDate({ period: p, added: added || undefined }); setEl(null); };
  const custom = from || to ? { from: from || undefined, to: to || undefined, added: added || undefined } : null;
  const customOk = !!custom && (!from || !to || from <= to);
  return (
    <>
      <Chip variant={d || state.f.year.length ? 'filled' : 'outlined'} aria-haspopup="dialog"
        label={<Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>{label ? t('日期 · {label}', { label }) : t('日期')}<ChevronDown size={16} strokeWidth={SW} /></Box>}
        onClick={(e) => setEl(e.currentTarget)} />
      <Popover open={!!el} anchorEl={el} onClose={() => setEl(null)} anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
        slotProps={{ paper: { 'aria-label': t('日期筛选'), sx: { p: 2, width: 320 } } as object }}>
        <ToggleButtonGroup size="small" exclusive fullWidth value={added ? 'added' : 'created'} onChange={(_, v) => v && setAdded(v === 'added')} sx={{ mb: 1.5 }}>
          <ToggleButton value="created">{t('按文档日期')}</ToggleButton>
          <ToggleButton value="added">{t('按添加时间')}</ToggleButton>
        </ToggleButtonGroup>
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75 }}>
          {PERIODS.map(([k, l]) => (
            <Chip key={k} label={l} size="small" variant={d?.period === k && !!d.added === added ? 'filled' : 'outlined'} color={d?.period === k && !!d.added === added ? 'primary' : 'default'} onClick={() => pick(k)} />
          ))}
        </Box>
        <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary', mt: 2, mb: 1 }}>{t('自定义起止')}</Typography>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          <TextField type="date" size="small" label={t('从')} value={from} onChange={(e) => setFrom(e.target.value)} slotProps={{ inputLabel: { shrink: true } }} sx={{ flex: 1 }} />
          <TextField type="date" size="small" label={t('到')} value={to} onChange={(e) => setTo(e.target.value)} slotProps={{ inputLabel: { shrink: true } }} sx={{ flex: 1 }} />
        </Stack>
        <Stack direction="row" sx={{ justifyContent: 'flex-end', mt: 1 }}>
          {d && <Button size="small" sx={{ mr: 'auto' }} onClick={() => { onDate(undefined); setEl(null); }}>{t('清除')}</Button>}
          <Button size="small" variant="contained" disabled={!customOk} onClick={() => { onDate(custom!); setEl(null); }}>{t('应用')}</Button>
        </Stack>
        {years.length > 0 && !added && (
          <>
            <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary', mt: 2, mb: 0.5 }}>{t('按年份（可多选）')}</Typography>
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75 }}>
              {years.map((y) => {
                const on = state.f.year.includes(y);
                return <Chip key={y} size="small" label={t('{y} 年 · {count}', { y, count: countOf.get(y) ?? 0 })} variant={on ? 'filled' : 'outlined'} color={on ? 'primary' : 'default'} onClick={() => onYear(y)} />;
              })}
            </Box>
          </>
        )}
      </Popover>
    </>
  );
}

/** 筛选面板上方的方式（v1.6.1）：标签“含任一个 / 全部含有 / 不含”和“没有标签”；类型、往来方“是 / 不是”。 */
function modeHeader(group: FilterGroup, state: DocListState, onFilters: (p: Partial<ListFilters>) => void): React.ReactNode {
  const tg = (value: string, items: [string, string][], onChange: (v: string) => void, label: string) => (
    <ToggleButtonGroup size="small" exclusive fullWidth value={value} onChange={(_, v) => v && onChange(v)} aria-label={label} sx={{ mb: 0.75, '& .MuiToggleButton-root': { py: 0.25, fontSize: 12 } }}>
      {items.map(([k, l]) => <ToggleButton key={k} value={k}>{l}</ToggleButton>)}
    </ToggleButtonGroup>
  );
  if (group === 'tag') {
    return (
      <>
        {tg(state.f.tagMode ?? 'any', [['any', t('含任一个')], ['all', t('全部含有')], ['none', t('不含')]], (v) => onFilters({ tagMode: v === 'any' ? undefined : v as 'all' | 'none', tagNone: undefined }), t('标签的筛选方式'))}
        <Box component="button" type="button" role="checkbox" aria-checked={!!state.f.tagNone}
          onClick={() => onFilters({ tagNone: state.f.tagNone ? undefined : true, tag: [], tagMode: undefined })}
          sx={{ display: 'flex', alignItems: 'center', gap: 1, width: '100%', px: 1, py: 0.5, mb: 0.5, border: 0, borderRadius: 1, font: 'inherit', fontSize: 13, color: 'text.primary', textAlign: 'left', cursor: 'pointer',
            bgcolor: state.f.tagNone ? 'action.selected' : 'transparent', '&:hover': { bgcolor: 'action.hover' } }}>
          <Box aria-hidden sx={{ display: 'flex', color: state.f.tagNone ? 'primary.main' : 'text.secondary' }}>{state.f.tagNone ? <SquareCheck size={18} strokeWidth={SW} /> : <Square size={18} strokeWidth={SW} />}</Box>{t('没有标签')}
        </Box>
      </>
    );
  }
  if (group === 'type' || group === 'correspondent' || group === 'person') {
    const key = group === 'type' ? 'typeNot' : group === 'person' ? 'personNot' : 'correspondentNot';
    return tg(state.f[key] ? 'not' : 'is', [['is', tc('条件', '是')], ['not', tc('条件', '不是')]], (v) => onFilters({ [key]: v === 'not' ? true : undefined }), t('{group}的筛选方式', { group: LABEL[group] }));
  }
  return null;
}

/** 按自定义字段筛选（v1.6.1）：选字段、条件、值，添加后显示为胶囊，可以加多个（彼此为“且”）。 */
function FieldFilterButton({ fields, onAdd }: { fields: CustomField[]; onAdd: (x: { fc: FieldCond; cond: Cond }) => void }) {
  const [el, setEl] = useState<HTMLElement | null>(null);
  const [fc, setFc] = useState<FieldCond | null>(null);
  const f = fc ? fields.find((x) => x.id === fc.fieldId) : undefined;
  const cond = fc ? fieldCondToCond(fc, f) : null;
  return (
    <>
      <Chip variant="outlined" aria-haspopup="dialog" label={<Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>{t('字段')}<ChevronDown size={16} strokeWidth={SW} /></Box>}
        onClick={(e) => { setFc(fields[0] ? startCond(fields[0]) : null); setEl(e.currentTarget); }} />
      <Popover open={!!el} anchorEl={el} onClose={() => setEl(null)} anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
        slotProps={{ paper: { role: 'dialog', 'aria-label': t('按字段筛选'), sx: { width: 320, p: 2, mt: 0.5 } } as object }}>
        <FieldCondEditor fields={fields} value={fc} onChange={setFc} />
        <Stack direction="row" sx={{ justifyContent: 'flex-end', mt: 1.5 }}>
          <Button size="small" variant="contained" disabled={!cond} onClick={() => { if (fc && cond) onAdd({ fc, cond }); setEl(null); }}>{t('添加')}</Button>
        </Stack>
      </Popover>
    </>
  );
}

/** 更多筛选（v1.6.1）：文件格式、原件编号。 */
function MoreFilterButton({ state, onFilters }: { state: DocListState; onFilters: (p: Partial<ListFilters>) => void }) {
  const [el, setEl] = useState<HTMLElement | null>(null);
  const kinds = state.f.kinds ?? [];
  const a = state.f.asn;
  const [ranging, setRanging] = useState(false); // 选了“范围”，还没应用
  const asnMode = a?.has ? 'has' : a?.none ? 'none' : a?.from || a?.to || ranging ? 'range' : 'any';
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const n = (kinds.length ? 1 : 0) + (a ? 1 : 0);
  const num = (s: string) => (/^\d{1,6}$/.test(s) ? Number(s) : undefined);
  return (
    <>
      <Chip variant={n ? 'filled' : 'outlined'} aria-haspopup="dialog" label={<Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>{n ? t('更多 · {n}', { n }) : t('更多')}<ChevronDown size={16} strokeWidth={SW} /></Box>}
        onClick={(e) => { setFrom(a?.from ? String(a.from) : ''); setTo(a?.to ? String(a.to) : ''); setRanging(false); setEl(e.currentTarget); }} />
      <Popover open={!!el} anchorEl={el} onClose={() => setEl(null)} anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
        slotProps={{ paper: { role: 'dialog', 'aria-label': t('更多筛选'), sx: { width: 320, p: 2, mt: 0.5 } } as object }}>
        <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary', mb: 0.75 }}>{t('文件格式（可多选）')}</Typography>
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75 }}>
          {FILE_KINDS.map(([k, l]) => {
            const on = kinds.includes(k);
            return <Chip key={k} size="small" label={l} variant={on ? 'filled' : 'outlined'} color={on ? 'primary' : 'default'}
              onClick={() => { const next = on ? kinds.filter((x) => x !== k) : [...kinds, k]; onFilters({ kinds: next.length ? next : undefined }); }} />;
          })}
        </Box>
        <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary', mt: 2, mb: 0.75 }}>{t('原件编号')}</Typography>
        <ToggleButtonGroup size="small" exclusive fullWidth value={asnMode} aria-label={t('原件编号')} sx={{ '& .MuiToggleButton-root': { py: 0.25, fontSize: 12 } }}
          onChange={(_, v) => {
            if (!v) return;
            setRanging(v === 'range');
            if (v !== 'range') onFilters({ asn: v === 'any' ? undefined : v === 'has' ? { has: true } : { none: true } });
          }}>
          <ToggleButton value="any">{t('不限')}</ToggleButton><ToggleButton value="has">{t('有')}</ToggleButton><ToggleButton value="none">{t('没有')}</ToggleButton><ToggleButton value="range">{t('范围')}</ToggleButton>
        </ToggleButtonGroup>
        {asnMode === 'range' && (
          <Stack direction="row" spacing={1} sx={{ mt: 1.5, alignItems: 'center' }}>
            <TextField size="small" label={t('从')} value={from} onChange={(e) => setFrom(e.target.value.replace(/\D/g, '').slice(0, 6))} slotProps={{ htmlInput: { inputMode: 'numeric' } }} sx={{ flex: 1 }} />
            <TextField size="small" label={t('到')} value={to} onChange={(e) => setTo(e.target.value.replace(/\D/g, '').slice(0, 6))} slotProps={{ htmlInput: { inputMode: 'numeric' } }} sx={{ flex: 1 }} />
            <Button size="small" variant="contained" disabled={!num(from) && !num(to) || (!!num(from) && !!num(to) && num(from)! > num(to)!)}
              onClick={() => onFilters({ asn: { from: num(from), to: num(to) } })}>{t('应用')}</Button>
          </Stack>
        )}
      </Popover>
    </>
  );
}

function toggleIn(l: DocListState, g: FilterGroup, v: number | string): Partial<DocListState> {
  const cur = (l.f[g] ?? []) as (number | string)[];
  const next = cur.includes(v) ? cur.filter((x) => x !== v) : [...cur, v];
  return { f: { ...l.f, [g]: next, ...(g === 'tag' ? { tagNone: undefined } : {}) }, sel: [] };
}

interface ChipInfo { key: string; text: string; remove: (l: DocListState) => Partial<DocListState> }

/** 已选条件的胶囊。 */
function filterChips(L: DocListState, ix: TaxonomyIndex, fieldDefs?: Map<number, CustomField>): ChipInfo[] {
  const out: ChipInfo[] = [];
  const name = (g: FilterGroup, v: number | string) => {
    if (g === 'year') return t('{y} 年', { y: v });
    if (v === 0) return NONE_LABEL[g] ?? t('没有');
    if (g === 'drawer') return ix.drawers.get(v as number)?.name ?? t('（已删除的抽屉）');
    if (g === 'type') return ix.types.get(v as number)?.name ?? t('（已删除的类型）');
    if (g === 'correspondent') return ix.correspondents.get(v as number)?.name ?? t('（已删除的往来方）');
    if (g === 'person') return ix.persons.get(v as number)?.name ?? t('（已删除的家人）');
    if (g === 'location') return ix.locations.get(v as number)?.name ?? t('（已删除的存放位置）');
    const tag = ix.tags.get(v as number);
    return tag ? ix.tagTree.find((x) => x.tag.id === tag.id)?.path ?? tag.name : t('（已删除的标签）');
  };
  // v1.6.1：“不是”“全部含有”“不含”写进胶囊
  const groupLabel = (g: FilterGroup) => {
    if (g === 'tag') return L.f.tagMode === 'all' ? t('标签（全部含有）') : L.f.tagMode === 'none' ? t('不含标签') : t('标签');
    if (g === 'type' && L.f.typeNot) return t('类型不是');
    if (g === 'correspondent' && L.f.correspondentNot) return t('往来方不是');
    if (g === 'person' && L.f.personNot) return t('家人不是');
    return LABEL[g];
  };
  for (const g of GROUPS) {
    for (const v of (L.f[g] ?? []) as (number | string)[]) out.push({ key: `${g}:${v}`, text: t('{label}：{value}', { label: groupLabel(g), value: name(g, v) }), remove: (l) => toggleIn(l, g, v) });
  }
  if (L.f.tagNone) out.push({ key: 'tagNone', text: t('没有标签'), remove: (l) => ({ f: { ...l.f, tagNone: undefined }, sel: [] }) });
  (L.f.fields ?? []).forEach((x, i) => out.push({
    key: `field:${i}:${JSON.stringify(x.cond)}`, text: describeFieldCond(x.fc, fieldDefs?.get(x.fc.fieldId)),
    remove: (l) => ({ f: { ...l.f, fields: (l.f.fields ?? []).filter((_, j) => j !== i) }, sel: [] }),
  }));
  if (L.f.kinds?.length) out.push({ key: 'kinds', text: t('文件格式：{kinds}', { kinds: L.f.kinds.map((k) => FILE_KINDS.find(([x]) => x === k)?.[1] ?? k).join(t('、')) }), remove: (l) => ({ f: { ...l.f, kinds: undefined }, sel: [] }) });
  if (L.f.asn) out.push({ key: 'asn', text: describeAsn(L.f.asn), remove: (l) => ({ f: { ...l.f, asn: undefined }, sel: [] }) });
  if (L.f.date) out.push({ key: 'date', text: describeDate(L.f.date), remove: (l) => ({ f: { ...l.f, date: undefined }, sel: [] }) });
  if (L.extra) out.push({ key: 'extra', text: L.extraLabel ?? t('自定义条件'), remove: () => ({ extra: undefined, extraLabel: undefined, sel: [] }) });
  return out;
}

function FilterButton({ group, state, ix, counts, onToggle, onFilters }: {
  group: FilterGroup; state: DocListState; ix: TaxonomyIndex; counts?: { key: string; count: number }[]; onToggle: (v: number | string) => void;
  /** 改“任一 / 全部 / 不含”“是 / 不是”“没有标签”（v1.6.1） */
  onFilters?: (p: Partial<ListFilters>) => void;
}) {
  const [el, setEl] = useState<HTMLElement | null>(null);
  const n = (state.f[group] ?? []).length + (group === 'tag' && state.f.tagNone ? 1 : 0);
  const countOf = new Map((counts ?? []).map((b) => [b.key, b.count]));
  let options: PickerOption[];
  if (group === 'year') {
    const years = [...new Set([...(counts ?? []).map((b) => b.key), ...state.f.year])].sort().reverse();
    options = years.map((y) => ({ id: Number(y), label: t('{y} 年', { y }) }));
  } else {
    options = group === 'drawer' ? drawerOptions(ix) : group === 'type' ? typeOptions(ix) : group === 'correspondent' ? correspondentOptions(ix)
      : group === 'person' ? personOptions(ix) : group === 'location' ? locationOptions(ix) : tagOptions(ix);
    if (group !== 'tag' && countOf.has('none')) options = [...options, { id: 0, label: NONE_LABEL[group] ?? t('没有') }];
  }
  options = options.map((o) => ({ ...o, count: countOf.get(o.id === 0 ? 'none' : String(o.id)) ?? 0 }));
  if (group === 'type' || group === 'correspondent' || group === 'person' || group === 'location') {
    // 文档多的在前（原型的做法）；数量为 0 的变淡但仍可选
    options.sort((a, b) => (b.count ?? 0) - (a.count ?? 0));
  }
  const selected = ((state.f[group] ?? []) as (number | string)[]).map((v) => (group === 'year' ? Number(v) : (v as number)));
  return (
    <>
      <Chip variant={n ? 'filled' : 'outlined'} aria-haspopup="listbox"
        label={<Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>{n ? `${LABEL[group]} · ${n}` : LABEL[group]}<ChevronDown size={16} strokeWidth={SW} /></Box>}
        onClick={(e) => setEl(e.currentTarget)} />
      <Picker anchor={el} onClose={() => setEl(null)} options={options} selected={selected} multi placeholder={t('搜索{group}', { group: LABEL[group] })}
        onPick={(id) => onToggle(group === 'year' ? String(id) : id)} header={onFilters && modeHeader(group, state, onFilters)} />
    </>
  );
}

/** 窄屏（手机）只显示标题和日期，其余列隐藏 */
const WIDE = { xs: 'none', md: 'flex' };
// 标题至少留这么宽：列多时先压缩其他的列（v1.5.6，加上自定义字段的列后容易挤）
const TITLE_MIN = 140;

function HeaderCell({ label, active, dir, onClick, flex, wide, minWidth = 0 }: { label: string; active: boolean; dir: 'asc' | 'desc'; onClick: () => void; flex: string; wide?: boolean; minWidth?: number }) {
  return (
    <Box component="button" onClick={onClick} title={label} sx={{ flex, minWidth, display: wide ? { xs: 'none', md: 'block' } : 'block', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left', border: 0, bgcolor: 'transparent', color: 'inherit', font: 'inherit', fontWeight: 500, cursor: 'pointer', p: 0, '&:hover': { color: 'text.primary' } }}>
      {label}{active ? (dir === 'asc' ? ' ↑' : ' ↓') : ''}
    </Box>
  );
}

/** 缩略图：有就显示真实的第一页，没有用纸张占位。 */
export function DocThumb({ d, w = 32, h = 42 }: { d: Pick<DocumentSummary, 'id' | 'hasThumbnail' | 'modifiedAt'> & { sensitive?: boolean }; w?: number | string; h?: number | string }) {
  const [failed, setFailed] = useState(false);
  // 敏感文档（v1.6.4）没有缩略图，显示一把锁
  if (d.sensitive) {
    return (
      <Box title={t('敏感文档')} aria-label={t('敏感文档')} sx={{ width: w, height: h, flex: 'none', display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'text.secondary',
        bgcolor: 'surface.containerLow', border: 1, borderColor: 'divider', borderRadius: '4px' }}>
        <Lock size={typeof w === 'number' && w < 48 ? 14 : 28} strokeWidth={SW} />
      </Box>
    );
  }
  if (!d.hasThumbnail || failed) return typeof w === 'number' && typeof h === 'number' ? <Thumb w={w} h={h} /> : null;
  return (
    <Box component="img" src={fileUrl(d.id, 'thumbnail', String(new Date(d.modifiedAt).getTime()))} alt="" loading="lazy" onError={() => setFailed(true)}
      sx={{ width: w, height: h, flex: 'none', objectFit: 'cover', objectPosition: 'top', bgcolor: 'background.paper', border: 1, borderColor: 'divider', borderRadius: '4px' }} />
  );
}

/** 悬停预览（v1.5.2）：鼠标在列表的缩略图上停一会儿，弹出第一页的大图；触屏不弹。 */
function HoverPreview({ d, children }: { d: Pick<DocumentSummary, 'id' | 'hasThumbnail' | 'modifiedAt' | 'title' | 'sensitive'>; children: React.ReactNode }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  if (!d.hasThumbnail || d.sensitive) return <>{children}</>;
  const leave = () => { window.clearTimeout(timer.current); setAnchor(null); };
  return (
    <Box sx={{ display: 'flex', flex: 'none' }} onMouseLeave={leave}
      onPointerEnter={(e) => {
        if (e.pointerType !== 'mouse') return;
        const el = e.currentTarget;
        window.clearTimeout(timer.current);
        timer.current = window.setTimeout(() => setAnchor(el), 450);
      }}>
      {children}
      <Popper open={!!anchor} anchorEl={anchor} placement="right-start" sx={{ zIndex: 1300, pointerEvents: 'none' }}
        modifiers={[{ name: 'offset', options: { offset: [0, 12] } }, { name: 'flip', options: { fallbackPlacements: ['left-start', 'bottom'] } }]}>
        <Paper elevation={6} sx={{ p: 0.75, borderRadius: 2 }} role="img" aria-label={t('“{title}”的第一页', { title: d.title })}>
          <Box component="img" src={fileUrl(d.id, 'thumbnail', String(new Date(d.modifiedAt).getTime()))} alt=""
            sx={{ display: 'block', width: 280, maxHeight: '70vh', objectFit: 'contain', objectPosition: 'top', borderRadius: 1 }} />
        </Paper>
      </Popper>
    </Box>
  );
}

function Tags({ ids, ix, max = 2, onFilter }: { ids: number[]; ix: TaxonomyIndex; max?: number; onFilter?: Filterer }) {
  const tags = ids.map((id) => ix.tags.get(id)).filter((tag) => !!tag);
  return (
    <>
      {tags.slice(0, max).map((tag) => (
        <Box key={tag.id} component={onFilter ? 'button' : 'span'} type={onFilter ? 'button' : undefined} title={onFilter ? t('{tag}（点一下只看带这个标签的）', { tag: ix.tagTree.find((x) => x.tag.id === tag.id)?.path ?? tag.name }) : ix.tagTree.find((x) => x.tag.id === tag.id)?.path ?? tag.name}
          onClick={onFilter ? (e: React.MouseEvent) => { e.stopPropagation(); onFilter('tag', tag.id); } : undefined}
          sx={onFilter ? { border: 0, p: 0, bgcolor: 'transparent', font: 'inherit', color: 'inherit', cursor: 'pointer', borderRadius: 1, '&:hover': { textDecoration: 'underline' } } : undefined}>
          <TagDot name={tag.name} color={tag.color} />
        </Box>
      ))}
      {tags.length > max && <Typography component="span" sx={{ fontSize: 12, color: 'text.secondary' }}>+{tags.length - max}</Typography>}
    </>
  );
}

/** onToggle(range)：range 为按住 Shift 点的，选中一段 */
/** 点列表里的标签、往来方、类型：把它加进当前列表的筛选条件（v1.6.0） */
type Filterer = (group: 'tag' | 'correspondent' | 'type' | 'person' | 'location', id: number) => void;

interface RowProps { d: DocumentSummary; ix: TaxonomyIndex; checked: boolean; onToggle: (range: boolean) => void; onOpen: () => void; onFilter?: Filterer }

/**
 * 文档标题是真正的链接（v1.6.0）：⌘ / Ctrl 点、Shift 点、中键点在新标签页或新窗口打开；
 * 普通点击照旧在当前页打开（记下来源列表，上一份 / 下一份和返回位置都在）。
 */
function TitleLink({ d, onOpen, children, sx }: { d: DocumentSummary; onOpen: () => void; children: React.ReactNode; sx?: object }) {
  return (
    <Box component={RouterLink} to={docHref(d.id)} draggable={false} onClick={docLinkClick(onOpen)}
      sx={{ color: 'inherit', textDecoration: 'none', minWidth: 0, '&:hover': { textDecoration: 'underline' }, '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', borderRadius: 0.5 }, ...sx }}>
      {children}
    </Box>
  );
}

/** 复选框的 onChange：点击时 nativeEvent 是鼠标事件，带着 Shift 键的状态。 */
const shiftOf = (e: React.ChangeEvent) => !!(e.nativeEvent as MouseEvent).shiftKey;

function Row({ d, ix, compact, cols, checked, current, onToggle, onOpen, onFilter }: RowProps & { compact: boolean; current: boolean; cols: (readonly [ColumnKey, Column])[] }) {
  const h = d.highlight;
  return (
    <Box id={`doc-row-${d.id}`} onClick={onOpen}
      sx={{ display: 'flex', alignItems: 'center', gap: 1.5, minHeight: compact ? 40 : 56, px: 1, py: 0.5, borderRadius: 2, cursor: 'pointer',
        bgcolor: checked ? 'surface.secondaryContainer' : 'transparent', outline: current ? '2px solid' : 'none', outlineColor: 'primary.main', outlineOffset: -2,
        transition: 'background 150ms ease-out', '&:hover': { bgcolor: checked ? 'surface.secondaryContainer' : 'action.hover' } }}>
      <Checkbox size="small" checked={checked} slotProps={{ input: { 'aria-label': t('选择“{title}”', { title: d.title }) } }} onClick={(e) => e.stopPropagation()} onChange={(e) => onToggle(shiftOf(e))} sx={{ p: 0.25, opacity: checked ? 1 : 0.5 }} />
      {!compact && <HoverPreview d={d}><DocThumb d={d} /></HoverPreview>}
      <Box sx={{ flex: 1, minWidth: TITLE_MIN }}>
        <Typography noWrap>
          {d.private && <Box component="span" title={t('家人的私密文档')} aria-label={t('私密')} sx={{ display: 'inline-flex', verticalAlign: '-2px', mr: 0.5, color: 'text.secondary' }}><EyeOff size={14} strokeWidth={SW} /></Box>}
          <TitleLink d={d} onOpen={onOpen}>{d.title}</TitleLink>
        </Typography>
        {h && h.length > 0 && (
          <Typography noWrap sx={{ fontSize: 12, color: 'text.secondary' }}>
            {h.map((seg, i) => (seg.hit
              ? <Box key={i} component="mark" sx={{ bgcolor: 'surface.secondaryContainer', color: 'text.primary', borderRadius: 0.5, px: 0.25 }}>{seg.text}</Box>
              : <span key={i}>{seg.text}</span>))}
          </Typography>
        )}
      </Box>
      {cols.map(([k, c]) => {
        return <Box key={k} sx={{ flex: c.flex, minWidth: 0, display: c.wide ? WIDE : 'flex', alignItems: 'center', justifyContent: c.align === 'right' ? 'flex-end' : undefined, fontSize: 13 }}>{c.cell(d, ix, onFilter)}</Box>;
      })}
    </Box>
  );
}

// 列表的列：标题之外可以选择显示的列（UI 设计规范 5.2；常用筛选会保存）。
interface Column { label: string; flex: string; wide?: boolean; align?: 'right'; sort?: DocumentSort; cell: (d: DocumentSummary, ix: TaxonomyIndex, onFilter?: Filterer) => React.ReactNode }

/** 往来方、类型的单元格：点一下只看它的文档。 */
function FilterCell({ name, onClick, muted: m }: { name?: string; onClick?: () => void; muted?: boolean }) {
  if (!name) return null;
  const color = m ? 'text.secondary' : 'text.primary';
  if (!onClick) return <Typography noWrap sx={{ fontSize: 13, color }}>{name}</Typography>;
  return (
    <Box component="button" type="button" title={t('只看“{name}”的文档', { name })} onClick={(e: React.MouseEvent) => { e.stopPropagation(); onClick(); }}
      sx={{ border: 0, p: 0, bgcolor: 'transparent', font: 'inherit', fontSize: 13, color, cursor: 'pointer', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', textAlign: 'left', '&:hover': { textDecoration: 'underline' } }}>
      {name}
    </Box>
  );
}
const muted = (v: React.ReactNode) => <Typography noWrap sx={{ fontSize: 13, color: 'text.secondary', fontVariantNumeric: 'tabular-nums' }}>{v}</Typography>;
type BuiltInColumn = Exclude<ColumnKey, `field:${number}`>;
export const COLUMN_ORDER: BuiltInColumn[] = ['correspondent', 'person', 'type', 'drawer', 'location', 'date', 'added', 'asn', 'amount', 'dueDate', 'notes', 'pages', 'tags'];
export const COLUMNS: Record<BuiltInColumn, Column> = {
  correspondent: { label: t('往来方'), flex: '0 1 150px', wide: true, sort: 'correspondent', cell: (d, ix, f) => <FilterCell muted name={d.correspondentId ? ix.correspondents.get(d.correspondentId)?.name : undefined} onClick={f && d.correspondentId ? () => f('correspondent', d.correspondentId!) : undefined} /> },
  // v1.6.3：家人、存放位置
  person: { label: t('家人'), flex: '0 1 80px', wide: true, cell: (d, ix, f) => <FilterCell muted name={d.personId ? ix.persons.get(d.personId)?.name : undefined} onClick={f && d.personId ? () => f('person', d.personId!) : undefined} /> },
  location: { label: t('存放位置'), flex: '0 1 110px', wide: true, cell: (d, ix, f) => <FilterCell muted name={d.locationId ? ix.locations.get(d.locationId)?.name : undefined} onClick={f && d.locationId ? () => f('location', d.locationId!) : undefined} /> },
  type: { label: t('类型'), flex: '0 1 90px', wide: true, cell: (d, ix, f) => <FilterCell name={d.documentTypeId ? ix.types.get(d.documentTypeId)?.name : undefined} onClick={f && d.documentTypeId ? () => f('type', d.documentTypeId!) : undefined} /> },
  drawer: {
    label: t('抽屉'), flex: '0 1 110px', wide: true, cell: (d, ix) => {
      const drawer = d.drawerId ? ix.drawers.get(d.drawerId) : undefined;
      return drawer ? <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, minWidth: 0 }}><DrawerIcon drawer={drawer} size={20} /><Typography noWrap sx={{ fontSize: 13 }}>{drawer.name}</Typography></Box> : muted('—');
    },
  },
  date: { label: t('日期'), flex: '0 0 96px', sort: 'created', cell: (d) => muted(d.createdDate) },
  added: { label: t('添加时间'), flex: '0 0 96px', wide: true, sort: 'added', cell: (d) => muted(d.addedAt.slice(0, 10)) },
  asn: { label: t('原件编号'), flex: '0 0 80px', wide: true, sort: 'asn', cell: (d) => muted(d.asn ? `#${String(d.asn).padStart(6, '0')}` : '') },
  amount: { label: t('金额'), flex: '0 0 96px', wide: true, align: 'right', sort: 'amount', cell: (d) => muted(d.amount ? `¥${d.amount}` : '') },
  dueDate: { label: t('到期日'), flex: '0 0 96px', wide: true, sort: 'due', cell: (d) => muted(d.dueDate ?? '') },
  tags: { label: tc('复数', '标签'), flex: '0 3 140px', wide: true, cell: (d, ix, f) => <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.25, overflow: 'hidden', minWidth: 0 }}><Tags ids={d.tagIds} ix={ix} onFilter={f} /></Box> },
  // v1.6.0：备注数和页数
  notes: { label: t('备注'), flex: '0 0 56px', wide: true, align: 'right', cell: (d) => (d.noteCount ? <Box title={t('{n} 条备注', { n: d.noteCount })} sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, color: 'text.secondary', fontSize: 13 }}><MessageSquare size={14} strokeWidth={SW} />{d.noteCount}</Box> : null) },
  pages: { label: t('页数'), flex: '0 0 56px', wide: true, align: 'right', cell: (d) => muted(d.pageCount ? String(d.pageCount) : '') },
};

/** 自定义字段的列（v1.5.6）。 */
const fieldColumn = (f: CustomField): Column => ({ label: f.name, flex: '0 1 110px', wide: true, align: ['integer', 'float', 'monetary'].includes(f.dataType) ? 'right' : undefined, sort: `field:${f.id}`, cell: (d) => <FieldCell f={f} d={d} /> });

function Card({ d, ix, checked, onToggle, onOpen }: RowProps) {
  const title = <TitleLink d={d} onOpen={onOpen}>{d.title}</TitleLink>;
  return (
    <Box onClick={onOpen} sx={{ cursor: 'pointer', '&:hover .cb': { opacity: 1 } }}>
      <Box sx={{ position: 'relative', aspectRatio: '1 / 1.414', bgcolor: 'background.paper', border: 1, borderColor: checked ? 'primary.main' : 'divider', borderRadius: 2, overflow: 'hidden', transition: 'box-shadow 150ms ease-out', '&:hover': { boxShadow: 1 } }}>
        <Checkbox className="cb" size="small" checked={checked} slotProps={{ input: { 'aria-label': t('选择“{title}”', { title: d.title }) } }} onClick={(e) => e.stopPropagation()} onChange={(e) => onToggle(shiftOf(e))}
          sx={{ position: 'absolute', left: 4, top: 4, zIndex: 1, p: 0.5, bgcolor: 'background.paper', borderRadius: 1, opacity: checked ? 1 : 0, transition: 'opacity 150ms' }} />
        {d.hasThumbnail || d.sensitive
          ? <DocThumb d={d} w="100%" h="100%" />
          : <Box sx={{ p: '28px 18px', display: 'flex', flexDirection: 'column', gap: '7px' }}>{[55, 100, 100, 80, 40, 65].map((w, i) => <Box key={i} sx={{ height: i ? 3 : 5, width: `${w}%`, bgcolor: 'surface.containerHigh', mt: i === 4 ? 1.5 : 0 }} />)}</Box>}
      </Box>
      <Typography sx={{ mt: 1, fontSize: 13, fontWeight: 500, display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>{title}</Typography>
      <Typography noWrap sx={{ fontSize: 12, color: 'text.secondary' }}>{[d.correspondentId ? ix.correspondents.get(d.correspondentId)?.name : '', d.createdDate].filter(Boolean).join(' · ')}</Typography>
    </Box>
  );
}

function ListSkeleton({ compact, rows = 8 }: { compact: boolean; rows?: number }) {
  return (
    <Box aria-busy="true" aria-label={t('正在加载')} sx={{ mt: 1.5 }}>
      {Array.from({ length: rows }, (_, i) => (
        <Stack key={i} direction="row" spacing={1.5} sx={{ alignItems: 'center', height: compact ? 40 : 56, px: 1 }}>
          <Skeleton variant="rounded" width={18} height={18} />
          {!compact && <Skeleton variant="rounded" width={32} height={42} />}
          <Skeleton sx={{ flex: 1 }} />
          <Skeleton width={120} />
          <Skeleton width={90} />
        </Stack>
      ))}
    </Box>
  );
}

/** 选中后浮在底部中央的批量操作栏。 */
function BatchBar({ count, selection, items, ix, canWrite, onDone, extra }: { count: number; selection: { ids: number[] } | { filter: string }; items: DocumentSummary[]; ix: TaxonomyIndex; canWrite: boolean; onDone: () => void; extra?: React.ReactNode }) {
  const app = useApp();
  const bulk = useBulk();
  const restore = useRestoreDocument();
  const [picker, setPicker] = useState<{ kind: 'type' | 'corr' | 'drawer' | 'person' | 'location'; el: HTMLElement } | null>(null);
  const [tagsOpen, setTagsOpen] = useState(false); // 批量改标签（v1.5.6）
  const [downloadEl, setDownloadEl] = useState<HTMLElement | null>(null);
  // 选中了敏感文档、又没有确认过密码：先确认，再下载（v1.6.4）
  const unlocked = useUnlocked();
  const [verifyThen, setVerifyThen] = useState<string | null>(null);
  const hasSensitive = 'ids' in selection ? selection.ids.some((id) => items.find((d) => d.id === id)?.sensitive) : items.some((d) => d.sensitive);
  const download = (url: string) => { setDownloadEl(null); if (hasSensitive && !unlocked) setVerifyThen(url); else startDownload(url); };
  const sel = 'ids' in selection ? { ids: selection.ids } : { filter: JSON.parse(selection.filter || '{}') as Record<string, unknown> };

  const run = (edit: Record<string, unknown>, done: (n: number) => string) => {
    bulk.mutate({ ...sel, edit }, {
      onSuccess: (r) => { app.toast(done(r.count)); onDone(); },
      onError: (e) => app.toast(e.message),
    });
  };
  const pick = (id: number) => {
    if (!picker) return;
    if (picker.kind === 'type') run({ documentTypeId: id }, (n) => id ? t('已把 {n} 份文档的类型设为“{name}”', { n, name: ix.types.get(id)?.name ?? '' }) : t('已清空 {n} 份文档的类型', { n }));
    if (picker.kind === 'drawer') run({ drawerId: id }, (n) => id ? t('已把 {n} 份文档放入“{name}”', { n, name: ix.drawers.get(id)?.name ?? '' }) : t('已把 {n} 份文档设为未入抽屉', { n }));
    if (picker.kind === 'corr') run({ correspondentId: id }, (n) => id ? t('已把 {n} 份文档的往来方设为“{name}”', { n, name: ix.correspondents.get(id)?.name ?? '' }) : t('已清空 {n} 份文档的往来方', { n }));
    if (picker.kind === 'person') run({ personId: id }, (n) => id ? t('已把 {n} 份文档设为“{name}”的', { n, name: ix.persons.get(id)?.name ?? '' }) : t('已清空 {n} 份文档的家人', { n }));
    if (picker.kind === 'location') run({ locationId: id }, (n) => id ? t('已记下 {n} 份文档的原件在“{name}”', { n, name: ix.locations.get(id)?.name ?? '' }) : t('已清空 {n} 份文档的存放位置', { n }));
  };
  const remove = () => app.confirm({
    title: t('删除 {n} 份文档？', { n: count }), body: t('文档会移到回收站，30 天后自动清除。在此之前可以从回收站恢复。'), verb: t('删除'),
    run: () => {
      app.confirm(null);
      bulk.mutate({ ...sel, delete: true }, {
        onSuccess: (r) => {
          const ids = 'ids' in selection ? selection.ids : null;
          app.toast(t('已删除 {n} 份文档', { n: r.count }), ids ? () => { ids.forEach((id) => restore.mutate(id, { onError: (e) => app.toast(t('没能恢复：{error}', { error: e.message })) })); app.set({ toast: null }); } : undefined);
          onDone();
        },
        onError: (e) => app.toast(e.message),
      });
    },
  });
  // 重新处理（v1.5.6）：重新识别文字、生成可搜索版和缩略图，用在改了识别设置之后
  const reprocess = () => app.confirm({
    title: t('重新处理 {n} 份文档？', { n: count }), body: t('会重新识别文字，生成可搜索版和缩略图；手动改过的识别文字会被新识别的文字覆盖。文档较多时需要一段时间。'), verb: t('重新处理'), safe: true,
    run: () => {
      app.confirm(null);
      bulk.mutate({ ...sel, reprocess: true }, {
        onSuccess: (r) => { app.toast(r.count ? t('正在后台重新处理 {n} 份文档', { n: r.count }) : t('没有可以重新处理的文档')); onDone(); },
        onError: (e) => app.toast(e.message),
      });
    },
  });
  const [classifyEl, setClassifyEl] = useState<HTMLElement | null>(null);
  const classify = (mode: 'fill' | 'overwrite') => {
    setClassifyEl(null);
    bulk.mutate({ ...sel, classify: { mode } }, {
      onSuccess: (r) => { app.toast(r.count ? t('正在后台重新分类 {n} 份文档，完成后列表会自动更新。', { n: r.count }) : t('没有可以重新分类的文档')); onDone(); },
      onError: (e) => app.toast(e.message),
    });
  };
  // 合并：选中的都已载入、都是 PDF，2 到 50 份
  const [mergeOpen, setMergeOpen] = useState(false);
  const mergeDocs = useMemo(() => {
    if (!('ids' in selection) || selection.ids.length < 2 || selection.ids.length > 50) return null;
    const byId = new Map(items.map((d) => [d.id, d]));
    const picked = selection.ids.map((id) => byId.get(id));
    return picked.every((d) => d && isPdfDocument(d)) ? picked.map((d) => ({ id: d!.id, title: d!.title })) : null;
  }, [selection, items]);
  // 通过邮件发送：配置了发信，选中的都已载入，最多 50 份
  const smtp = useMe().data?.features?.smtp ?? false;
  const [emailOpen, setEmailOpen] = useState(false);
  const emailDocs = useMemo(() => {
    if (!smtp || !('ids' in selection) || !selection.ids.length || selection.ids.length > 50) return null;
    const byId = new Map(items.map((d) => [d.id, d]));
    const picked = selection.ids.map((id) => byId.get(id));
    return picked.every(Boolean) ? picked.map((d) => ({ id: d!.id, title: d!.title })) : null;
  }, [smtp, selection, items]);
  // 让 AI 整理（阶段 31）：开启了 AI，按 ID 选中，最多 50 份
  const ai = useAgentSettings(eeBuild);
  const organize = useOrganizeDocuments();
  const organizeIds = ai.data?.enabled && 'ids' in selection && selection.ids.length <= 50 ? selection.ids : null;
  const runOrganize = () => organizeIds && organize.mutate(organizeIds, {
    onSuccess: (r) => { app.toast(r.queued ? t('AI 正在整理 {n} 份文档，建议会出现在各文档的“AI 建议”里', { n: r.queued }) : t('没有可以整理的文档')); onDone(); },
    onError: (e) => app.toast(e.message),
  });
  // 共享给其他空间（v1.0 阶段 37）：空间管理员，按 ID 选中，最多 1000 份
  const isOwner = useRole().isOwner;
  const [sharing, setSharing] = useState(false);
  const [bundling, setBundling] = useState(false); // 分享包（v1.4.2）
  const [fieldOpen, setFieldOpen] = useState(false); // 批量设字段（v1.5.2）
  const shareIds = isOwner && 'ids' in selection && selection.ids.length <= 1000 ? selection.ids : null;
  // 移到其他空间（v1.3.8）：按 ID 选中，最多 200 份；移完取消选择
  const moveDocs = 'ids' in selection && selection.ids.length <= 200
    ? selection.ids.map((id) => ({ id, title: items.find((d) => d.id === id)?.title ?? '' }))
    : null;
  const options = picker?.kind === 'type' ? typeOptions(ix) : picker?.kind === 'corr' ? correspondentOptions(ix)
    : picker?.kind === 'person' ? personOptions(ix) : picker?.kind === 'location' ? locationOptions(ix) : drawerOptions(ix);
  const PICKED: Record<string, [string, string]> = { type: [t('搜索类型'), t('清空类型')], corr: [t('搜索往来方'), t('清空往来方')], drawer: [t('搜索抽屉'), t('未入抽屉')], person: [t('搜索家人'), t('清空家人')], location: [t('搜索存放位置'), t('清空存放位置')] };

  return (
    <Paper elevation={2} role="toolbar" aria-label={t('批量操作')} sx={{ position: 'fixed', left: '50%', bottom: 24, transform: 'translateX(-50%)', zIndex: 1200, display: 'flex', alignItems: 'center', gap: 0.5, flexWrap: 'wrap', maxWidth: 'calc(100vw - 32px)', p: 1, pl: 2, borderRadius: 3, bgcolor: 'surface.container' }}>
      <Typography variant="h6" sx={{ mr: 1 }}>{t('已选 {n} 份', { n: count })}</Typography>
      {extra}
      {canWrite && <>
        <Button size="small" disabled={bulk.isPending} onClick={() => setTagsOpen(true)}>{tc('复数', '标签')}</Button>
        {tagsOpen && <TagBatchDialog open selection={sel} items={items} count={count} ix={ix} onClose={() => setTagsOpen(false)} onDone={onDone} />}
        <Button size="small" disabled={bulk.isPending} onClick={(e) => setPicker({ kind: 'type', el: e.currentTarget })}>{t('设类型')}</Button>
        <Button size="small" disabled={bulk.isPending} onClick={(e) => setPicker({ kind: 'corr', el: e.currentTarget })}>{t('设往来方')}</Button>
        {ix.personList.length > 0 && <Button size="small" disabled={bulk.isPending} onClick={(e) => setPicker({ kind: 'person', el: e.currentTarget })}>{t('设家人')}</Button>}
        {ix.locationList.length > 0 && <Button size="small" disabled={bulk.isPending} onClick={(e) => setPicker({ kind: 'location', el: e.currentTarget })}>{t('设存放位置')}</Button>}
        <Button size="small" disabled={bulk.isPending} onClick={(e) => setPicker({ kind: 'drawer', el: e.currentTarget })}>{t('放入抽屉')}</Button>
        <Button size="small" disabled={bulk.isPending} onClick={() => setFieldOpen(true)}>{t('设字段')}</Button>
        {fieldOpen && <BulkFieldDialog open selection={sel} count={count} onClose={() => setFieldOpen(false)} onDone={onDone} />}
        <Button size="small" disabled={bulk.isPending} onClick={(e) => setClassifyEl(e.currentTarget)}>{t('重新分类')}</Button>
        <Button size="small" disabled={bulk.isPending} onClick={reprocess}>{t('重新处理')}</Button>
        {eeBuild && organizeIds && <Button size="small" disabled={organize.isPending} onClick={runOrganize}>{t('让 AI 整理')}</Button>}
        {eeBuild && moveDocs && <Button size="small" onClick={() => setBundling(true)}>{t('生成分享链接')}</Button>}
        {moveDocs && bundling && <ShareBundleDialog open docs={moveDocs} onClose={() => { setBundling(false); onDone(); }} />}
        {moveDocs && <Button size="small" onClick={() => { app.set({ moveDocs }); onDone(); }}>{t('移到其他空间')}</Button>}
        {eeBuild && shareIds && <Button size="small" onClick={() => setSharing(true)}>{t('共享给其他空间')}</Button>}
        {shareIds && sharing && <CollectionDialog open documentIds={shareIds} onClose={() => setSharing(false)} onDone={onDone} />}
        {mergeDocs && <Button size="small" onClick={() => setMergeOpen(true)}>{t('合并为一份')}</Button>}
        {mergeDocs && <MergeDialog docs={mergeDocs} open={mergeOpen} onClose={() => setMergeOpen(false)} onDone={onDone} />}
        {emailDocs && <Button size="small" onClick={() => setEmailOpen(true)}>{t('通过邮件发送')}</Button>}
        {emailDocs && <EmailDialog docs={emailDocs} open={emailOpen} onClose={() => setEmailOpen(false)} />}
        <Menu anchorEl={classifyEl} open={!!classifyEl} onClose={() => setClassifyEl(null)}>
          <MenuItem onClick={() => classify('fill')}><ListItemText primary={t('只补空')} secondary={t('只填空着的类型和往来方，只加标签')} /></MenuItem>
          <MenuItem onClick={() => classify('overwrite')}><ListItemText primary={t('覆盖')} secondary={t('替换以前自动分类的结果；手动改过的不动')} /></MenuItem>
        </Menu>
      </>}
      <Button size="small" disabled={count > 1000} title={count > 1000 ? t('一次最多下载 {n} 份', { n: 1000 }) : undefined} endIcon={<ChevronDown size={14} strokeWidth={SW} />} onClick={(e) => setDownloadEl(e.currentTarget)}>{t('下载')}</Button>
      <Menu anchorEl={downloadEl} open={!!downloadEl} onClose={() => setDownloadEl(null)}>
        <MenuItem onClick={() => download(downloadUrl(selection, 'original'))}><ListItemText primary={t('原件')} secondary={t('上传时的文件')} /></MenuItem>
        <MenuItem onClick={() => download(downloadUrl(selection, 'archive'))}><ListItemText primary={t('可搜索版')} secondary={t('识别过文字的 PDF；没有的用原件')} /></MenuItem>
      </Menu>
      {canWrite && <Button size="small" color="error" disabled={bulk.isPending} onClick={remove}>{t('删除')}</Button>}
      <VerifyDialog open={!!verifyThen} onClose={() => setVerifyThen(null)} onDone={() => verifyThen && startDownload(verifyThen)} />
      <Tooltip title={t('取消选择')}><IconButton size="small" aria-label={t('取消选择')} onClick={onDone}><X size={18} strokeWidth={SW} /></IconButton></Tooltip>
      <Picker anchor={picker?.el ?? null} onClose={() => setPicker(null)} options={options} selected={[]} onPick={pick}
        placeholder={picker ? PICKED[picker.kind][0] : ''} clearLabel={picker ? PICKED[picker.kind][1] : undefined} />
    </Paper>
  );
}

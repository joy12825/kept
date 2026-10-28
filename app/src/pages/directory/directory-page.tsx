// 目录：往来方、家人、标签、类型、自定义字段，按拼音首字母分组（UI 设计规范 5.8）；存放位置单独一页（v1.6.3）
import { useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { Box, Button, ButtonBase, Chip, InputBase, Paper, Stack, ToggleButton, ToggleButtonGroup, Typography } from '@mui/material';
import { BookUser, Plus, Search, Shapes, SlidersHorizontal, Square, SquareCheck, Tags, Users } from 'lucide-react';
import {
  KIND_LABEL, algorithmLabel, currencyLabel, fieldTypeLabel, retentionLabel, tagPath, useTaxonomy,
  type CustomField, type DirectoryKind, type Tag, type TaxonomyItem, type TaxonomyKind,
} from '../../api/taxonomy';
import { useRole } from '../../api/workspaces';
import { EmptyState, ErrorState } from '../../components/common';
import { SW } from '../../components/icons';
import { EditPanel } from './edit-panel';
import { OrganizeSuggestions } from '@ee';
import { DirectoryBulkBar } from './bulk-bar';
import { LocationsPanel } from './locations-panel';
import { t } from '../../i18n';

const KINDS: TaxonomyKind[] = ['correspondents', 'persons', 'tags', 'document-types', 'custom-fields'];
/** 目录的分页：分类对象，加上存放位置 */
type Tab = TaxonomyKind | 'locations';
/** 整理建议按分页显示；自定义字段没有建议。 */
const SUGGEST_TARGET: Record<TaxonomyKind, 'correspondent' | 'tag' | 'type' | undefined> = { correspondents: 'correspondent', tags: 'tag', 'document-types': 'type', 'custom-fields': undefined, persons: undefined };
const LETTERS = [...'ABCDEFGHIJKLMNOPQRSTUVWXYZ', '#'];

const EMPTY: Record<TaxonomyKind, { icon: React.ReactNode; title: string; create: string; text: string }> = {
  correspondents: { icon: <BookUser size={40} strokeWidth={SW} />, title: t('还没有往来方'), create: t('新建往来方'), text: t('往来方是文档的来源或对象，比如国家电网、招商银行。') },
  persons: { icon: <Users size={40} strokeWidth={SW} />, title: t('还没有家人'), create: t('新建家人'), text: t('家人是文档属于谁，比如孩子的护照、爸爸的体检报告。可以按家人筛选，到期提醒也会写明是谁的。') },
  tags: { icon: <Tags size={40} strokeWidth={SW} />, title: t('还没有标签'), create: t('新建标签'), text: t('标签用来标记用途，比如税务、报销，一份文档可以有多个标签。') },
  'document-types': { icon: <Shapes size={40} strokeWidth={SW} />, title: t('还没有类型'), create: t('新建类型'), text: t('类型描述文档是什么，比如账单、合同、发票。') },
  'custom-fields': { icon: <SlidersHorizontal size={40} strokeWidth={SW} />, title: t('还没有自定义字段'), create: t('新建自定义字段'), text: t('自定义字段用来记录文档的额外信息，比如户号、保单号。') },
};

type Entry = { id: number; name: string; initial: string; sortKey: string };
/** 列表里的一行：depth 是标签的层级缩进，path 是查找标签时显示的完整路径。 */
type Row = { item: Entry; depth: number; path?: string };

function meta(kind: TaxonomyKind, item: Entry): string {
  if (kind === 'custom-fields') {
    const f = item as CustomField;
    if (f.dataType === 'monetary') return t('金额 · {currency}', { currency: currencyLabel(f.extra.currency) });
    if (f.dataType === 'select') return t('单选 · {n} 个选项', { n: f.extra.options?.length ?? 0 });
    return fieldTypeLabel(f.dataType);
  }
  const it = item as TaxonomyItem;
  return [
    it.documentCount ? t('{n} 份', { n: it.documentCount }) : t('还没有文档'),
    it.documentCount && it.lastDocumentDate ? t('最近 {date}', { date: it.lastDocumentDate }) : '',
    t('匹配：{algorithm}', { algorithm: algorithmLabel(it.rule.algorithm) }),
    it.retentionMonths ? t('保留 {period}', { period: retentionLabel(it.retentionMonths) }) : '',
  ].filter(Boolean).join(' · ');
}

const matches = (e: Entry, q: string) => e.name.toLowerCase().includes(q) || e.sortKey.includes(q.replace(/\s+/g, ''));

/** 按首字母分组；标签显示为树：顶层标签分组，子标签缩进排在上级下面。 */
function groupRows(kind: TaxonomyKind, items: Entry[], q: string): { letter: string; rows: Row[] }[] {
  const query = q.trim().toLowerCase();
  let rows: Row[];
  if (kind === 'tags') {
    const tags = items as Tag[];
    const byId = new Map(tags.map((tg) => [tg.id, tg]));
    if (query) {
      rows = tags.filter((tg) => matches(tg, query)).map((tg) => ({ item: tg, depth: 0, path: tg.parentId ? tagPath(tg, byId) : undefined }));
    } else {
      const children = new Map<number, Tag[]>();
      for (const tg of tags) if (tg.parentId && byId.has(tg.parentId)) children.set(tg.parentId, [...(children.get(tg.parentId) ?? []), tg]);
      rows = [];
      const walk = (tg: Tag, depth: number) => { rows.push({ item: tg, depth }); for (const c of children.get(tg.id) ?? []) walk(c, depth + 1); };
      for (const tg of tags) if (!tg.parentId || !byId.has(tg.parentId)) walk(tg, 0);
    }
  } else {
    rows = items.filter((e) => !query || matches(e, query)).map((item) => ({ item, depth: 0 }));
  }
  // 子标签跟着上级走，所以按“所在的顶层行”的首字母分组
  const groups: { letter: string; rows: Row[] }[] = [];
  for (const r of rows) {
    const letter = r.depth > 0 ? groups[groups.length - 1].letter : r.item.initial;
    const g = groups.find((x) => x.letter === letter);
    if (g) g.rows.push(r); else groups.push({ letter, rows: [r] });
  }
  return LETTERS.map((l) => groups.find((g) => g.letter === l)).filter((g): g is { letter: string; rows: Row[] } => !!g);
}

export default function DirectoryPage() {
  const [params, setParams] = useSearchParams();
  const tab = params.get('tab');
  const locations = tab === 'locations';
  const kind = (KINDS.includes(tab as TaxonomyKind) ? tab : 'correspondents') as TaxonomyKind;
  const [newLocation, setNewLocation] = useState(false);
  const [q, setQ] = useState('');
  const [editing, setEditing] = useState<{ item: Entry | null } | null>(null);
  const role = useRole();
  const nav = useNavigate();
  const list = useTaxonomy(kind);
  const items = (list.data ?? []) as Entry[];
  const groups = useMemo(() => groupRows(kind, items, q), [kind, items, q]);
  const tags = useTaxonomy('tags');
  const byId = useMemo(() => new Map((tags.data ?? []).map((tg) => [tg.id, tg])), [tags.data]);

  // 多选（v1.5.8）：批量删除、合并；自定义字段不在内
  const [selecting, setSelecting] = useState(false);
  const [sel, setSel] = useState<number[]>([]);
  const canBulk = role.canWrite && kind !== 'custom-fields';
  const stopSelecting = () => { setSelecting(false); setSel([]); };
  const toggle = (id: number) => setSel((x) => (x.includes(id) ? x.filter((y) => y !== id) : [...x, id]));
  const switchTab = (k: Tab | null) => { if (k) { setParams({ tab: k }, { replace: true }); setQ(''); stopSelecting(); } };
  const jump = (l: string) => {
    const el = document.getElementById('L-' + l);
    if (el) window.scrollTo({ top: el.getBoundingClientRect().top + window.scrollY - 130, behavior: 'smooth' });
  };

  return (
    <Box sx={{ px: 3, pt: 0.5, pb: 10 }}>
      <Stack direction="row" spacing={1.5} useFlexGap sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
        <ToggleButtonGroup size="small" exclusive value={locations ? 'locations' : kind} onChange={(_, v) => switchTab(v)} aria-label={t('分类')}
          sx={{ maxWidth: '100%', overflowX: 'auto', scrollbarWidth: 'none', '& .MuiToggleButton-root': { flex: 'none', whiteSpace: 'nowrap' } }}>
          {KINDS.map((k) => <ToggleButton key={k} value={k}>{KIND_LABEL[k]}</ToggleButton>)}
          <ToggleButton value="locations">{t('存放位置')}</ToggleButton>
        </ToggleButtonGroup>
        <Box sx={{ flex: 1 }} />
        {locations ? role.canWrite && (
          <Button variant="tonal" startIcon={<Plus size={16} strokeWidth={SW} />} onClick={() => setNewLocation(true)}>{t('新建存放位置')}</Button>
        ) : <>
          <Paper elevation={0} sx={{ display: 'flex', alignItems: 'center', gap: 1, width: 240, height: 40, px: 1.75, borderRadius: 5, bgcolor: 'surface.containerHigh', color: 'text.secondary' }}>
            <Search size={16} strokeWidth={SW} />
            <InputBase value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('查找（可输入拼音）')} inputProps={{ 'aria-label': t('查找') }} sx={{ flex: 1, fontSize: 14 }} />
          </Paper>
          {canBulk && items.length > 1 && <Button variant={selecting ? 'contained' : 'outlined'} onClick={() => (selecting ? stopSelecting() : setSelecting(true))}>{selecting ? t('完成') : t('多选')}</Button>}
          {role.canWrite && (
            <Button variant="tonal" startIcon={<Plus size={16} strokeWidth={SW} />} onClick={() => setEditing({ item: null })}>{EMPTY[kind].create}</Button>
          )}
        </>}
      </Stack>
      {locations ? <LocationsPanel canWrite={role.canWrite} creating={newLocation} onCreated={() => setNewLocation((x) => !x)} /> : <>

      {SUGGEST_TARGET[kind] && <OrganizeSuggestions kind={SUGGEST_TARGET[kind]} canWrite={role.canWrite} />}

      {list.isError && <ErrorState onRetry={() => list.refetch()} />}
      {list.isSuccess && !items.length && (
        <EmptyState icon={EMPTY[kind].icon} title={EMPTY[kind].title} text={EMPTY[kind].text}
          action={role.canWrite ? EMPTY[kind].create : undefined} onAction={role.canWrite ? () => setEditing({ item: null }) : undefined} />
      )}
      {!!items.length && (
        <>
          <Stack direction="row" sx={{ flexWrap: 'wrap', position: 'sticky', top: 64, zIndex: 3, py: 1.25, mt: 1.5, bgcolor: 'background.default', borderBottom: 1, borderColor: 'divider' }}>
            {LETTERS.map((l) => {
              const has = groups.some((g) => g.letter === l);
              return (
                <ButtonBase key={l} disabled={!has} onClick={() => jump(l)} aria-label={t('跳到 {letter}', { letter: l })}
                  sx={{ width: 28, height: 28, borderRadius: '50%', fontSize: 12, fontWeight: 500, color: has ? 'text.primary' : 'text.disabled', '&:hover': { bgcolor: 'action.hover' } }}>
                  {l}
                </ButtonBase>
              );
            })}
          </Stack>
          {!groups.length && <Typography sx={{ color: 'text.secondary', py: 4, textAlign: 'center' }}>{t('没有找到“{q}”', { q: q.trim() })}</Typography>}
          {groups.map((g) => (
            <Box key={g.letter} id={'L-' + g.letter} sx={{ pt: 2 }}>
              <Typography variant="h6" sx={{ color: 'text.secondary', px: 1, pb: 0.5 }}>{g.letter}</Typography>
              {g.rows.map(({ item, depth, path }) => (
                <ButtonBase key={item.id} onClick={() => (selecting ? toggle(item.id) : kind === 'custom-fields' ? setEditing({ item }) : nav(`/directory/${kind}/${item.id}`))}
                  role={selecting ? 'checkbox' : undefined} aria-checked={selecting ? sel.includes(item.id) : undefined}
                  sx={{ width: '100%', display: 'flex', justifyContent: 'flex-start', gap: 2, minHeight: 44, px: 1, pl: 1 + depth * 3, borderRadius: 2, textAlign: 'left',
                    bgcolor: selecting && sel.includes(item.id) ? 'surface.secondaryContainer' : undefined, '&:hover': { bgcolor: selecting && sel.includes(item.id) ? 'surface.secondaryContainer' : 'action.hover' } }}>
                  {selecting && <Box aria-hidden sx={{ display: 'flex', mr: -1, color: sel.includes(item.id) ? 'primary.main' : 'text.secondary' }}>{sel.includes(item.id) ? <SquareCheck size={20} strokeWidth={SW} /> : <Square size={20} strokeWidth={SW} />}</Box>}
                  <Box sx={{ width: { xs: 160, sm: 240 }, flex: 'none', display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
                    {kind === 'tags' && <Box component="span" sx={{ width: 8, height: 8, borderRadius: '50%', flex: 'none', bgcolor: (item as Tag).color }} />}
                    <Typography noWrap>{path ?? item.name}</Typography>
                  </Box>
                  <Typography variant="body2" noWrap sx={{ color: 'text.secondary', flex: 1, minWidth: 0 }}>{meta(kind, item)}</Typography>
                  {kind === 'custom-fields' && (item as CustomField).builtIn && <Chip size="small" label={t('内置')} />}
                </ButtonBase>
              ))}
            </Box>
          ))}
        </>
      )}

      </>}

      {selecting && sel.length > 0 && canBulk && (
        <DirectoryBulkBar kind={kind as DirectoryKind} onDone={stopSelecting}
          picked={sel.map((id) => items.find((x) => x.id === id)).filter((x): x is Entry => !!x).map((x) => ({ id: x.id, name: x.name, documentCount: (x as TaxonomyItem).documentCount ?? 0 }))} />
      )}
      {editing && (
        <EditPanel
          key={`${kind}-${editing.item?.id ?? 'new'}`} kind={kind} item={editing.item as never} readOnly={!role.canWrite}
          tags={tags.data ?? []} tagsById={byId} onClose={() => setEditing(null)}
        />
      )}
    </Box>
  );
}

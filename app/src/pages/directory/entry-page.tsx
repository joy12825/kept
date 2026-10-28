// 目录的条目页（UI 设计规范 5.8）：一个往来方、类型或标签的文档数、时间跨度、每年文档数的迷你柱状图，
// 下面是它的文档列表（复用全部文件的列表）。“常一起出现”和匹配规则的“试一试”随阶段 6。
import { useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Box, Button, Stack, Typography } from '@mui/material';
import { ArrowLeft, Pencil } from 'lucide-react';
import { useFacets } from '../../api/documents';
import type { Cond, FilterGroup } from '../../api/filters';
import { KIND_LABEL, tagPath, useTaxonomy, type Tag, type TaxonomyItem, type TaxonomyKind } from '../../api/taxonomy';
import { useRole } from '../../api/workspaces';
import { EmptyState, LoadingState } from '../../components/common';
import DocList from '../../components/doc-list';
import { SW } from '../../components/icons';
import { YearSpark } from '../../components/charts-lazy';
import { EditPanel } from './edit-panel';
import { t } from '../../i18n';

const ENTRY_KINDS = ['correspondents', 'document-types', 'tags', 'persons'] as const;
type EntryKind = (typeof ENTRY_KINDS)[number];
const GROUP: Record<EntryKind, FilterGroup> = { correspondents: 'correspondent', 'document-types': 'type', tags: 'tag', persons: 'person' };

function baseCondition(kind: EntryKind, id: number): Cond {
  if (kind === 'tags') return { field: 'tag', op: 'has_any', value: [id], descendants: true };
  return { field: kind === 'correspondents' ? 'correspondent' : kind === 'persons' ? 'person' : 'type', op: 'in', value: [id] };
}

export default function EntryPage() {
  const params = useParams();
  const kind = params.kind as TaxonomyKind;
  const id = Number(params.id);
  const nav = useNavigate();
  const role = useRole();
  const valid = (ENTRY_KINDS as readonly string[]).includes(kind) && Number.isInteger(id);
  const list = useTaxonomy(valid ? kind : 'correspondents');
  const tags = useTaxonomy('tags');
  const byId = useMemo(() => new Map((tags.data ?? []).map((tg) => [tg.id, tg])), [tags.data]);
  const [editing, setEditing] = useState(false);
  const base = useMemo(() => (valid ? [baseCondition(kind as EntryKind, id)] : []), [valid, kind, id]);
  const years = useFacets(JSON.stringify(base[0] ?? {}), ['year'], valid);

  const back = () => nav(`/directory?tab=${valid ? kind : 'correspondents'}`);
  if (!valid) return <EmptyState title={t('找不到这一项')} text={t('它可能已被删除。')} action={t('回到目录')} onAction={back} />;
  if (list.isPending) return <LoadingState />;
  const item = list.data?.find((x) => x.id === id);
  if (!item) return <EmptyState title={t('找不到这一项')} text={t('这个{kind}可能已被删除。', { kind: KIND_LABEL[kind].toLowerCase() })} action={t('回到目录')} onAction={back} />;

  const name = kind === 'tags' ? tagPath(item as Tag, byId) : item.name;
  const buckets = [...(years.data?.year ?? [])].sort((a, b) => a.key.localeCompare(b.key));
  const first = buckets[0]?.key;
  const last = buckets[buckets.length - 1]?.key;
  const span: { year: string; count: number }[] = [];
  if (first && last) for (let y = Number(first); y <= Number(last); y++) span.push({ year: String(y), count: buckets.find((b) => b.key === String(y))?.count ?? 0 });
  const count = (item as TaxonomyItem | Tag).documentCount ?? 0;

  return (
    <Box sx={{ px: 3, pt: 0.5 }}>
      <Button size="small" startIcon={<ArrowLeft size={16} strokeWidth={SW} />} onClick={back} sx={{ color: 'text.secondary', mb: 1 }}>{t('目录')} · {KIND_LABEL[kind]}</Button>
      <Stack direction="row" spacing={3} useFlexGap sx={{ alignItems: 'flex-end', flexWrap: 'wrap', mb: 3 }}>
        <Box sx={{ minWidth: 0 }}>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
            {kind === 'tags' && <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: (item as Tag).color }} />}
            <Typography variant="h4" component="h2">{name}</Typography>
          </Stack>
          <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5 }}>
            {count ? [t('{n} 份', { n: count }), first ? (first === last ? first : `${first}–${last}`) : ''].filter(Boolean).join(' · ') : t('还没有文档')}
          </Typography>
        </Box>
        {span.length > 1 && (
          <YearSpark span={span} />
        )}
        <Box sx={{ flex: 1 }} />
        <Button variant="outlined" startIcon={<Pencil size={16} strokeWidth={SW} />} onClick={() => setEditing(true)}>{role.canWrite ? t('编辑') : t('查看设置')}</Button>
      </Stack>
      <DocList key={`${kind}-${id}`} listKey={`entry:${kind}:${id}`} base={base} hideGroups={[GROUP[kind as EntryKind]]}
        placeholder={t('在“{name}”中搜索…', { name: item.name })} backLabel={item.name}
        emptyTitle={t('“{name}”还没有文档', { name: item.name })} emptyText={t('文档设置了这一项后会出现在这里。')} />
      {editing && (
        <EditPanel kind={kind} item={item as never} readOnly={!role.canWrite} tags={tags.data ?? []} tagsById={byId}
          onClose={() => setEditing(false)} onDeleted={back} />
      )}
    </Box>
  );
}

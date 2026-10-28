import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';
import { t } from '../i18n';

export type MatchRule = components['schemas']['MatchRule'];
export type MatchAlgorithm = MatchRule['algorithm'];
export type TaxonomyItem = components['schemas']['TaxonomyItem'];
export type Tag = components['schemas']['Tag'];
export type CustomField = components['schemas']['CustomField'];
export type CustomFieldType = components['schemas']['CustomFieldType'];
export type CustomFieldExtra = components['schemas']['CustomFieldExtra'];
export type SelectOption = components['schemas']['SelectOption'];

/** 分类对象，值就是接口路径的最后一段；家人（persons）是 v1.6.3 加的。 */
export type TaxonomyKind = 'correspondents' | 'tags' | 'document-types' | 'custom-fields' | 'persons';

export interface KindRecord {
  correspondents: TaxonomyItem;
  'document-types': TaxonomyItem;
  persons: TaxonomyItem;
  tags: Tag;
  'custom-fields': CustomField;
}
interface InputRecord {
  correspondents: components['schemas']['TaxonomyItemInput'];
  'document-types': components['schemas']['TaxonomyItemInput'];
  persons: components['schemas']['TaxonomyItemInput'];
  tags: components['schemas']['TagInput'];
  'custom-fields': components['schemas']['CustomFieldInput'];
}
interface PatchRecord {
  correspondents: components['schemas']['TaxonomyItemPatch'];
  'document-types': components['schemas']['TaxonomyItemPatch'];
  persons: components['schemas']['TaxonomyItemPatch'];
  tags: components['schemas']['TagPatch'];
  'custom-fields': components['schemas']['CustomFieldPatch'];
}

export const KIND_LABEL: Record<TaxonomyKind, string> = { correspondents: t('往来方'), tags: t('标签'), 'document-types': t('类型'), 'custom-fields': t('自定义字段'), persons: t('家人') };

export const ALGORITHMS: { value: MatchAlgorithm; label: string; help: string }[] = [
  { value: 'auto', label: t('自动学习'), help: t('根据已经整理好的文档自动学习归类') },
  { value: 'any', label: t('任意词'), help: t('文档里出现任意一个关键词就归入（关键词用空格或逗号分隔，引号括起的算一个）') },
  { value: 'all', label: t('全部词'), help: t('所有关键词都出现才归入（关键词用空格或逗号分隔，引号括起的算一个）') },
  { value: 'literal', label: t('精确匹配'), help: t('出现完整的这段文字才归入') },
  { value: 'regex', label: t('正则表达式'), help: t('按正则表达式匹配') },
  { value: 'fuzzy', label: t('模糊匹配'), help: t('与这段文字相似（相似度 90% 以上）就归入') },
  { value: 'none', label: t('不自动匹配'), help: t('不自动归入，只能手动选择') },
];
export const algorithmLabel = (a: MatchAlgorithm) => ALGORITHMS.find((x) => x.value === a)?.label ?? a;

export const FIELD_TYPES: { value: CustomFieldType; label: string }[] = [
  { value: 'text', label: t('文本') }, { value: 'integer', label: t('整数') }, { value: 'float', label: t('小数') },
  { value: 'boolean', label: t('是 / 否') }, { value: 'date', label: t('日期') }, { value: 'url', label: t('链接') },
  { value: 'monetary', label: t('金额') }, { value: 'select', label: t('单选') }, { value: 'document_link', label: t('文档链接') },
];
export const fieldTypeLabel = (ft: CustomFieldType) => FIELD_TYPES.find((x) => x.value === ft)?.label ?? ft;

export const CURRENCIES: [string, string][] = [['CNY', t('人民币')], ['USD', t('美元')], ['EUR', t('欧元')], ['HKD', t('港币')], ['JPY', t('日元')], ['GBP', t('英镑')]];
export const currencyLabel = (c?: string) => CURRENCIES.find(([k]) => k === c)?.[1] ?? c ?? '';

/** 规范 3.1 的 12 种预设标签颜色。 */
export const TAG_COLORS: [string, string][] = [
  ['#4F6D7A', t('青灰')], ['#3A6EA5', t('湖蓝')], ['#4B4F9C', t('靛')], ['#7A4E8C', t('紫')], ['#A34D6D', t('玫')], ['#A5523A', t('砖')],
  ['#9A6A12', t('赭')], ['#6B7A2E', t('橄榄')], ['#2F6B4F', t('松绿')], ['#2C7A7B', t('青')], ['#555B63', t('石墨')], ['#8A6F4E', t('驼')],
];

export const taxonomyKey = (kind: TaxonomyKind) => ['taxonomy', kind] as const;

// openapi-fetch 按字面路径推导类型；四类接口的形状由上面的 *Record 保证，这里按其中一条路径调用即可。
const listPath = (kind: TaxonomyKind) => `/api/v1/${kind}` as '/api/v1/tags';
const itemPath = (kind: TaxonomyKind) => `/api/v1/${kind}/{id}` as '/api/v1/tags/{id}';

export function useTaxonomy<K extends TaxonomyKind>(kind: K) {
  return useQuery<KindRecord[K][], ApiError>({
    queryKey: taxonomyKey(kind),
    queryFn: async () => unwrap(await api.GET(listPath(kind))) as unknown as KindRecord[K][],
  });
}

export function useCreateTaxonomy<K extends TaxonomyKind>(kind: K) {
  const qc = useQueryClient();
  return useMutation<KindRecord[K], ApiError, InputRecord[K]>({
    mutationFn: async (body) => unwrap(await api.POST(listPath(kind), { body: body as never })) as unknown as KindRecord[K],
    onSuccess: () => qc.invalidateQueries({ queryKey: taxonomyKey(kind) }),
  });
}

export function useUpdateTaxonomy<K extends TaxonomyKind>(kind: K) {
  const qc = useQueryClient();
  return useMutation<KindRecord[K], ApiError, { id: number; patch: PatchRecord[K] }>({
    mutationFn: async ({ id, patch }) =>
      unwrap(await api.PATCH(itemPath(kind), { params: { path: { id } }, body: patch as never })) as unknown as KindRecord[K],
    onSuccess: () => qc.invalidateQueries({ queryKey: taxonomyKey(kind) }),
  });
}

export function useDeleteTaxonomy(kind: TaxonomyKind) {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE(itemPath(kind), { params: { path: { id } } })); },
    onSuccess: () => qc.invalidateQueries({ queryKey: taxonomyKey(kind) }),
  });
}

/** 标签的完整路径，例如“财务 / 发票”。 */
export function tagPath(tag: Tag, byId: Map<number, Tag>): string {
  const parts = [tag.name];
  let p = tag.parentId ? byId.get(tag.parentId) : undefined;
  for (let guard = 0; p && guard < 10; guard++) {
    parts.unshift(p.name);
    p = p.parentId ? byId.get(p.parentId) : undefined;
  }
  return parts.join(' / ');
}

/** 标签自己和它的全部后代的 ID（移动标签时排除这些，避免成环）。 */
export function tagSubtree(id: number, tags: Tag[]): Set<number> {
  const out = new Set([id]);
  for (let grew = true; grew;) {
    grew = false;
    for (const t of tags) if (t.parentId && out.has(t.parentId) && !out.has(t.id)) { out.add(t.id); grew = true; }
  }
  return out;
}

/** 可以批量整理的几类（v1.5.8，家人 v1.6.3）；自定义字段不在内。 */
export type DirectoryKind = 'correspondents' | 'document-types' | 'tags' | 'persons';

/** 批量删除往来方、类型或标签；文档不删。 */
export function useBulkDeleteTaxonomy(kind: DirectoryKind) {
  const qc = useQueryClient();
  return useMutation<{ count: number }, ApiError, number[]>({
    mutationFn: async (ids) => unwrap(await api.POST('/api/v1/directory/{kind}/delete', { params: { path: { kind } }, body: { ids } })),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ['taxonomy'] }); void qc.invalidateQueries({ queryKey: ['documents'] }); },
  });
}

/** 把几个合并到一个：它们的文档改为保留的那个。 */
export function useMergeTaxonomy(kind: DirectoryKind) {
  const qc = useQueryClient();
  return useMutation<{ documents: number }, ApiError, { keep: number; merge: number[] }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/directory/{kind}/merge', { params: { path: { kind } }, body })),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ['taxonomy'] }); void qc.invalidateQueries({ queryKey: ['documents'] }); void qc.invalidateQueries({ queryKey: ['drawers'] }); },
  });
}

/** 类型的保留期限（v1.6.5，月）；0 为永久保留。 */
export const RETENTIONS: [number, string][] = [[0, t('永久保留')], ...[6, 12, 24, 36, 60, 84, 120].map((m): [number, string] => [m, m % 12 === 0 ? t('{n} 年', { n: m / 12 }) : t('{n} 个月', { n: m })])];
export const retentionLabel = (m?: number) => (!m ? t('永久保留') : RETENTIONS.find(([x]) => x === m)?.[1] ?? (m % 12 === 0 ? t('{n} 年', { n: m / 12 }) : t('{n} 个月', { n: m })));

// 列表的筛选状态与筛选条件 JSON（设计文档 11.9）之间的转换。
// 筛选条：组内为“或”，组间为“且”；标签包含子标签；年份按文档日期；搜索框为文字条件。
import type { DocumentSort } from './documents';
import type { CustomField } from './taxonomy';
import { condToFieldCond, type FieldCond } from '../utils/field-conds';
import { t } from '../i18n';

/** 抽屉、类型、往来方里的 0 表示“未入抽屉 / 没有类型 / 没有往来方”。 */
export interface ListFilters {
  drawer: number[]; type: number[]; correspondent: number[]; tag: number[]; year: string[]; date?: DateFilter;
  // v1.6.1
  /** 标签：含任一个（默认）、全部含有、不含 */
  tagMode?: 'all' | 'none';
  /** 没有任何标签（和选标签二选一） */
  tagNone?: boolean;
  /** 类型、往来方选“不是” */
  typeNot?: boolean; correspondentNot?: boolean;
  /** 自定义字段条件（彼此为“且”）；cond 是添加时算好的筛选条件 */
  fields?: { fc: FieldCond; cond: Cond }[];
  /** 文件格式大类 */
  kinds?: FileKind[];
  /** 原件编号：有、没有，或范围 */
  asn?: AsnFilter;
  // v1.6.3
  /** 家人（0 表示没有家人）；personNot 为“不是” */
  person?: number[]; personNot?: boolean;
  /** 原件的存放位置（0 表示没有记录位置） */
  location?: number[];
}

export type FileKind = 'pdf' | 'image' | 'office' | 'email' | 'ebook' | 'text';
export const FILE_KINDS: [FileKind, string][] = [['pdf', 'PDF'], ['image', t('图片')], ['office', 'Office'], ['email', t('邮件')], ['ebook', t('电子书')], ['text', t('文本')]];
export interface AsnFilter { has?: boolean; none?: boolean; from?: number; to?: number }

export function describeAsn(a: AsnFilter): string {
  if (a.has) return t('原件编号：有');
  if (a.none) return t('原件编号：没有');
  if (a.from && a.to) return t('原件编号：{from}–{to}', { from: a.from, to: a.to });
  if (a.from) return t('原件编号：{from} 起', { from: a.from });
  return t('原件编号：到 {to}', { to: String(a.to) });
}

function asnCond(a: AsnFilter): Cond | null {
  if (a.has) return { field: 'asn', op: 'not_null' };
  if (a.none) return { field: 'asn', op: 'is_null' };
  if (a.from && a.to) return { field: 'asn', op: 'between', value: [a.from, a.to] };
  if (a.from) return { field: 'asn', op: 'gte', value: a.from };
  if (a.to) return { field: 'asn', op: 'lte', value: a.to };
  return null;
}

function readAsn(c: Cond): AsnFilter | null {
  if (c.field !== 'asn') return null;
  if (c.op === 'not_null') return { has: true };
  if (c.op === 'is_null') return { none: true };
  if (c.op === 'between' && Array.isArray(c.value) && c.value.length === 2) return { from: c.value[0] as number, to: c.value[1] as number };
  if (c.op === 'gte' && typeof c.value === 'number') return { from: c.value };
  if (c.op === 'lte' && typeof c.value === 'number') return { to: c.value };
  return null;
}
export type FilterGroup = 'drawer' | 'type' | 'correspondent' | 'person' | 'location' | 'tag' | 'year';

/** 相对今天的时间段（v1.5.2）：保存的筛选每次打开按当天计算。 */
export type Period = 'this_month' | 'last_month' | 'last_7_days' | 'last_30_days' | 'last_90_days' | 'this_year' | 'last_year';
export const PERIODS: [Period, string][] = [
  ['this_month', t('本月')], ['last_month', t('上个月')], ['last_7_days', t('最近 7 天')], ['last_30_days', t('最近 30 天')],
  ['last_90_days', t('最近 3 个月')], ['this_year', t('今年')], ['last_year', t('去年')],
];
/** 日期筛选：时间段或自定义起止，按文档日期或添加时间；和“按年份”二选一。 */
export interface DateFilter { period?: Period; from?: string; to?: string; added?: boolean }

export function describeDate(d: DateFilter): string {
  const what = d.added ? t('添加时间') : t('日期');
  if (d.period) return t('{what}：{value}', { what, value: PERIODS.find(([k]) => k === d.period)?.[1] ?? d.period });
  if (d.from && d.to) return t('{what}：{from} 至 {to}', { what, from: d.from, to: d.to });
  if (d.from) return t('{what}：{from} 起', { what, from: d.from });
  return t('{what}：{to} 止', { what, to: String(d.to) });
}

function dateCond(d: DateFilter): Cond | null {
  const field = d.added ? 'added' : 'created';
  if (d.period) return { field, op: 'period', value: d.period };
  if (d.from && d.to) return { field, op: 'between', value: [d.from, d.to] };
  if (d.from) return { field, op: 'gte', value: d.from };
  if (d.to) return { field, op: 'lte', value: d.to };
  return null;
}

function readDate(c: Cond): DateFilter | null {
  if (c.field !== 'created' && c.field !== 'added') return null;
  const added = c.field === 'added' ? true : undefined;
  if (c.op === 'period' && PERIODS.some(([k]) => k === c.value)) return { period: c.value as Period, added };
  if (c.op === 'between' && Array.isArray(c.value) && (c.field === 'added' || !readYear(c))) return { from: (c.value as string[])[0], to: (c.value as string[])[1], added };
  if (c.op === 'gte' && typeof c.value === 'string') return { from: c.value, added };
  if (c.op === 'lte' && typeof c.value === 'string') return { to: c.value, added };
  return null;
}

export interface DocListState {
  q: string;
  f: ListFilters;
  /** 认不出的条件（套用的常用筛选里手写的），整体保留，显示为一个胶囊 */
  extra?: Cond;
  /** extra 的胶囊上显示的名称（例如抽屉要点的名称）；没有时显示“自定义条件” */
  extraLabel?: string;
  view: 'list' | 'grid';
  /** 列表中标题之外显示的列；没有时用默认的列 */
  columns?: ColumnKey[];
  sort: DocumentSort;
  dir: 'asc' | 'desc';
  /** 输入关键词时自动改为按相关度排序，这里记下原来的排序，清空关键词后恢复 */
  sortBeforeSearch?: { sort: DocumentSort; dir: 'asc' | 'desc' };
  sel: number[];
}

export type Cond = Record<string, unknown>;

/** field:字段ID 为自定义字段的列（v1.5.6） */
export type ColumnKey = 'correspondent' | 'person' | 'location' | 'type' | 'drawer' | 'date' | 'tags' | 'asn' | 'added' | 'amount' | 'dueDate' | 'notes' | 'pages' | `field:${number}`;

/** 自定义字段的列或排序对应的字段 ID；不是时为 null。 */
export const fieldIdOf = (k: string): number | null => (/^field:\d+$/.test(k) ? Number(k.slice(6)) : null);

export const emptyListFilters = (): ListFilters => ({ drawer: [], type: [], correspondent: [], tag: [], year: [] });
export const defaultDocList = (): DocListState => ({ q: '', f: emptyListFilters(), view: 'list', sort: 'created', dir: 'desc', sel: [] });

function classification(field: 'drawer' | 'type' | 'correspondent' | 'person' | 'location', vals: number[], not = false): Cond | null {
  const ids = vals.filter((v) => v > 0);
  const none = vals.includes(0);
  if (not) {
    // “不是 X”包含没有这一项的文档；“不是‘没有类型’”即有类型
    const parts: Cond[] = [];
    if (ids.length) parts.push({ field, op: 'not_in', value: ids });
    if (none) parts.push({ field, op: 'not_null' });
    return parts.length === 0 ? null : parts.length === 1 ? parts[0] : { all: parts };
  }
  const inIds = { field, op: 'in', value: ids };
  const isNull = { field, op: 'is_null' };
  if (ids.length && none) return { any: [inIds, isNull] };
  if (ids.length) return inIds;
  if (none) return isNull;
  return null;
}

const yearCond = (y: string): Cond => ({ field: 'created', op: 'between', value: [`${y}-01-01`, `${y}-12-31`] });

/** 列表状态 → 条件列表（不含基础条件）。 */
export function conditions(state: Pick<DocListState, 'q' | 'f' | 'extra'>): Cond[] {
  const out: Cond[] = [];
  const d = classification('drawer', state.f.drawer ?? []);
  if (d) out.push(d);
  const t = classification('type', state.f.type, state.f.typeNot);
  if (t) out.push(t);
  const c = classification('correspondent', state.f.correspondent, state.f.correspondentNot);
  if (c) out.push(c);
  const p = classification('person', state.f.person ?? [], state.f.personNot);
  if (p) out.push(p);
  const l = classification('location', state.f.location ?? []);
  if (l) out.push(l);
  if (state.f.tagNone) out.push({ field: 'tag', op: 'is_null' });
  else if (state.f.tag.length) out.push({ field: 'tag', op: state.f.tagMode === 'all' ? 'has_all' : state.f.tagMode === 'none' ? 'has_none' : 'has_any', value: state.f.tag, descendants: true });
  for (const x of state.f.fields ?? []) out.push(x.cond);
  if (state.f.kinds?.length) out.push({ field: 'mime', op: 'kind', value: state.f.kinds });
  const ac = state.f.asn && asnCond(state.f.asn);
  if (ac) out.push(ac);
  if (state.f.year.length === 1) out.push(yearCond(state.f.year[0]));
  else if (state.f.year.length > 1) out.push({ any: state.f.year.map(yearCond) });
  const dc = state.f.date && dateCond(state.f.date);
  if (dc) out.push(dc);
  if (state.extra) out.push(state.extra);
  if (state.q.trim()) out.push({ text: state.q.trim() });
  return out;
}

/** 组合成筛选条件 JSON；base 是页面固定的条件（如条目页的“往来方是 X”）。没有条件时为 ''。 */
export function toFilter(state: Pick<DocListState, 'q' | 'f' | 'extra'>, base: Cond[] = []): string {
  const all = [...base, ...conditions(state)];
  if (!all.length) return '';
  return JSON.stringify(all.length === 1 ? all[0] : { all });
}

/** 不含搜索词的筛选条件（保存筛选、批量操作用整个条件，分面计数和月份也用它）。 */
export const hasConditions = (s: Pick<DocListState, 'q' | 'f' | 'extra'>) => conditions(s).length > 0;

// ---------- 反过来：套用常用筛选 ----------

const isNum = (v: unknown): v is number => typeof v === 'number';

function readClassification(c: Cond, field: string): number[] | null {
  if (c.field === field && c.op === 'in' && Array.isArray(c.value) && c.value.every(isNum)) return c.value as number[];
  if (c.field === field && c.op === 'is_null') return [0];
  if (Array.isArray(c.any) && c.any.length === 2) {
    const a = readClassification(c.any[0] as Cond, field);
    const b = readClassification(c.any[1] as Cond, field);
    if (a && b && b.length === 1 && b[0] === 0 && !a.includes(0)) return [...a, 0];
  }
  return null;
}

/** “不是”：not_in、not_null，或两者都有。 */
function readNot(c: Cond, field: string): number[] | null {
  if (c.field === field && c.op === 'not_in' && Array.isArray(c.value) && c.value.every(isNum)) return c.value as number[];
  if (c.field === field && c.op === 'not_null') return [0];
  if (Array.isArray(c.all) && c.all.length === 2) {
    const a = readNot(c.all[0] as Cond, field), b = readNot(c.all[1] as Cond, field);
    if (a && b && b.length === 1 && b[0] === 0 && !a.includes(0)) return [...a, 0];
  }
  return null;
}

function readYear(c: Cond): string | null {
  if (c.field !== 'created' || c.op !== 'between' || !Array.isArray(c.value)) return null;
  const [from, to] = c.value as string[];
  const y = from?.slice(0, 4);
  return from === `${y}-01-01` && to === `${y}-12-31` ? y : null;
}

/** 筛选条件 → 列表状态；认不出的部分放进 extra。typeOf 给出自定义字段的类型（日期的早于 / 晚于）。 */
export function fromFilter(filter: Cond | undefined, typeOf: (id: number) => CustomField['dataType'] | undefined = () => undefined): Pick<DocListState, 'q' | 'f' | 'extra'> {
  const out: Pick<DocListState, 'q' | 'f' | 'extra'> = { q: '', f: emptyListFilters() };
  if (!filter || !Object.keys(filter).length) return out;
  const parts = Array.isArray(filter.all) ? (filter.all as Cond[]) : [filter];
  const unknown: Cond[] = [];
  for (const c of parts) {
    const drawer = readClassification(c, 'drawer');
    const type = readClassification(c, 'type');
    const corr = readClassification(c, 'correspondent');
    const year = readYear(c);
    const years = Array.isArray(c.any) ? (c.any as Cond[]).map(readYear) : null;
    const date = readDate(c);
    const typeNot = readNot(c, 'type');
    const corrNot = readNot(c, 'correspondent');
    const person = readClassification(c, 'person');
    const personNot = readNot(c, 'person');
    const location = readClassification(c, 'location');
    const asn = readAsn(c);
    const fc = condToFieldCond(c, typeOf);
    if (drawer && !out.f.drawer.length) out.f.drawer = drawer;
    else if (type && !out.f.type.length) out.f.type = type;
    else if (corr && !out.f.correspondent.length) out.f.correspondent = corr;
    // “不是”的 not_in 和 not_null 可能分成两条读到（只有这一个条件时外层的 all 被拆开）：合在一起
    else if (typeNot && (!out.f.type.length || out.f.typeNot)) { out.f.type = [...new Set([...out.f.type, ...typeNot])]; out.f.typeNot = true; }
    else if (corrNot && (!out.f.correspondent.length || out.f.correspondentNot)) { out.f.correspondent = [...new Set([...out.f.correspondent, ...corrNot])]; out.f.correspondentNot = true; }
    else if (person && !out.f.person?.length) out.f.person = person;
    else if (personNot && (!out.f.person?.length || out.f.personNot)) { out.f.person = [...new Set([...(out.f.person ?? []), ...personNot])]; out.f.personNot = true; }
    else if (location && !out.f.location?.length) out.f.location = location;
    else if (c.field === 'tag' && ['has_any', 'has_all', 'has_none'].includes(c.op as string) && c.descendants === true && Array.isArray(c.value) && !out.f.tag.length && !out.f.tagNone) {
      out.f.tag = c.value as number[];
      if (c.op !== 'has_any') out.f.tagMode = c.op === 'has_all' ? 'all' : 'none';
    } else if (c.field === 'tag' && c.op === 'is_null' && !out.f.tag.length) out.f.tagNone = true;
    else if (c.field === 'mime' && c.op === 'kind' && Array.isArray(c.value) && !out.f.kinds) out.f.kinds = c.value as FileKind[];
    else if (asn && !out.f.asn) out.f.asn = asn;
    else if (fc) out.f.fields = [...(out.f.fields ?? []), { fc, cond: c }];
    else if (year && !out.f.year.length && !out.f.date) out.f.year = [year];
    else if (date && !out.f.date && !out.f.year.length) out.f.date = date;
    else if (years && years.length && years.every((y): y is string => !!y) && !out.f.year.length) out.f.year = years;
    else if (typeof c.text === 'string' && !out.q) out.q = c.text;
    else unknown.push(c);
  }
  if (unknown.length) out.extra = unknown.length === 1 ? unknown[0] : { all: unknown };
  return out;
}

/** 常用筛选里的排序 "created:desc"。 */
export function parseSort(s: string): Pick<DocListState, 'sort' | 'dir'> | null {
  const i = s.lastIndexOf(':');
  const sort = s.slice(0, i), dir = s.slice(i + 1); // field:12:asc
  const known = ['created', 'added', 'modified', 'title', 'correspondent', 'amount', 'due', 'asn', 'relevance'].includes(sort) || fieldIdOf(sort) !== null;
  if (i < 0 || !known || !['asc', 'desc'].includes(dir)) return null;
  return { sort: sort as DocumentSort, dir: dir as 'asc' | 'desc' };
}

/** 筛选条件里的搜索词（v1.5.4）：从搜索结果打开文档时，预览里自动查找这些词。没有时返回 ''。 */
export function filterText(filter: string): string {
  if (!filter) return '';
  let c: unknown;
  try { c = JSON.parse(filter); } catch { return ''; }
  const walk = (x: unknown): string => {
    if (!x || typeof x !== 'object') return '';
    const o = x as Record<string, unknown>;
    if (typeof o.text === 'string') return o.text.trim();
    for (const k of ['all', 'any']) {
      if (Array.isArray(o[k])) for (const y of o[k] as unknown[]) { const t = walk(y); if (t) return t; }
    }
    return '';
  };
  return walk(c);
}

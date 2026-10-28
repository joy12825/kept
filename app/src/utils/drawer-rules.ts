// 收纳规则编辑器（设计文档 3.6）：原型的平铺条件（当 / 且 / 或，字段，属于 / 不属于 / 包含 / 不包含，值）
// 与筛选条件 JSON 之间的转换，以及把规则写成一句话。“且”比“或”结合得紧：any 里套 all。
import type { Cond } from '../api/filters';
import type { CustomField } from '../api/taxonomy';
import { condToFieldCond, describeFieldCond, type FieldCond } from './field-conds';
import { t } from '../i18n';

export type RuleField = 'type' | 'correspondent' | 'person' | 'tag' | 'text' | `cf:${number}`;
export type RuleOp = 'in' | 'not_in';

/** 一行条件。join 是与上一行的连接（第一行忽略）；关键词的 values 是词，其余是 ID（单选字段是选项 ID）。 */
export interface RuleRow {
  join: 'and' | 'or'; field: RuleField; op: RuleOp; values: (number | string)[];
  /** 自定义字段的条件（v1.6.1，所有字段类型）；cond 是编辑时按字段算好的筛选条件，值没填好时为 null */
  fc?: FieldCond; cond?: Cond | null;
}

/** 关键词输入框里的分隔：顿号、逗号、空格。 */
export const splitWords = (s: string) => s.split(/[、，,;；\s]+/).map((w) => w.trim()).filter(Boolean);

function rowCond(r: RuleRow): Cond | null {
  if (r.fc) return r.cond ?? null;
  if (!r.values.length) return null;
  if (r.field === 'text') {
    const words = r.values.map(String);
    const any: Cond = words.length === 1 ? { text: words[0] } : { any: words.map((w) => ({ text: w })) };
    return r.op === 'in' ? any : { not: any };
  }
  if (r.field === 'tag') return { field: 'tag', op: r.op === 'in' ? 'has_any' : 'has_none', value: r.values, descendants: true };
  if (r.field.startsWith('cf:')) {
    const c = { field: r.field, op: 'in', value: r.values };
    return r.op === 'in' ? c : { not: c };
  }
  return { field: r.field, op: r.op, value: r.values };
}

/** 平铺条件 → 筛选条件 JSON；没有有效条件时为 null（不自动收纳）。 */
export function rowsToRule(rows: RuleRow[]): Cond | null {
  const groups: Cond[][] = [];
  rows.forEach((r, i) => {
    const c = rowCond(r);
    if (!c) return;
    if (i === 0 || r.join === 'or' || !groups.length) groups.push([c]);
    else groups[groups.length - 1].push(c);
  });
  const parts = groups.map((g) => (g.length === 1 ? g[0] : { all: g }));
  if (!parts.length) return null;
  return parts.length === 1 ? parts[0] : { any: parts };
}

function textWords(c: Cond): string[] | null {
  if (typeof c.text === 'string') return [c.text];
  if (Array.isArray(c.any) && c.any.length && (c.any as Cond[]).every((x) => typeof x.text === 'string')) return (c.any as Cond[]).map((x) => x.text as string);
  return null;
}

const ids = (v: unknown): number[] | null => (Array.isArray(v) && v.every((x) => typeof x === 'number') ? (v as number[]) : null);

type TypeOf = (id: number) => CustomField['dataType'] | undefined;

function condRow(c: Cond, typeOf: TypeOf): Omit<RuleRow, 'join'> | null {
  // 自定义字段（v1.6.1）：所有类型都认，包括“不是”
  const fc = condToFieldCond(c, typeOf);
  if (fc) {
    const list = fc.op === 'in' || fc.op === 'not_in';
    return { field: `cf:${fc.fieldId}`, op: fc.op === 'not_in' ? 'not_in' : 'in', values: list ? (fc.value as string[]) : [], fc, cond: c };
  }
  const words = textWords(c);
  if (words) return { field: 'text', op: 'in', values: words };
  if (c.not && typeof c.not === 'object') {
    const inner = condRow(c.not as Cond, typeOf);
    if (inner && inner.op === 'in' && (inner.field === 'text' || inner.field.startsWith('cf:'))) return { ...inner, op: 'not_in' };
    return null;
  }
  if ((c.field === 'type' || c.field === 'correspondent' || c.field === 'person') && (c.op === 'in' || c.op === 'not_in') && ids(c.value)) {
    return { field: c.field, op: c.op, values: ids(c.value)! };
  }
  if (c.field === 'tag' && (c.op === 'has_any' || c.op === 'has_none') && ids(c.value)) {
    return { field: 'tag', op: c.op === 'has_any' ? 'in' : 'not_in', values: ids(c.value)! };
  }
  if (typeof c.field === 'string' && c.field.startsWith('cf:') && c.op === 'in' && Array.isArray(c.value)) {
    return { field: c.field as RuleField, op: 'in', values: c.value as string[] };
  }
  return null;
}

/** 筛选条件 JSON → 平铺条件；编辑器表示不了的形状返回 null。 */
export function ruleToRows(rule: Cond | null | undefined, typeOf: TypeOf = () => undefined): RuleRow[] | null {
  if (!rule) return [];
  const groups = Array.isArray(rule.any) && !textWords(rule) ? (rule.any as Cond[]) : [rule];
  const rows: RuleRow[] = [];
  for (const g of groups) {
    const parts = Array.isArray(g.all) ? (g.all as Cond[]) : [g];
    for (let i = 0; i < parts.length; i++) {
      const r = condRow(parts[i], typeOf);
      if (!r) return null;
      rows.push({ ...r, join: i === 0 ? 'or' : 'and' });
    }
  }
  if (rows.length) rows[0].join = 'and';
  return rows;
}

export interface Names {
  type: (id: number) => string | undefined;
  correspondent: (id: number) => string | undefined;
  /** 家人（v1.6.3） */
  person: (id: number) => string | undefined;
  tag: (id: number) => string | undefined;
  field: (id: number) => { name: string; option: (id: string) => string | undefined } | undefined;
  /** 自定义字段本身（v1.6.1，写出“保修截止今天起 90 天内”这样的句子）；没有时按旧的写法 */
  customField?: (id: number) => CustomField | undefined;
}

export const FIELD_LABEL: Record<'type' | 'correspondent' | 'person' | 'tag' | 'text', string> = { type: t('类型'), correspondent: t('往来方'), person: t('家人'), tag: t('标签'), text: t('关键词') };

/** 一行写成一句：“类型为账单、合同”“关键词包含“车险”或“保单””。 */
export function describeRow(r: RuleRow, n: Names): string {
  if (r.field === 'text') {
    const words = r.values.map((w) => `“${w}”`).join(t('、'));
    return r.op === 'in' ? t('关键词包含{words}', { words }) : t('关键词不含{words}', { words });
  }
  let label: string;
  let vals: string[];
  if (r.fc && n.customField) return describeFieldCond(r.fc, n.customField(r.fc.fieldId));
  if (r.field.startsWith('cf:')) {
    const f = n.field(Number(r.field.slice(3)));
    label = f?.name ?? t('已删除的字段');
    vals = r.values.map((v) => f?.option(String(v)) ?? t('已删除的选项'));
  } else {
    const k = r.field as 'type' | 'correspondent' | 'person' | 'tag';
    label = FIELD_LABEL[k];
    vals = r.values.map((v) => n[k](Number(v)) ?? t('已删除的{name}', { name: label.toLowerCase() }));
  }
  const values = vals.join(t('、'));
  return r.op === 'in' ? t('{label}为{values}', { label, values }) : t('{label}不是{values}', { label, values });
}

/** 整条规则写成一句；没有规则时说明不会自动收纳，写不出来的手写规则说“自定义条件”。 */
export function describeRule(rule: Cond | null | undefined, n: Names): string {
  if (!rule) return t('没有收纳规则，只收手动放入的文档');
  const rows = ruleToRows(rule, (id) => n.customField?.(id)?.dataType);
  if (!rows) return t('自定义条件');
  let out = '';
  rows.forEach((r, i) => { out += (i === 0 ? '' : r.join === 'and' ? t('，且') : t('；或')) + describeRow(r, n); });
  return out;
}

// 自定义字段的条件（v1.6.1，参照 paperless-ngx 的字段查询）：文件列表的“字段”筛选和条件编辑器（收纳规则、工作流）共用。
// 按字段类型给出能用的运算，和筛选条件 JSON（设计文档 11.9）互相转换，并写成一句话。
import type { Cond } from '../api/filters';
import type { CustomField } from '../api/taxonomy';
import { t } from '../i18n';

export type FieldOp =
  | 'has' | 'empty' // 有值 / 没有值
  | 'contains' | 'eq' // 文本：包含 / 等于；数值、日期：等于
  | 'gt' | 'lt' | 'between' // 数值：大于 / 小于 / 介于
  | 'before' | 'after' | 'next_days' | 'last_days' // 日期
  | 'is_true' | 'is_false' // 是否
  | 'in' | 'not_in'; // 单选

/** value：文本、日期、金额是字符串；整数、小数、天数是数字；介于是两个值；单选是选项 ID 数组。 */
export interface FieldCond { fieldId: number; op: FieldOp; value?: unknown }

const OPS: Record<CustomField['dataType'], [FieldOp, string][]> = {
  text: [['contains', t('包含')], ['eq', t('等于')], ['has', t('有值')], ['empty', t('没有值')]],
  url: [['contains', t('包含')], ['has', t('有值')], ['empty', t('没有值')]],
  integer: [['eq', t('等于')], ['gt', t('大于')], ['lt', t('小于')], ['between', t('介于')], ['has', t('有值')], ['empty', t('没有值')]],
  float: [['eq', t('等于')], ['gt', t('大于')], ['lt', t('小于')], ['between', t('介于')], ['has', t('有值')], ['empty', t('没有值')]],
  monetary: [['eq', t('等于')], ['gt', t('大于')], ['lt', t('小于')], ['between', t('介于')], ['has', t('有值')], ['empty', t('没有值')]],
  date: [['next_days', t('今天起几天内')], ['last_days', t('最近几天')], ['before', t('早于')], ['after', t('晚于')], ['between', t('介于')], ['eq', t('是这一天')], ['has', t('有值')], ['empty', t('没有值')]],
  boolean: [['is_true', t('是')], ['is_false', t('否')], ['empty', t('没有填')]],
  select: [['in', t('是其中之一')], ['not_in', t('不是')], ['has', t('有值')], ['empty', t('没有值')]],
  document_link: [['has', t('有值')], ['empty', t('没有值')]],
};

/** 这个类型的字段能用的运算（第一个是默认的）。 */
export const fieldOps = (f: CustomField): [FieldOp, string][] => OPS[f.dataType];

/** 运算是否需要填值；介于要两个值。 */
export const needsValue = (op: FieldOp) => !['has', 'empty', 'is_true', 'is_false'].includes(op);

/** 金额写成两位小数（服务端按两位小数的字符串比较）。 */
function money(v: unknown): string | null {
  const s = String(v ?? '').trim();
  if (!/^-?\d+(\.\d{1,2})?$/.test(s)) return null;
  const [w, f = ''] = s.split('.');
  return `${w}.${(f + '00').slice(0, 2)}`;
}

function one(f: CustomField, v: unknown): unknown {
  switch (f.dataType) {
    case 'integer': { const n = Number(v); return v !== '' && Number.isInteger(n) ? n : null; }
    case 'float': { const n = Number(v); return v !== '' && Number.isFinite(n) ? n : null; }
    case 'monetary': return money(v);
    case 'date': return typeof v === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(v) ? v : null;
    default: { const s = String(v ?? '').trim(); return s || null; }
  }
}

/** 条件 → 筛选条件 JSON；值没填好时为 null。 */
export function fieldCondToCond(fc: FieldCond, f: CustomField | undefined): Cond | null {
  if (!f) return null;
  const field = `cf:${fc.fieldId}`;
  switch (fc.op) {
    case 'has': return { field, op: 'not_null' };
    case 'empty': return { field, op: 'is_null' };
    case 'is_true': return { field, op: 'eq', value: true };
    case 'is_false': return { field, op: 'eq', value: false };
    case 'in': case 'not_in': {
      const ids = Array.isArray(fc.value) ? (fc.value as string[]) : [];
      if (!ids.length) return null;
      const c = { field, op: 'in', value: ids };
      return fc.op === 'in' ? c : { not: c };
    }
    case 'next_days': case 'last_days': {
      const n = Number(fc.value);
      if (!Number.isInteger(n) || n < 1 || n > 36500) return null;
      return { field, op: 'within_days', value: fc.op === 'next_days' ? n : -n };
    }
    case 'between': {
      const [a, b] = Array.isArray(fc.value) ? fc.value : [];
      const lo = one(f, a), hi = one(f, b);
      return lo === null || hi === null ? null : { field, op: 'between', value: [lo, hi] };
    }
    default: {
      const v = one(f, fc.value);
      if (v === null) return null;
      const op = fc.op === 'before' ? 'lt' : fc.op === 'after' ? 'gt' : fc.op;
      return { field, op, value: v };
    }
  }
}

/** 筛选条件 → 条件；不是单个自定义字段条件的返回 null。types 给出字段的类型（日期的 gt / lt 读成晚于 / 早于）。 */
export function condToFieldCond(c: Cond, typeOf: (id: number) => CustomField['dataType'] | undefined): FieldCond | null {
  if (c.not && typeof c.not === 'object') {
    const inner = condToFieldCond(c.not as Cond, typeOf);
    return inner && inner.op === 'in' ? { ...inner, op: 'not_in' } : null;
  }
  const m = typeof c.field === 'string' ? /^cf:(\d+)$/.exec(c.field) : null;
  if (!m) return null;
  const fieldId = Number(m[1]);
  const type = typeOf(fieldId);
  switch (c.op) {
    case 'not_null': return { fieldId, op: 'has' };
    case 'is_null': return { fieldId, op: 'empty' };
    case 'in': return Array.isArray(c.value) ? { fieldId, op: 'in', value: c.value } : null;
    case 'within_days': return typeof c.value === 'number' ? { fieldId, op: c.value > 0 ? 'next_days' : 'last_days', value: Math.abs(c.value) } : null;
    case 'between': return Array.isArray(c.value) && c.value.length === 2 ? { fieldId, op: 'between', value: c.value } : null;
    case 'eq':
      if (typeof c.value === 'boolean') return { fieldId, op: c.value ? 'is_true' : 'is_false' };
      return { fieldId, op: 'eq', value: c.value };
    case 'contains': return { fieldId, op: 'contains', value: c.value };
    case 'gt': case 'lt':
      if (type === 'date') return { fieldId, op: c.op === 'gt' ? 'after' : 'before', value: c.value };
      return { fieldId, op: c.op, value: c.value };
  }
  return null;
}

/** 读条件时还不知道字段类型（字段列表没载入）：日期的大于、小于改成晚于、早于。 */
export function fixOps(fc: FieldCond, f: CustomField | undefined): FieldCond {
  if (f?.dataType === 'date' && (fc.op === 'gt' || fc.op === 'lt')) return { ...fc, op: fc.op === 'gt' ? 'after' : 'before' };
  return fc;
}

/** 写成一句：“保修截止今天起 90 天内”“报销状态不是已报销”“金额大于 1000.00”。 */
export function describeFieldCond(raw: FieldCond, f: CustomField | undefined): string {
  if (!f) return t('已删除的字段');
  const fc = fixOps(raw, f);
  const label = fieldOps(f).find(([k]) => k === fc.op)?.[1] ?? fc.op;
  const opt = (id: unknown) => f.extra.options?.find((o) => o.id === id)?.label ?? t('已删除的选项');
  switch (fc.op) {
    case 'has': return t('{name}有值', { name: f.name });
    case 'empty': return f.dataType === 'boolean' ? t('{name}没有填', { name: f.name }) : t('{name}没有值', { name: f.name });
    case 'is_true': return t('{name}：是', { name: f.name });
    case 'is_false': return t('{name}：否', { name: f.name });
    case 'in': return t('{name}是{values}', { name: f.name, values: (fc.value as string[] ?? []).map(opt).join(t('、')) });
    case 'not_in': return t('{name}不是{values}', { name: f.name, values: (fc.value as string[] ?? []).map(opt).join(t('、')) });
    case 'next_days': return t('{name}今天起 {n} 天内', { name: f.name, n: String(fc.value) });
    case 'last_days': return t('{name}最近 {n} 天', { name: f.name, n: String(fc.value) });
    case 'between': { const [a, b] = (fc.value as unknown[]) ?? []; return t('{name}介于 {a} 和 {b}', { name: f.name, a: String(a), b: String(b) }); }
    case 'contains': return t('{name}包含“{value}”', { name: f.name, value: String(fc.value) });
    case 'eq': return f.dataType === 'date' ? t('{name}是 {value}', { name: f.name, value: String(fc.value) }) : t('{name}等于 {value}', { name: f.name, value: String(fc.value) });
    default: return t('{name}{op} {value}', { name: f.name, op: label.toLowerCase(), value: String(fc.value) });
  }
}

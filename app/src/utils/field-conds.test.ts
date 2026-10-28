import { describe, expect, it } from 'vitest';
import type { CustomField } from '../api/taxonomy';
import { condToFieldCond, describeFieldCond, fieldCondToCond, fieldOps, type FieldCond } from './field-conds';

const f = (id: number, name: string, dataType: CustomField['dataType'], extra: CustomField['extra'] = {}): CustomField => ({ id, name, initial: 'X', sortKey: 'x', dataType, extra, builtIn: false });
const amount = f(1, '金额', 'monetary', { currency: 'CNY' });
const warranty = f(2, '保修截止', 'date');
const status = f(3, '报销状态', 'select', { options: [{ id: 'p', label: '待报销' }, { id: 'd', label: '已报销' }] });
const plate = f(4, '车牌', 'text');
const paid = f(5, '已付', 'boolean');
const km = f(6, '里程', 'float');
const byId = new Map([amount, warranty, status, plate, paid, km].map((x) => [x.id, x]));
const typeOf = (id: number) => byId.get(id)?.dataType;

describe('自定义字段条件（v1.6.1）', () => {
  it('每种类型有自己的运算', () => {
    expect(fieldOps(warranty).map(([k]) => k)).toContain('next_days');
    expect(fieldOps(status).map(([k]) => k)).toEqual(['in', 'not_in', 'has', 'empty']);
    expect(fieldOps(f(9, '链接', 'document_link')).map(([k]) => k)).toEqual(['has', 'empty']);
  });

  it('转成筛选条件，读回来一样', () => {
    const cases: [FieldCond, object | null][] = [
      [{ fieldId: 1, op: 'gt', value: '1000' }, { field: 'cf:1', op: 'gt', value: '1000.00' }],
      [{ fieldId: 1, op: 'between', value: ['100', '200.5'] }, { field: 'cf:1', op: 'between', value: ['100.00', '200.50'] }],
      [{ fieldId: 2, op: 'next_days', value: 90 }, { field: 'cf:2', op: 'within_days', value: 90 }],
      [{ fieldId: 2, op: 'last_days', value: 30 }, { field: 'cf:2', op: 'within_days', value: -30 }],
      [{ fieldId: 2, op: 'before', value: '2025-01-01' }, { field: 'cf:2', op: 'lt', value: '2025-01-01' }],
      [{ fieldId: 3, op: 'not_in', value: ['d'] }, { not: { field: 'cf:3', op: 'in', value: ['d'] } }],
      [{ fieldId: 4, op: 'contains', value: '京A' }, { field: 'cf:4', op: 'contains', value: '京A' }],
      [{ fieldId: 5, op: 'is_false' }, { field: 'cf:5', op: 'eq', value: false }],
      [{ fieldId: 6, op: 'lt', value: '12.5' }, { field: 'cf:6', op: 'lt', value: 12.5 }],
      [{ fieldId: 4, op: 'empty' }, { field: 'cf:4', op: 'is_null' }],
    ];
    for (const [fc, want] of cases) {
      const c = fieldCondToCond(fc, byId.get(fc.fieldId));
      expect(c).toEqual(want);
      const back = condToFieldCond(c!, typeOf)!;
      expect(fieldCondToCond(back, byId.get(back.fieldId))).toEqual(want);
    }
  });

  it('值没填好时不成条件', () => {
    expect(fieldCondToCond({ fieldId: 1, op: 'gt', value: '1.234' }, amount)).toBeNull();
    expect(fieldCondToCond({ fieldId: 2, op: 'next_days', value: 0 }, warranty)).toBeNull();
    expect(fieldCondToCond({ fieldId: 3, op: 'in', value: [] }, status)).toBeNull();
    expect(fieldCondToCond({ fieldId: 6, op: 'eq', value: '' }, km)).toBeNull();
    expect(fieldCondToCond({ fieldId: 1, op: 'gt', value: '1' }, undefined)).toBeNull();
  });

  it('写成一句话', () => {
    expect(describeFieldCond({ fieldId: 2, op: 'next_days', value: 90 }, warranty)).toBe('保修截止今天起 90 天内');
    expect(describeFieldCond({ fieldId: 3, op: 'not_in', value: ['d'] }, status)).toBe('报销状态不是已报销');
    expect(describeFieldCond({ fieldId: 1, op: 'gt', value: '1000.00' }, amount)).toBe('金额大于 1000.00');
    expect(describeFieldCond({ fieldId: 4, op: 'contains', value: '京A' }, plate)).toBe('车牌包含“京A”');
    expect(describeFieldCond({ fieldId: 9, op: 'has' }, undefined)).toBe('已删除的字段');
  });
});

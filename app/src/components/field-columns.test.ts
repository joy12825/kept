import { describe, expect, it } from 'vitest';
import { fieldText } from './field-columns';
import type { CustomField } from '../api/taxonomy';

const f = (dataType: CustomField['dataType'], extra: CustomField['extra'] = {}): CustomField => ({ id: 1, name: 'x', initial: 'X', sortKey: 'x', dataType, extra, builtIn: false });

describe('列表里的自定义字段值', () => {
  it('按类型显示', () => {
    expect(fieldText(f('boolean'), true)).toBe('是');
    expect(fieldText(f('boolean'), false)).toBe('否');
    expect(fieldText(f('select', { options: [{ id: 'a', label: '已报销' }] }), 'a')).toBe('已报销');
    expect(fieldText(f('monetary', { currency: 'USD' }), '12.00')).toBe('$12.00');
    expect(fieldText(f('monetary', { currency: 'CHF' }), '12.00')).toBe('CHF 12.00');
    expect(fieldText(f('text'), null)).toBe('');
  });

  it('多行文本只显示第一行', () => {
    expect(fieldText(f('text', { multiline: true }), '保修两年\n人为损坏不保')).toBe('保修两年 …');
    expect(fieldText(f('text'), '一行')).toBe('一行');
  });
});

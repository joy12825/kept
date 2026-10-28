import { describe, expect, it } from 'vitest';
import { describeRule, ruleToRows, rowsToRule, splitWords, type Names, type RuleRow } from './drawer-rules';

const names: Names = {
  type: (id) => ({ 1: '账单', 2: '合同' } as Record<number, string>)[id],
  correspondent: (id) => ({ 10: '国家电网', 11: '链家' } as Record<number, string>)[id],
  person: (id) => ({ 20: '乐乐' } as Record<number, string>)[id],
  tag: (id) => ({ 30: '水电' } as Record<number, string>)[id],
  field: (id) => (id === 5 ? { name: '等级', option: (o) => (o === 'a' ? 'A 级' : undefined) } : undefined),
};

describe('收纳规则', () => {
  it('且比或结合得紧', () => {
    const rows: RuleRow[] = [
      { join: 'and', field: 'type', op: 'in', values: [1, 2] },
      { join: 'and', field: 'correspondent', op: 'in', values: [10, 11] },
      { join: 'or', field: 'text', op: 'in', values: ['车险'] },
    ];
    const rule = rowsToRule(rows);
    expect(rule).toEqual({ any: [
      { all: [{ field: 'type', op: 'in', value: [1, 2] }, { field: 'correspondent', op: 'in', value: [10, 11] }] },
      { text: '车险' },
    ] });
    expect(ruleToRows(rule)).toEqual(rows);
    expect(describeRule(rule, names)).toBe('类型为账单、合同，且往来方为国家电网、链家；或关键词包含“车险”');
  });

  it('不包含、标签、单选字段，空值的行跳过', () => {
    const rows: RuleRow[] = [
      { join: 'and', field: 'type', op: 'in', values: [1] },
      { join: 'and', field: 'text', op: 'not_in', values: ['儿童', '疫苗'] },
      { join: 'and', field: 'tag', op: 'not_in', values: [30] },
      { join: 'and', field: 'cf:5', op: 'in', values: ['a'] },
      { join: 'or', field: 'correspondent', op: 'in', values: [] },
    ];
    const rule = rowsToRule(rows);
    expect(rule).toEqual({ all: [
      { field: 'type', op: 'in', value: [1] },
      { not: { any: [{ text: '儿童' }, { text: '疫苗' }] } },
      { field: 'tag', op: 'has_none', value: [30], descendants: true },
      { field: 'cf:5', op: 'in', value: ['a'] },
    ] });
    const back = ruleToRows(rule)!;
    expect(back.map(({ fc: _fc, cond: _cond, ...r }) => r)).toEqual(rows.slice(0, 4));
    expect(back[3].fc).toEqual({ fieldId: 5, op: 'in', value: ['a'] }); // 单选字段读成字段条件（v1.6.1）
    expect(describeRule(rule, names)).toBe('类型为账单，且关键词不含“儿童”、“疫苗”，且标签不是水电，且等级为A 级');
  });

  it('所有类型的自定义字段条件（v1.6.1）', () => {
    const warranty = { id: 7, name: '保修截止', initial: 'B', sortKey: 'b', dataType: 'date' as const, extra: {}, builtIn: false };
    const rule = { all: [{ field: 'type', op: 'in', value: [1] }, { field: 'cf:7', op: 'within_days', value: 90 }, { field: 'cf:7', op: 'gt', value: '2024-01-01' }] };
    const rows = ruleToRows(rule, () => 'date')!;
    expect(rows[1].fc).toEqual({ fieldId: 7, op: 'next_days', value: 90 });
    expect(rows[2].fc).toEqual({ fieldId: 7, op: 'after', value: '2024-01-01' });
    expect(rowsToRule(rows)).toEqual(rule);
    const n = { ...names, customField: (id: number) => (id === 7 ? warranty : undefined) };
    expect(describeRule(rule, n)).toBe('类型为账单，且保修截止今天起 90 天内，且保修截止晚于 2024-01-01');
    // 值没填好的字段行跳过
    expect(rowsToRule([{ join: 'and', field: 'cf:7', op: 'in', values: [], fc: { fieldId: 7, op: 'next_days' }, cond: null }])).toBeNull();
  });

  it('模板的关键词规则、没有规则、编辑器表示不了的形状', () => {
    const tpl = { any: [{ field: 'type', op: 'in', value: [1] }, { any: [{ text: '护照' }, { text: '身份证' }] }] };
    expect(ruleToRows(tpl)).toEqual([
      { join: 'and', field: 'type', op: 'in', values: [1] },
      { join: 'or', field: 'text', op: 'in', values: ['护照', '身份证'] },
    ]);
    expect(rowsToRule([])).toBeNull();
    expect(describeRule(null, names)).toBe('没有收纳规则，只收手动放入的文档');
    expect(ruleToRows({ field: 'created', op: 'gte', value: '2024-01-01' })).toBeNull();
    expect(describeRule({ field: 'created', op: 'gte', value: '2024-01-01' }, names)).toBe('自定义条件');
  });

  it('关键词按顿号、逗号、空格分开', () => {
    expect(splitWords('儿童、幼儿园，疫苗 学费')).toEqual(['儿童', '幼儿园', '疫苗', '学费']);
  });
});

describe('v1.6.3：家人', () => {
  it('规则里的家人读写和说明', () => {
    const rule = { all: [{ field: 'person', op: 'in', value: [20] }, { text: '体检' }] };
    const rows = ruleToRows(rule)!;
    expect(rows.map((r) => r.field)).toEqual(['person', 'text']);
    expect(rowsToRule(rows)).toEqual(rule);
    expect(describeRule(rule, names)).toBe('家人为乐乐，且关键词包含“体检”');
  });
});

import { describe, expect, it } from 'vitest';
import { emptyListFilters, fromFilter, parseSort, toFilter, describeDate, type Cond, filterText, describeAsn } from './filters';

const state = (f: Partial<ReturnType<typeof emptyListFilters>>, q = '') => ({ q, f: { ...emptyListFilters(), ...f } });

describe('筛选状态与筛选条件 JSON', () => {
  it('没有条件时为空串；单个条件不包 all', () => {
    expect(toFilter(state({}))).toBe('');
    expect(JSON.parse(toFilter(state({ type: [3] })))).toEqual({ field: 'type', op: 'in', value: [3] });
  });

  it('组内为“或”，组间为“且”；标签含子标签，年份按日期，搜索为文字条件', () => {
    const f = JSON.parse(toFilter(state({ type: [3, 0], tag: [7], year: ['2024', '2023'] }, ' 电费 '), [{ field: 'correspondent', op: 'in', value: [9] }]));
    expect(f).toEqual({
      all: [
        { field: 'correspondent', op: 'in', value: [9] },
        { any: [{ field: 'type', op: 'in', value: [3] }, { field: 'type', op: 'is_null' }] },
        { field: 'tag', op: 'has_any', value: [7], descendants: true },
        { any: [{ field: 'created', op: 'between', value: ['2024-01-01', '2024-12-31'] }, { field: 'created', op: 'between', value: ['2023-01-01', '2023-12-31'] }] },
        { text: '电费' },
      ],
    });
  });

  it('套用常用筛选：认得的条件还原成筛选条，认不出的保留为 extra', () => {
    const s = state({ type: [3, 0], correspondent: [0], tag: [7], year: ['2024'] }, '电费');
    expect(fromFilter(JSON.parse(toFilter(s)))).toEqual({ q: '电费', f: s.f });
    // v1.6.1 起自定义字段条件也认得；认不出的形状（全文的“非”）保留为 extra
    const custom = { field: 'cf:12', op: 'gt', value: '1000.00' };
    const odd = { not: { text: '广告' } };
    const back = fromFilter({ all: [{ field: 'type', op: 'in', value: [3] }, custom, odd] });
    expect(back.f.type).toEqual([3]);
    expect(back.f.fields).toEqual([{ fc: { fieldId: 12, op: 'gt', value: '1000.00' }, cond: custom }]);
    expect(back.extra).toEqual(odd);
    expect(JSON.parse(toFilter(back))).toEqual({ all: [{ field: 'type', op: 'in', value: [3] }, custom, odd] });
    expect(fromFilter({})).toEqual({ q: '', f: emptyListFilters() });
  });

  it('解析常用筛选的排序', () => {
    expect(parseSort('title:asc')).toEqual({ sort: 'title', dir: 'asc' });
    expect(parseSort('')).toBeNull();
    // v1.5.6：v1.4.3 的排序和自定义字段（field:12:asc 有两个冒号）
    expect(parseSort('amount:desc')).toEqual({ sort: 'amount', dir: 'desc' });
    expect(parseSort('modified:desc')).toEqual({ sort: 'modified', dir: 'desc' });
    expect(parseSort('field:12:asc')).toEqual({ sort: 'field:12', dir: 'asc' });
    expect(parseSort('field:x:asc')).toBeNull();
    expect(parseSort('field:12')).toBeNull();
    expect(parseSort('size:desc')).toBeNull();
  });
});

describe('日期筛选（v1.5.2）', () => {
  const f = (date: object) => ({ q: '', f: { ...emptyListFilters(), date } });
  it('时间段、自定义起止、按添加时间，互相转换', () => {
    for (const [date, cond] of [
      [{ period: 'last_month' }, { field: 'created', op: 'period', value: 'last_month' }],
      [{ period: 'this_year', added: true }, { field: 'added', op: 'period', value: 'this_year' }],
      [{ from: '2024-03-01', to: '2024-03-15' }, { field: 'created', op: 'between', value: ['2024-03-01', '2024-03-15'] }],
      [{ from: '2024-03-01' }, { field: 'created', op: 'gte', value: '2024-03-01' }],
      [{ to: '2024-03-15', added: true }, { field: 'added', op: 'lte', value: '2024-03-15' }],
    ] as const) {
      expect(JSON.parse(toFilter(f(date)))).toEqual(cond);
      expect(fromFilter(cond as Cond).f.date).toEqual(date);
    }
  });
  it('整年仍读作年份；说明文字', () => {
    expect(fromFilter({ field: 'created', op: 'between', value: ['2024-01-01', '2024-12-31'] }).f).toMatchObject({ year: ['2024'] });
    expect(describeDate({ period: 'last_30_days' })).toBe('日期：最近 30 天');
    expect(describeDate({ from: '2024-03-01', to: '2024-03-15', added: true })).toBe('添加时间：2024-03-01 至 2024-03-15');
  });
});

describe('筛选条件里的搜索词', () => {
  it('从顶层或 all / any 里取出，没有时为空', () => {
    expect(filterText(JSON.stringify({ text: ' 电费 ' }))).toBe('电费');
    expect(filterText(JSON.stringify({ all: [{ field: 'tag', op: 'has_any', value: [1] }, { text: '保修卡' }] }))).toBe('保修卡');
    expect(filterText(JSON.stringify({ all: [{ any: [{ text: '合同' }] }] }))).toBe('合同');
    expect(filterText(JSON.stringify({ field: 'asn', op: 'eq', value: 3 }))).toBe('');
    expect(filterText('')).toBe('');
    expect(filterText('{bad')).toBe('');
  });
});

describe('v1.6.1 的筛选', () => {
  it('标签的全部含有、不含、没有标签；类型、往来方的不是', () => {
    const conds = (f: Partial<ReturnType<typeof emptyListFilters>>) => JSON.parse(toFilter(state(f)) || 'null');
    expect(conds({ tag: [7, 8], tagMode: 'all' })).toEqual({ field: 'tag', op: 'has_all', value: [7, 8], descendants: true });
    expect(conds({ tag: [7], tagMode: 'none' })).toEqual({ field: 'tag', op: 'has_none', value: [7], descendants: true });
    expect(conds({ tagNone: true })).toEqual({ field: 'tag', op: 'is_null' });
    expect(conds({ type: [3], typeNot: true })).toEqual({ field: 'type', op: 'not_in', value: [3] });
    expect(conds({ correspondent: [5, 0], correspondentNot: true })).toEqual({ all: [{ field: 'correspondent', op: 'not_in', value: [5] }, { field: 'correspondent', op: 'not_null' }] });
    for (const f of [{ tag: [7, 8], tagMode: 'all' as const }, { tag: [7], tagMode: 'none' as const }, { tagNone: true }, { type: [3], typeNot: true }, { correspondent: [5, 0], correspondentNot: true }]) {
      expect(fromFilter(JSON.parse(toFilter(state(f)))).f).toMatchObject(f);
    }
  });

  it('文件格式与原件编号', () => {
    const s = state({ kinds: ['pdf', 'image'], asn: { from: 10, to: 20 } });
    expect(JSON.parse(toFilter(s))).toEqual({ all: [{ field: 'mime', op: 'kind', value: ['pdf', 'image'] }, { field: 'asn', op: 'between', value: [10, 20] }] });
    expect(fromFilter(JSON.parse(toFilter(s))).f).toMatchObject({ kinds: ['pdf', 'image'], asn: { from: 10, to: 20 } });
    expect(JSON.parse(toFilter(state({ asn: { none: true } })))).toEqual({ field: 'asn', op: 'is_null' });
    expect(describeAsn({ from: 10 })).toBe('原件编号：10 起');
  });
});

describe('v1.6.3 的家人和存放位置', () => {
  it('家人是、不是、没有家人；存放位置；能读回来', () => {
    const conds = (f: Partial<ReturnType<typeof emptyListFilters>>) => JSON.parse(toFilter(state(f)) || 'null');
    expect(conds({ person: [3] })).toEqual({ field: 'person', op: 'in', value: [3] });
    expect(conds({ person: [0] })).toEqual({ field: 'person', op: 'is_null' });
    expect(conds({ person: [3, 0], personNot: true })).toEqual({ all: [{ field: 'person', op: 'not_in', value: [3] }, { field: 'person', op: 'not_null' }] });
    expect(conds({ location: [5, 0] })).toEqual({ any: [{ field: 'location', op: 'in', value: [5] }, { field: 'location', op: 'is_null' }] });
    for (const f of [{ person: [3] }, { person: [3, 0], personNot: true }, { location: [5] }, { person: [3], location: [5], correspondent: [1] }]) {
      expect(fromFilter(JSON.parse(toFilter(state(f)))).f).toMatchObject(f);
    }
  });
});

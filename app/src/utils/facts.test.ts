import { describe, expect, it } from 'vitest';
import { factView } from './facts';
import type { FactValue } from '../api/drawers';

const fact = (kind: FactValue['fact']['kind'], config = {}) => ({ id: 1, drawerId: 1, kind, label: '要点', config });

describe('要点的显示', () => {
  it('到期', () => {
    const v = factView({ fact: fact('due'), warn: true, dueDate: '2024-10-29', daysLeft: 21, document: { id: 1, title: '护照', date: '2019-10-29' } });
    expect(v).toMatchObject({ value: '2024-10-29', note: '还剩 21 天 · 护照', warn: true });
    expect(factView({ fact: fact('due'), warn: true, dueDate: '2024-10-01', daysLeft: -7 }).note).toBe('已过期 7 天');
  });
  it('汇总带月度小柱', () => {
    const months = Array.from({ length: 12 }, (_, i) => ({ month: `2024-${String(i + 1).padStart(2, '0')}`, total: i === 2 ? '300.00' : '0.00' }));
    const v = factView({ fact: fact('sum', { period: 'this_year' }), warn: false, total: '3812.40', money: true, count: 18, months });
    expect(v.value).toBe('¥3,812');
    expect(v.note).toBe('今年 · 18 份');
    expect(v.bars).toHaveLength(12);
    expect(v.bars![2]).toEqual({ h: 24, title: '3 月 · ¥300.00' });
  });
  it('周期检查、最新值、固定文档', () => {
    expect(factView({ fact: fact('expect', { period: 'monthly' }), warn: true, periodLabel: '10 月', present: false })).toMatchObject({ value: '10 月未存入', note: '每月一份', warn: true });
    expect(factView({ fact: fact('latest'), warn: false, fieldValue: '326.40', fieldType: 'monetary', document: { id: 2, title: '9月电费', date: '2024-09-05' } }))
      .toMatchObject({ value: '¥326.40', note: '9月电费 · 2024-09-05' });
    expect(factView({ fact: fact('pinned'), warn: false, documents: [{ id: 3, title: '房产证', date: '2020-01-01' }] }).links).toEqual([{ id: 3, title: '房产证' }]);
  });
});

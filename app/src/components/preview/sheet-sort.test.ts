import { describe, expect, it } from 'vitest';
import { cellDate, cellNumber, columnValues, compareCells, findHeader, isActive, viewRows, type SheetViewState } from './sheet-sort';

const none: SheetViewState = { header: 0, sort: null, filters: {}, query: '' };

describe('表格的排序和筛选（v1.11.4）', () => {
  it('认得数字、金额、百分数、日期', () => {
    expect(cellNumber('1,234.5')).toBe(1234.5);
    expect(cellNumber('¥ 58.00')).toBe(58);
    expect(cellNumber('-3')).toBe(-3);
    expect(cellNumber('12%')).toBeCloseTo(0.12);
    expect(cellNumber('三十')).toBeNull();
    expect(cellNumber('2024-03-05')).toBeNull();
    expect(cellDate('2024-03-05')).toBe(Date.UTC(2024, 2, 5));
    expect(cellDate('2024/3/5')).toBe(Date.UTC(2024, 2, 5));
    expect(cellDate('2024年3月5日')).toBe(Date.UTC(2024, 2, 5));
    expect(cellDate('2024-03-05 08:30')).toBe(Date.UTC(2024, 2, 5, 8, 30));
    expect(cellDate('2024-13-05')).toBeNull();
  });

  it('比较：日期按日期，数字按数值，文字按中文排序，数字在文字前', () => {
    expect(compareCells('2024/3/5', '2024-12-01')).toBeLessThan(0);
    expect(compareCells('9', '10')).toBeLessThan(0);
    expect(compareCells('1,000', '999')).toBeGreaterThan(0);
    expect(compareCells('电费', '水费')).toBeLessThan(0); // dian < shui
    expect(compareCells('12', '燃气')).toBeLessThan(0);
  });

  it('找标题行：跳过横跨大半张表的合并标题', () => {
    expect(findHeader([['日期', '项目', '金额'], ['2026-10-04', '电费', '212.4']], [], 3)).toBe(0);
    expect(findHeader([['2026 年家庭支出'], ['日期', '项目', '金额'], ['a', 'b', 'c']], [[0, 0, 0, 2]], 3)).toBe(1);
    expect(findHeader([['1'], ['2'], ['3']], [], 1)).toBe(-1);
  });

  const rows = [['日期', '项目', '金额'], ['2026-10-04', '电费', '212.4'], ['2026-09-04', '水费', '58'], ['2026-10-12', '电费', ''], ['2026-08-30', '燃气', '1,020']];

  it('排序：标题行不动，空白在最后，一样的保持原来顺序', () => {
    expect(viewRows(rows, { ...none, sort: { col: 2, dir: 'asc' } }).data).toEqual([2, 1, 4, 3]);
    expect(viewRows(rows, { ...none, sort: { col: 2, dir: 'desc' } }).data).toEqual([4, 1, 2, 3]);
    expect(viewRows(rows, { ...none, sort: { col: 0, dir: 'asc' } }).data).toEqual([4, 2, 1, 3]);
    expect(viewRows(rows, { ...none, sort: { col: 1, dir: 'asc' } }).data).toEqual([1, 3, 4, 2]);
    expect(viewRows(rows, { ...none, sort: { col: 2, dir: 'asc' } }).fixed).toEqual([0]);
  });

  it('筛选：按值、按搜索，组合起来', () => {
    expect(viewRows(rows, { ...none, filters: { 1: ['电费'] } }).data).toEqual([1, 3]);
    expect(viewRows(rows, { ...none, query: '2026-10' }).data).toEqual([1, 3]);
    const both = viewRows(rows, { ...none, filters: { 1: ['电费', '水费'] }, query: '58' });
    expect(both.data).toEqual([2]);
    expect(both.total).toBe(4);
    expect(viewRows(rows, { ...none, header: -1 }).data).toHaveLength(5);
    expect(isActive(none)).toBe(false);
    expect(isActive({ ...none, query: ' x ' })).toBe(true);
  });

  it('合计行留在最后，不参加排序和筛选', () => {
    const withTotal = [...rows, ['', '合计', '1,488.4']];
    const v = viewRows(withTotal, { ...none, sort: { col: 2, dir: 'desc' } });
    expect(v.data).toEqual([4, 1, 2, 3]);
    expect(v.footer).toEqual([5]);
    expect(v.total).toBe(4);
    expect(viewRows(withTotal, { ...none, filters: { 1: ['电费'] } }).footer).toEqual([5]);
    expect(columnValues(withTotal, 0, 1).map((x) => x.value)).not.toContain('合计');
    expect(viewRows(rows, none).footer).toEqual([]);
  });

  it('列的值和次数', () => {
    expect(columnValues(rows, 0, 1)).toEqual([{ value: '电费', count: 2 }, { value: '燃气', count: 1 }, { value: '水费', count: 1 }]);
    const amounts = columnValues(rows, 0, 2);
    expect(amounts[amounts.length - 1]).toEqual({ value: '', count: 1 });
  });
});

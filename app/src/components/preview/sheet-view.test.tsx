import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { SheetView } from './sheet-view';

afterEach(() => { vi.unstubAllGlobals(); });

const sheets = { sheets: [
  { name: '支出', merges: [[0, 0, 0, 2]], truncated: false, rows: [
    ['2026 年家庭支出'], ['日期', '项目', '金额'],
    ['2026-10-04', '电费', '212.4'], ['2026-09-04', '水费', '58'], ['2026-10-12', '电费', '198'], ['2026-08-30', '燃气', '1,020'],
  ] },
  { name: '收入', merges: [], truncated: false, rows: [['月份', '金额'], ['9 月', '18000'], ['10 月', '18500']] },
] };

/** 表格里数据行的行号（第一格是原来的行号） */
function rowNumbers(grid: HTMLElement): string[] {
  return within(grid).getAllByRole('row').slice(1).map((r) => within(r).getAllByRole('rowheader')[0]?.textContent ?? '').filter(Boolean);
}

describe('表格的排序和筛选（v1.11.4）', () => {
  it('点列头排序：标题行固定，行号保留原来的；合并单元格在排序时不合并', async () => {
    stubFetch({ 'GET /api/v1/documents/5/sheets': () => jsonResponse(200, sheets) });
    renderWithProviders(<SheetView docId={5} />);
    const grid = await screen.findByRole('grid');
    expect(screen.getByLabelText('标题行')).toHaveTextContent('第 2 行');
    expect(within(grid).getByText('2026 年家庭支出')).toHaveAttribute('colspan', '3');
    await userEvent.click(screen.getByRole('button', { name: '金额 列：排序和筛选' }));
    await userEvent.click(screen.getByRole('button', { name: '降序' }));
    expect(rowNumbers(grid)).toEqual(['1', '2', '6', '3', '5', '4']);
    expect(within(grid).getByText('2026 年家庭支出')).not.toHaveAttribute('colspan');
    expect(screen.getByText('排序或筛选时，合并的单元格不再合并')).toBeInTheDocument();
    expect(screen.getByText('显示 4 / 4 行')).toBeInTheDocument();
  });

  it('按值筛选、搜索，清除；切换工作表各自保留', async () => {
    stubFetch({ 'GET /api/v1/documents/5/sheets': () => jsonResponse(200, sheets) });
    renderWithProviders(<SheetView docId={5} />);
    const grid = await screen.findByRole('grid');
    await userEvent.click(screen.getByRole('button', { name: '项目 列：排序和筛选' }));
    await userEvent.click(screen.getByRole('checkbox', { name: '全选' }));
    await userEvent.click(screen.getByRole('checkbox', { name: /电费/ }));
    await userEvent.click(screen.getByRole('button', { name: '确定' }));
    expect(rowNumbers(grid)).toEqual(['1', '2', '3', '5']);
    expect(screen.getByText('显示 2 / 4 行')).toBeInTheDocument();
    await userEvent.type(screen.getByRole('textbox', { name: '在表里找' }), '10-12');
    expect(rowNumbers(grid)).toEqual(['1', '2', '5']);

    await userEvent.click(screen.getByRole('tab', { name: '收入' }));
    expect(await screen.findByText('显示 2 / 2 行')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('tab', { name: '支出' }));
    expect(await screen.findByText('显示 1 / 4 行')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '清除排序和筛选' }));
    expect(screen.getByText('显示 4 / 4 行')).toBeInTheDocument();
  });

  it('筛得一行不剩时照实说', async () => {
    stubFetch({ 'GET /api/v1/documents/5/sheets': () => jsonResponse(200, sheets) });
    renderWithProviders(<SheetView docId={5} />);
    await screen.findByRole('grid');
    await userEvent.type(screen.getByRole('textbox', { name: '在表里找' }), '没有这个');
    expect(screen.getByText('没有符合筛选的行')).toBeInTheDocument();
  });
});

import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import YearbookPage from './yearbook-page';

const months = (n: number[]) => Array.from({ length: 12 }, (_, i) => n[i] ?? 0);
const empty = { year: 2026, asOf: '2026-10-02', documents: 0, newCorrespondents: 0, busiestMonth: 0, months: months([]), drawers: [], unfiled: 0, tags: [], events: [] };
const y2024 = {
  year: 2024, documents: 46, newCorrespondents: 5, topType: { id: 1, name: '账单', count: 30 }, busiestMonth: 9, months: months([4, 4, 4, 5, 5, 4, 4, 5, 6, 1, 2]),
  drawers: [{ id: 1, name: '房子', icon: 'home', color: 'blue', count: 24, sums: [{ label: '水电费合计', total: '3576.00', money: true }] }],
  unfiled: 22, tags: [{ id: 3, name: '税务', color: '#3A6EA5', count: 2, amount: '18000.00' }],
  events: [{ date: '03-20', kind: 'milestone', text: '租金发票 2024年第一季度（链家）', documentId: 9 }],
};

afterEach(() => { vi.unstubAllGlobals(); });

const editor = () => jsonResponse(200, { user: { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false },
  workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 2 }, csrfToken: 'tok' });

describe('年度回顾', () => {
  it('今年还没有文档时改看最近有文档的一年；数字、各抽屉、用途标签、大事记', async () => {
    stubFetch({
      'GET /api/v1/yearbook': () => jsonResponse(200, [2026, 2024, 2023]),
      'GET /api/v1/yearbook/2026': () => jsonResponse(200, empty),
      'GET /api/v1/yearbook/2024': () => jsonResponse(200, y2024),
    });
    renderWithProviders(<YearbookPage />);
    expect(await screen.findByRole('heading', { name: '2024' })).toBeInTheDocument();
    expect(screen.getByText('新增往来方 5 家 · 账单最多（30 份） · 9 月最忙')).toBeInTheDocument();
    const drawers = screen.getByRole('region', { name: '各抽屉' });
    expect(within(drawers).getByText('水电费合计 ¥3,576')).toBeInTheDocument();
    expect(within(drawers).getByText('22 份')).toBeInTheDocument();
    expect(screen.getByText('2 份，合计 ¥18,000')).toBeInTheDocument();
    expect(screen.getByText('租金发票 2024年第一季度（链家）')).toBeInTheDocument();
    expect(screen.getByRole('img', { name: /9 月 6 份/ })).toBeInTheDocument();
  });
});

describe('年度回顾增强', () => {
  const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
  const me = (role: string) => () => jsonResponse(200, { user, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role, memberCount: 2 }, csrfToken: 'tok' });
  const rich = {
    ...y2024,
    previous: { year: 2023, documents: 40, drawers: [{ id: 1, name: '房子', icon: 'home', color: 'blue', count: 20, sums: [{ label: '水电费合计', total: '3256.00', money: true }] }], tags: [] },
    history: [{ year: 2020, documents: 0 }, { year: 2021, documents: 3 }, { year: 2022, documents: 12 }, { year: 2023, documents: 40 }, { year: 2024, documents: 46 }],
    storage: { originals: 30 << 20, searchable: 12 << 20, thumbnails: 1 << 20, versions: 0, total: 43 << 20, workspace: 120 << 20, largest: [{ id: 9, title: '车险保单 2024', bytes: 8 << 20 }] },
  };

  it('与上一年的对比、近五年、存储', async () => {
    stubFetch({
      'GET /api/v1/me': me('editor'),
      'GET /api/v1/yearbook': () => jsonResponse(200, [2024, 2023]),
      'GET /api/v1/yearbook/2024': () => jsonResponse(200, rich),
    });
    renderWithProviders(<YearbookPage />);
    expect(await screen.findByText('比 2023 年多 6 份（+15%）')).toBeInTheDocument();
    expect(screen.getByRole('img', { name: /2022 年 12 份/ })).toBeInTheDocument();
    const drawers = screen.getByRole('region', { name: '各抽屉' });
    expect(within(drawers).getByText('+4')).toBeInTheDocument();
    expect(within(drawers).getByText('+¥320')).toBeInTheDocument();
    const storage = screen.getByRole('region', { name: '存储' });
    expect(within(storage).getByText('43.0 MB')).toBeInTheDocument();
    expect(within(storage).getByRole('link', { name: '车险保单 2024' })).toHaveAttribute('href', '/docs/9');
    expect(screen.queryByRole('button', { name: '规则' })).not.toBeInTheDocument();
  });

  it('空间管理员编辑大事记规则', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': me('owner'),
      'GET /api/v1/yearbook': () => jsonResponse(200, [2024]),
      'GET /api/v1/yearbook/2024': () => jsonResponse(200, rich),
      'GET /api/v1/yearbook/rules': () => jsonResponse(200, { default: true, rules: [{ verb: '签订', filter: { field: 'type', op: 'in', value: [2] } }] }),
      'PUT /api/v1/yearbook/rules': () => jsonResponse(200, { default: false, rules: [] }),
      'GET /api/v1/document-types': () => jsonResponse(200, []),
      'GET /api/v1/correspondents': () => jsonResponse(200, []),
      'GET /api/v1/tags': () => jsonResponse(200, []),
      'GET /api/v1/custom-fields': () => jsonResponse(200, []),
      'GET /api/v1/drawers': () => jsonResponse(200, []),
    });
    renderWithProviders(<YearbookPage />);
    await userEvent.click(await screen.findByRole('button', { name: '规则' }));
    const verb = await screen.findByRole('textbox', { name: '动词' });
    expect(verb).toHaveValue('签订');
    await userEvent.clear(verb);
    await userEvent.type(verb, '续签');
    await userEvent.click(screen.getByRole('button', { name: '保存' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'PUT')).toBe(true));
    const body = await calls.find((r) => r.method === 'PUT')!.clone().json();
    expect(body).toEqual({ rules: [{ verb: '续签', filter: { field: 'type', op: 'in', value: [2] } }] });
  });
});

describe('读完的书', () => {
  it('列出自己这一年读完的书，开篇写本数；没有时不显示', async () => {
    stubFetch({
      'GET /api/v1/yearbook': () => jsonResponse(200, [2024]),
      'GET /api/v1/yearbook/2024': () => jsonResponse(200, { ...y2024, booksFinished: 2, books: [
        { id: 11, title: '红楼梦', finishedOn: '2024-05-20', rating: 5, hasCover: true, hasThumbnail: true },
        { id: 12, title: '呐喊', finishedOn: '2024-09-03', rating: 0, hasCover: false, hasThumbnail: false },
      ] }),
    });
    renderWithProviders(<YearbookPage />);
    const sec = await screen.findByRole('region', { name: '读完的书' });
    expect(screen.getByText('读完了 2 本书')).toBeInTheDocument();
    expect(within(sec).getByText('5 月读完')).toBeInTheDocument();
    expect(within(sec).getByRole('img', { name: '5 星' })).toBeInTheDocument();
    expect(within(sec).getAllByRole('link')[0]).toHaveAttribute('href', '/docs/11');
  });

  it('这一年没有读完的书', async () => {
    stubFetch({ 'GET /api/v1/yearbook': () => jsonResponse(200, [2024]), 'GET /api/v1/yearbook/2024': () => jsonResponse(200, { ...y2024, booksFinished: 0, books: [] }) });
    renderWithProviders(<YearbookPage />);
    await screen.findByRole('heading', { name: '2024' });
    expect(screen.queryByRole('region', { name: '读完的书' })).not.toBeInTheDocument();
    expect(screen.queryByText(/读完了/)).not.toBeInTheDocument();
  });

  it('抽屉管家的叙述：注明由 AI 生成，数据有更新时提示；可以重写', async () => {
    let written = false;
    const calls = stubFetch({
      'GET /api/v1/me': editor,
      'GET /api/v1/yearbook': () => jsonResponse(200, [2024]),
      'GET /api/v1/yearbook/2024': () => jsonResponse(200, { ...y2024, narrativeAvailable: true,
        narrative: written ? { text: '新写的一段。', createdAt: '2026-10-06T09:00:00Z', stale: false } : { text: '这一年你们存了 46 份文档，账单最多。', createdAt: '2026-01-02T03:30:00Z', stale: true } }),
      'POST /api/v1/yearbook/2024/narrative': () => { written = true; return jsonResponse(202, { jobId: 3 }); },
    });
    renderWithProviders(<YearbookPage />);
    const box = await screen.findByRole('region', { name: 'AI 叙述' });
    expect(within(box).getByText('这一年你们存了 46 份文档，账单最多。')).toBeInTheDocument();
    expect(within(box).getByText(/由 AI 生成/)).toHaveTextContent('由 AI 生成 · 数据有更新');
    await userEvent.click(await within(box).findByRole('button', { name: '重写' }));
    expect(await within(box).findByText('新写的一段。', {}, { timeout: 4000 })).toBeInTheDocument();
    expect(calls.some((r) => r.method === 'POST')).toBe(true);
    expect(within(box).getByRole('button', { name: '重写' })).toBeEnabled();
  });

  it('没有开启叙述时不显示', async () => {
    stubFetch({ 'GET /api/v1/me': editor, 'GET /api/v1/yearbook': () => jsonResponse(200, [2024]), 'GET /api/v1/yearbook/2024': () => jsonResponse(200, y2024) });
    renderWithProviders(<YearbookPage />);
    await screen.findByRole('heading', { name: '2024' });
    expect(screen.queryByRole('region', { name: 'AI 叙述' })).not.toBeInTheDocument();
  });
});

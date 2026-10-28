import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken } from '../../api/client';
import { ConfirmDialog } from '../../components/overlays';
import { DrawerDialog } from '../../components/drawer-dialogs';
import DrawerPage from './drawer-page';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const library = {
  id: 9, kind: 'library', name: '书房', icon: 'book', color: 'orange', description: '所有书籍，按封面排列', documentCount: 2, alertCount: 0, createdAt: '2026-10-01T00:00:00Z',
  rule: { field: 'book', op: 'eq', value: true }, manualCount: 0, firstYear: 2024, lastYear: 2024, facts: [],
};
const book = (id: number, title: string, hasThumbnail: boolean, reading?: { status: string; percent: number }) => ({ reading,
  id, title, createdDate: '2024-03-05', addedAt: '2024-10-01T08:00:00Z', modifiedAt: '2024-10-01T08:00:00Z', mimeType: 'application/pdf', originalFilename: 'a.pdf',
  pageCount: 1, hasArchive: false, hasThumbnail, inbox: false, tagIds: [], noteCount: 0, drawerId: 9, drawerManual: false, isMilestone: false, isBook: true,
});

function backend(extra: Record<string, (r: Request) => Response> = {}, drawers: unknown[] = [library]) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 2 }, csrfToken: 't' }),
    'GET /api/v1/drawers/9': () => jsonResponse(200, library),
    'GET /api/v1/drawers': () => jsonResponse(200, { items: drawers, unfiled: 0 }),
    'GET /api/v1/documents': () => jsonResponse(200, { items: [book(1, '活着', true, { status: 'finished', percent: 1 }), book(2, '许三观卖血记', false, { status: 'reading', percent: 0.3 })], total: 2 }),
    'GET /api/v1/documents/facets': () => jsonResponse(200, {}),
    'GET /api/v1/document-types': () => jsonResponse(200, []),
    'GET /api/v1/correspondents': () => jsonResponse(200, []),
    'GET /api/v1/tags': () => jsonResponse(200, []),
    'GET /api/v1/custom-fields': () => jsonResponse(200, []),
    ...extra,
  });
}

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); try { localStorage.clear(); } catch { /* */ } });

describe('书房', () => {
  it('封面墙；没有缩略图时显示书名；不能编辑收纳规则', async () => {
    const calls = backend();
    renderWithProviders(<Routes><Route path="/drawers/:id" element={<DrawerPage />} /></Routes>, { route: '/drawers/9' });
    expect(await screen.findByText(/2 本 · 收纳：所有书籍/)).toBeInTheDocument();
    const wall = await screen.findByRole('list', { name: '封面' });
    expect(within(wall).getAllByRole('listitem')).toHaveLength(2);
    expect(within(within(wall).getByRole('listitem', { name: '许三观卖血记' })).getAllByText('许三观卖血记')).toHaveLength(2);
    expect(screen.queryByRole('button', { name: '编辑收纳规则' })).not.toBeInTheDocument();
    expect(within(wall).getByRole('button', { name: '阅读“活着”' })).toBeInTheDocument();
    const list = calls.find((c) => c.url.includes('/api/v1/documents?'));
    expect(new URL(list!.url).searchParams.get('filter')).toBe('{"field":"drawer","op":"in","value":[9]}');
    await userEvent.click(screen.getByRole('button', { name: '列表' }));
    expect(screen.queryByRole('list', { name: '封面' })).not.toBeInTheDocument();
  });

  it('关闭书房要先确认', async () => {
    let closed = false;
    backend({ 'DELETE /api/v1/library': () => { closed = true; return jsonResponse(200, { jobId: 3 }); } });
    renderWithProviders(<><Routes><Route path="/drawers/:id" element={<DrawerPage />} /><Route path="/" element={<div>首页</div>} /></Routes><ConfirmDialog /></>, { route: '/drawers/9' });
    await userEvent.click(await screen.findByRole('button', { name: '更多' }));
    expect(screen.queryByRole('menuitem', { name: '删除抽屉' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('menuitem', { name: '关闭书房' }));
    await userEvent.click(await screen.findByRole('button', { name: '关闭' }));
    await waitFor(() => expect(closed).toBe(true));
  });

  it('新建抽屉时可以开启书房；已经开启时不再提示', async () => {
    let enabled = false;
    backend({ 'POST /api/v1/library': () => { enabled = true; return jsonResponse(200, { drawer: library, jobId: 4 }); } }, []);
    const onCreated = vi.fn();
    const { unmount } = renderWithProviders(<DrawerDialog open onClose={() => {}} onCreated={onCreated} />);
    await userEvent.click(await screen.findByRole('button', { name: '开启书房' }));
    await waitFor(() => expect(enabled).toBe(true));
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(expect.objectContaining({ id: 9 })));
    unmount();
    vi.unstubAllGlobals();
    backend();
    renderWithProviders(<DrawerDialog open onClose={() => {}} />);
    expect(await screen.findByRole('textbox', { name: '名称' })).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole('button', { name: '开启书房' })).not.toBeInTheDocument();
  });

  it('按自己的阅读状态筛选；读完的标记、在读的进度', async () => {
    const calls = backend();
    renderWithProviders(<Routes><Route path="/drawers/:id" element={<DrawerPage />} /></Routes>, { route: '/drawers/9' });
    const wall = await screen.findByRole('list', { name: '封面' });
    expect(within(within(wall).getByRole('listitem', { name: '活着' })).getByLabelText('读完')).toBeInTheDocument();
    expect(within(within(wall).getByRole('listitem', { name: '许三观卖血记' })).getByRole('progressbar', { name: '读到 30%' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '在读' }));
    await waitFor(() => expect(calls.some((c) => (new URL(c.url).searchParams.get('filter') ?? '').includes('reading'))).toBe(true));
    const last = calls.filter((c) => c.url.includes('/api/v1/documents?')).pop()!;
    expect(JSON.parse(new URL(last.url).searchParams.get('filter')!)).toEqual({ all: [{ field: 'drawer', op: 'in', value: [9] }, { field: 'reading', op: 'in', value: ['reading'] }] });
    expect(screen.getByRole('button', { name: '我的批注' })).toBeInTheDocument();
  });
});


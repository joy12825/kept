import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken } from '../../api/client';
import { ConfirmDialog } from '../../components/overlays';
import BookListsPage from './book-lists-page';
import BookListPage from './book-list-page';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const me = (role = 'editor') => () => jsonResponse(200, { user, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role, memberCount: 2 }, csrfToken: 't' });
const item = (documentId: number, title: string, note = '', reading?: unknown) => ({ documentId, title, authors: ['作者'], hasCover: false, hasThumbnail: false, note, addedAt: '2024-10-01T00:00:00Z', reading });
const list = { id: 3, name: '四大名著', description: '经典', count: 2, preview: [item(1, '红楼梦'), item(2, '西游记')], createdAt: '2024-10-01T00:00:00Z', updatedAt: '2024-10-01T00:00:00Z' };
const detail = { ...list, items: [item(1, '红楼梦', '值得反复读', { status: 'finished', percent: 1 }), item(2, '西游记')] };
const doc = (id: number, title: string) => ({ id, title, createdDate: '2024-01-01', addedAt: '2024-01-01T00:00:00Z', modifiedAt: '2024-01-01T00:00:00Z', mimeType: 'application/pdf', originalFilename: 'a.pdf', pageCount: 1, hasArchive: false, hasThumbnail: false, inbox: false, tagIds: [], noteCount: 0, drawerManual: false, isMilestone: false, isBook: true });

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); });

describe('书单', () => {
  it('列表：本数与说明；新建后进入书单', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': me(),
      'GET /api/v1/book-lists': () => jsonResponse(200, [list]),
      'POST /api/v1/book-lists': () => jsonResponse(201, { ...list, id: 4, name: '暑假', count: 0, preview: [] }),
      'GET /api/v1/book-lists/4': () => jsonResponse(200, { ...detail, id: 4, name: '暑假', items: [] }),
      'GET /api/v1/documents': () => jsonResponse(200, { items: [], total: 0 }),
    });
    renderWithProviders(<Routes><Route path="/book-lists" element={<BookListsPage />} /><Route path="/book-lists/:id" element={<BookListPage />} /></Routes>, { route: '/book-lists' });
    const card = await screen.findByRole('listitem', { name: '四大名著' });
    expect(within(card).getByText('2 本 · 经典')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '新建书单' }));
    const dlg = await screen.findByRole('dialog', { name: '新建书单' });
    await userEvent.type(within(dlg).getByRole('textbox', { name: '名称' }), '暑假');
    await userEvent.click(within(dlg).getByRole('button', { name: '新建' }));
    expect(await screen.findByRole('heading', { name: '暑假' })).toBeInTheDocument();
    expect(await calls.find((c) => c.method === 'POST')!.clone().json()).toEqual({ name: '暑假', description: '' });
  });

  it('详情：顺序、推荐语、自己的阅读状态；上移、改推荐语、添加、移出、删除', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': me(),
      'GET /api/v1/book-lists/3': () => jsonResponse(200, detail),
      'PUT /api/v1/book-lists/3/order': () => new Response(null, { status: 204 }),
      'PATCH /api/v1/book-lists/3/items/2': () => new Response(null, { status: 204 }),
      'POST /api/v1/book-lists/3/items': () => new Response(null, { status: 204 }),
      'DELETE /api/v1/book-lists/3/items/1': () => new Response(null, { status: 204 }),
      'DELETE /api/v1/book-lists/3': () => new Response(null, { status: 204 }),
      'GET /api/v1/book-lists': () => jsonResponse(200, []),
      'GET /api/v1/documents': () => jsonResponse(200, { items: [doc(1, '红楼梦'), doc(5, '三国演义')], total: 2 }),
    });
    renderWithProviders(<><Routes><Route path="/book-lists/:id" element={<BookListPage />} /><Route path="/book-lists" element={<div>书单列表</div>} /></Routes><ConfirmDialog /></>, { route: '/book-lists/3' });
    const books = await screen.findByRole('list', { name: '书单里的书' });
    const first = within(books).getByRole('listitem', { name: '红楼梦' });
    expect(within(first).getByText('作者 · 读完')).toBeInTheDocument();
    expect(within(first).getByText('值得反复读')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: '上移“西游记”' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    expect(await calls.find((c) => c.method === 'PUT')!.clone().json()).toEqual({ ids: [2, 1] });

    await userEvent.click(within(within(books).getByRole('listitem', { name: '西游记' })).getByRole('button', { name: '写一句推荐语…' }));
    await userEvent.type(screen.getByRole('textbox', { name: '推荐语' }), '取经路上{Enter}');
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    expect(await calls.find((c) => c.method === 'PATCH')!.clone().json()).toEqual({ note: '取经路上' });

    await userEvent.click(screen.getByRole('combobox', { name: '添加书' }));
    const opts = await screen.findAllByRole('option');
    expect(opts.find((o) => o.textContent === '红楼梦已在书单里')).toHaveAttribute('aria-disabled', 'true');
    await userEvent.click(screen.getByRole('option', { name: '三国演义' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    expect(await calls.find((c) => c.method === 'POST')!.clone().json()).toEqual({ documentId: 5 });

    await userEvent.click(screen.getByRole('button', { name: '把“红楼梦”移出书单' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url.endsWith('/items/1'))).toBe(true));

    await userEvent.click(screen.getByRole('button', { name: '更多' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '删除书单' }));
    await userEvent.click(await screen.findByRole('button', { name: '删除' }));
    expect(await screen.findByText('书单列表')).toBeInTheDocument();
  });

  it('只看的成员不能编辑', async () => {
    stubFetch({ 'GET /api/v1/me': me('viewer'), 'GET /api/v1/book-lists/3': () => jsonResponse(200, detail) });
    renderWithProviders(<Routes><Route path="/book-lists/:id" element={<BookListPage />} /></Routes>, { route: '/book-lists/3' });
    expect(await screen.findByText('值得反复读')).toBeInTheDocument();
    expect(screen.queryByRole('combobox', { name: '添加书' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '更多' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /上移/ })).not.toBeInTheDocument();
  });
});

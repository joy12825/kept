import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken } from '../../api/client';
import { formatIsbn, isbnValid } from '../../api/books';
import { splitAuthors } from './book';
import DetailPage from './detail-page';
import { ConfirmDialog } from '../../components/overlays';

vi.mock('../../components/pdf-preview', () => ({ PdfPreview: () => <div /> }));

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const book = {
  documentId: 7, isbn: '9787506365437', authors: ['余华'], publisher: '作家出版社', series: '', edition: '', published: '2012-08',
  pages: 191, language: '中文', description: '讲述了福贵的一生。', hasCover: false, updatedAt: '2026-10-06T00:00:00Z',
};
const doc = (extra: Record<string, unknown> = {}) => ({
  id: 7, title: '活着', createdDate: '2024-03-05', addedAt: '2024-10-01T08:00:00Z', modifiedAt: '2024-10-01T08:00:00Z',
  mimeType: 'application/pdf', originalFilename: 'huozhe.pdf', pageCount: 191, hasArchive: false, hasThumbnail: true, inbox: false, tagIds: [], noteCount: 0,
  content: '', checksum: 'ab'.repeat(32), fields: [], drawerManual: false, isMilestone: false, isBook: false, source: 'web', fieldSources: {}, ...extra,
});

function backend(d: ReturnType<typeof doc>, extra: Record<string, (r: Request) => Response | Promise<Response>> = {}, lookup = true) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 2 }, csrfToken: 't', features: { smtp: false, bookLookup: lookup } }),
    'GET /api/v1/documents/7': () => jsonResponse(200, d),
    'GET /api/v1/documents': () => jsonResponse(200, { items: [d], total: 1 }),
    'GET /api/v1/documents/7/notes': () => jsonResponse(200, []),
    'GET /api/v1/documents/7/history': () => jsonResponse(200, { items: [] }),
    'GET /api/v1/document-types': () => jsonResponse(200, []),
    'GET /api/v1/correspondents': () => jsonResponse(200, []),
    'GET /api/v1/tags': () => jsonResponse(200, []),
    'GET /api/v1/custom-fields': () => jsonResponse(200, []),
    'GET /api/v1/drawers': () => jsonResponse(200, { items: [], unfiled: 0 }),
    ...extra,
  });
}

const render = () => renderWithProviders(<><Routes><Route path="/docs/:id" element={<DetailPage />} /></Routes><ConfirmDialog /></>, { route: '/docs/7' });

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); });

describe('书目', () => {
  it('ISBN 校验与显示、作者拆分', () => {
    expect(isbnValid('978-7-5063-6543-7')).toBe(true);
    expect(isbnValid('7-5063-6543-X')).toBe(true);
    expect(isbnValid('978-7-5063-6543-8')).toBe(false);
    expect(isbnValid('12345')).toBe(false);
    expect(isbnValid('')).toBe(true);
    expect(formatIsbn('9787506365437')).toBe('978-7506365437');
    expect(splitAuthors('余华、 莫言,王小波；')).toEqual(['余华', '莫言', '王小波']);
  });

  it('书籍显示书目', async () => {
    backend(doc({ isBook: true, book }));
    render();
    const sec = await screen.findByRole('region', { name: '书目' });
    expect(within(sec).getByText('余华')).toBeInTheDocument();
    expect(within(sec).getByText('978-7506365437')).toBeInTheDocument();
    expect(within(sec).getByText('191 页')).toBeInTheDocument();
    expect(within(sec).getByText('讲述了福贵的一生。')).toBeInTheDocument();
  });

  it('设为书籍：ISBN 不对时不能保存，保存提交书目', async () => {
    const bodies: unknown[] = [];
    backend(doc(), { 'PUT /api/v1/documents/7/book': async (r) => { bodies.push(await r.json()); return jsonResponse(200, book); } });
    render();
    await userEvent.click(await screen.findByRole('button', { name: '更多' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '设为书籍' }));
    const dlg = await screen.findByRole('dialog', { name: '设为书籍' });
    await userEvent.type(within(dlg).getByRole('textbox', { name: 'ISBN' }), '978-7-5063-6543-8');
    expect(within(dlg).getByRole('button', { name: '保存' })).toBeDisabled();
    await userEvent.clear(within(dlg).getByRole('textbox', { name: 'ISBN' }));
    await userEvent.type(within(dlg).getByRole('textbox', { name: 'ISBN' }), '7-5063-6543-X');
    await userEvent.type(within(dlg).getByRole('textbox', { name: '作者' }), '余华');
    await userEvent.type(within(dlg).getByRole('textbox', { name: '页数' }), '191');
    await userEvent.click(within(dlg).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({ isbn: '7-5063-6543-X', authors: ['余华'], pages: 191, publisher: '' });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('不再作为书籍要先确认', async () => {
    let deleted = false;
    backend(doc({ isBook: true, book }), { 'DELETE /api/v1/documents/7/book': () => { deleted = true; return new Response(null, { status: 204 }); } });
    render();
    await userEvent.click(await screen.findByRole('button', { name: '更多' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '不再作为书籍' }));
    await userEvent.click(await screen.findByRole('button', { name: '确定' }));
    await waitFor(() => expect(deleted).toBe(true));
  });

  it('查找：只填空着的栏目，可以改用其他数据源；保存时改标题、存封面', async () => {
    const calls = backend(doc({ title: 'scan_0012' }), {
      'GET /api/v1/books/lookup': () => jsonResponse(200, {
        isbn: '9787506365437',
        results: [
          { source: 'openlibrary', sourceName: 'Open Library', title: '活着', authors: ['Yu Hua'], publisher: '', published: '2012-08', pages: 191, language: '', description: '', series: '', hasCover: true },
          { source: 'googlebooks', sourceName: 'Google Books', title: '活着', authors: ['余华'], publisher: '作家出版社', published: '2012', pages: 0, language: '中文', description: '长篇小说', series: '', hasCover: false },
        ],
        failures: [{ source: 'custom', sourceName: '自定义书目接口', message: '没有及时回应' }],
      }),
      'PUT /api/v1/documents/7/book': () => jsonResponse(200, book),
      'POST /api/v1/documents/7/book/cover/fetch': () => jsonResponse(200, { ...book, hasCover: true }),
      'PATCH /api/v1/documents/7': () => jsonResponse(200, doc({ title: '活着' })),
    });
    render();
    await userEvent.click(await screen.findByRole('button', { name: '更多' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '设为书籍' }));
    const dlg = await screen.findByRole('dialog', { name: '设为书籍' });
    await userEvent.type(within(dlg).getByRole('textbox', { name: '页数' }), '200');
    await userEvent.type(within(dlg).getByRole('textbox', { name: 'ISBN' }), '9787506365437');
    await userEvent.click(within(dlg).getByRole('button', { name: '查找' }));
    const found = await within(dlg).findByRole('alert', { name: '查找结果' });
    expect(found).toHaveTextContent('已从 Open Library 填入 2 项：《活着》');
    expect(found).toHaveTextContent('自定义书目接口：没有及时回应');
    expect(within(dlg).getByRole('textbox', { name: '作者' })).toHaveValue('Yu Hua');
    expect(within(dlg).getByRole('textbox', { name: '页数' })).toHaveValue('200');
    await userEvent.click(within(found).getByRole('button', { name: '改用 Google Books' }));
    expect(within(dlg).getByRole('textbox', { name: '作者' })).toHaveValue('余华');
    expect(within(dlg).getByRole('textbox', { name: '出版时间' })).toHaveValue('2012');
    expect(within(dlg).getByRole('textbox', { name: '出版社' })).toHaveValue('作家出版社');
    expect(within(dlg).queryByRole('checkbox', { name: '同时保存封面' })).not.toBeInTheDocument();
    await userEvent.click(within(dlg).getByRole('button', { name: '改用 Open Library' }));
    expect(within(dlg).getByRole('textbox', { name: '出版社' })).toHaveValue('');
    expect(within(dlg).getByRole('checkbox', { name: '文档标题改为《活着》' })).toBeChecked();
    expect(within(dlg).getByRole('checkbox', { name: '同时保存封面' })).toBeChecked();
    await userEvent.click(within(dlg).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    const sent = (m: string, path: string) => calls.filter((c) => c.method === m && new URL(c.url).pathname === path);
    expect(await sent('PUT', '/api/v1/documents/7/book')[0].clone().json()).toMatchObject({ isbn: '9787506365437', authors: ['Yu Hua'], pages: 200, published: '2012-08' });
    expect(await sent('POST', '/api/v1/documents/7/book/cover/fetch')[0].clone().json()).toEqual({ isbn: '9787506365437', source: 'openlibrary' });
    expect(await sent('PATCH', '/api/v1/documents/7')[0].clone().json()).toEqual({ title: '活着' });
  });

  it('在线查找关闭时没有“查找”', async () => {
    backend(doc(), {}, false);
    render();
    await userEvent.click(await screen.findByRole('button', { name: '更多' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '设为书籍' }));
    const dlg = await screen.findByRole('dialog', { name: '设为书籍' });
    expect(within(dlg).queryByRole('button', { name: '查找' })).not.toBeInTheDocument();
  });

  it('封面：显示、上传、删除', async () => {
    const calls = backend(doc({ isBook: true, book: { ...book, hasCover: true } }), {
      'PUT /api/v1/documents/7/book/cover': () => jsonResponse(200, book),
      'DELETE /api/v1/documents/7/book/cover': () => jsonResponse(200, { ...book, hasCover: false }),
    });
    render();
    const sec = await screen.findByRole('region', { name: '书目' });
    expect(within(sec).getByRole('img', { name: '封面' })).toHaveAttribute('src', expect.stringContaining('/api/v1/documents/7/cover'));
    await userEvent.upload(within(sec).getByLabelText('选择封面图'), new File(['png'], 'c.png', { type: 'image/png' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT' && c.url.endsWith('/book/cover'))).toBe(true));
    expect(calls.find((c) => c.method === 'PUT' && c.url.endsWith('/book/cover'))!.headers.get('Content-Type')).toBe('image/png');
    await userEvent.click(within(sec).getByRole('button', { name: '删除封面' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url.endsWith('/book/cover'))).toBe(true));
  });

  it('我的阅读：状态、评分，只发改的那一项', async () => {
    const reading = { position: 'page:3', percent: 0.42, status: 'reading', rating: 0, startedOn: '2024-10-01', finishedOn: '', highlightCount: 2 };
    const calls = backend(doc({ isBook: true, book }), {
      'GET /api/v1/documents/7/reading': () => jsonResponse(200, reading),
      'PATCH /api/v1/documents/7/reading': () => jsonResponse(200, { ...reading, status: 'finished', finishedOn: '2024-10-08' }),
    });
    render();
    const sec = await screen.findByRole('region', { name: '我的阅读' });
    expect(within(sec).getByRole('button', { name: '在读' })).toHaveAttribute('aria-pressed', 'true');
    expect(within(sec).getByText('读到 42%')).toBeInTheDocument();
    expect(within(sec).getByRole('link', { name: '批注 2 条' })).toHaveAttribute('href', '/highlights?doc=7');
    await userEvent.click(within(sec).getByRole('button', { name: '读完' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    expect(await calls.find((c) => c.method === 'PATCH')!.clone().json()).toEqual({ status: 'finished' });
    expect(await within(sec).findByRole('textbox', { name: '读完的日期' })).toHaveValue('2024-10-08');
  });

  it('加入书单：勾选已有的书单，或新建一个', async () => {
    const lists = [{ id: 3, name: '四大名著', description: '', count: 3, preview: [], createdAt: '2024-10-01T00:00:00Z', updatedAt: '2024-10-01T00:00:00Z' }];
    const calls = backend(doc({ isBook: true, book }), {
      'GET /api/v1/book-lists': () => jsonResponse(200, lists),
      'GET /api/v1/documents/7/book-lists': () => jsonResponse(200, []),
      'POST /api/v1/book-lists/3/items': () => new Response(null, { status: 204 }),
      'POST /api/v1/book-lists': () => jsonResponse(201, { ...lists[0], id: 4, name: '余华', count: 0 }),
      'POST /api/v1/book-lists/4/items': () => new Response(null, { status: 204 }),
    });
    render();
    const sec = await screen.findByRole('region', { name: '书目' });
    await userEvent.click(within(sec).getByRole('button', { name: '加入书单' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: /四大名著/ }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/book-lists/3/items'))).toBe(true));
    expect(await calls.find((c) => c.url.endsWith('/book-lists/3/items'))!.clone().json()).toEqual({ documentId: 7 });
    await userEvent.click(screen.getByRole('menuitem', { name: '新建书单…' }));
    const dlg = await screen.findByRole('dialog', { name: '新建书单' });
    await userEvent.type(within(dlg).getByRole('textbox', { name: '名称' }), '余华');
    await userEvent.click(within(dlg).getByRole('button', { name: '新建' }));
    await waitFor(() => expect(calls.some((c) => c.url.endsWith('/book-lists/4/items'))).toBe(true));
  });
});


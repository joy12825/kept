import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import ReaderPage from './reader-page';

// epub.js 的替身：记下调用，relocated 由测试触发
const fake = vi.hoisted(() => {
  const handlers: Record<string, (...args: unknown[]) => void> = {};
  const rendition = {
    on: vi.fn((ev: string, fn: (...args: unknown[]) => void) => { handlers[ev] = fn; }),
    display: vi.fn(async () => undefined),
    next: vi.fn(async () => undefined),
    prev: vi.fn(async () => undefined),
    currentLocation: vi.fn(() => ({ start: { cfi: 'epubcfi(/6/4!/4/2/1:0)', href: 'ch1.xhtml' } })),
    themes: { register: vi.fn(), select: vi.fn(), fontSize: vi.fn() },
    annotations: { highlight: vi.fn(), remove: vi.fn() },
    getContents: vi.fn(() => []),
  };
  const book = {
    renderTo: vi.fn(() => rendition),
    loaded: { navigation: Promise.resolve({ toc: [{ label: '静夜思', href: 'ch1.xhtml', subitems: [] }, { label: '春望', href: 'ch2.xhtml', subitems: [{ label: '注释', href: 'ch2.xhtml#n', subitems: [] }] }] }) },
    locations: { generate: vi.fn(() => new Promise(() => {})), length: () => 0, percentageFromCfi: () => 0.5 },
    navigation: { get: (href: string) => ({ label: href === 'ch2.xhtml' ? '春望' : '静夜思' }) },
    destroy: vi.fn(),
  };
  return { handlers, rendition, book, ePub: vi.fn(() => book) };
});
vi.mock('epubjs', () => ({ default: fake.ePub }));

const pdfProps = vi.hoisted(() => ({ current: null as null | { initialPage?: number; onPage?: (n: number, t: number) => void } }));
vi.mock('../../components/pdf-preview', () => ({
  PdfPreview: (p: { initialPage?: number; onPage?: (n: number, t: number) => void }) => { pdfProps.current = p; return <div data-testid="pdf" />; },
}));

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const doc = (extra: Record<string, unknown>) => ({
  id: 7, title: '唐诗选读', createdDate: '2024-03-05', addedAt: '2024-10-01T08:00:00Z', modifiedAt: '2024-10-01T08:00:00Z', mimeType: 'application/epub+zip',
  originalFilename: 'a.epub', pageCount: 0, hasArchive: false, hasThumbnail: true, inbox: false, tagIds: [], noteCount: 0, content: '', checksum: 'ab',
  fields: [], drawerManual: false, isMilestone: false, isBook: true, source: 'web', fieldSources: {}, ...extra,
});

const mark = { id: 31, documentId: 7, locator: 'epubcfi(/6/4!/4/6,/1:0,/1:5)', quote: '感时花溅泪', note: '安史之乱', color: 'pink', createdAt: '2024-10-01T08:00:00Z', updatedAt: '2024-10-01T08:00:00Z' };

function backend(d: ReturnType<typeof doc>, position = '', marks: unknown[] = []) {
  return stubFetch({
    'GET /api/v1/documents/7/highlights': () => jsonResponse(200, marks),
    'POST /api/v1/documents/7/highlights': () => jsonResponse(201, { ...mark, id: 32 }),
    'PATCH /api/v1/highlights/31': () => jsonResponse(200, mark),
    'DELETE /api/v1/highlights/31': () => new Response(null, { status: 204 }),
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'viewer', memberCount: 2 }, csrfToken: 't' }),
    'GET /api/v1/documents/7': () => jsonResponse(200, d),
    'GET /api/v1/documents/7/reading': () => jsonResponse(200, { position, percent: 0.1 }),
    'GET /api/v1/documents/7/original': () => new Response(new ArrayBuffer(8)),
    'PUT /api/v1/documents/7/reading': () => jsonResponse(200, { position: '', percent: 0 }),
  });
}

const render = () => renderWithProviders(<Routes><Route path="/read/:id" element={<ReaderPage />} /><Route path="/docs/:id" element={<div>详情页</div>} /></Routes>, { route: '/read/7' });
const saved = async (calls: Request[]) => Promise.all(calls.filter((c) => c.method === 'PUT').map((c) => c.clone().json()));

beforeEach(() => { vi.clearAllMocks(); try { localStorage.clear(); } catch { /* */ } });
afterEach(() => { vi.unstubAllGlobals(); });

describe('阅读器', () => {
  it('EPUB：回到上次的位置，翻页后保存，离开时立即保存', async () => {
    const calls = backend(doc({}), 'epubcfi(/6/2!/4/2/1:0)');
    const { unmount } = render();
    await waitFor(() => expect(fake.rendition.display).toHaveBeenCalledWith('epubcfi(/6/2!/4/2/1:0)'));
    expect(fake.rendition.on).toHaveBeenCalledWith('relocated', expect.any(Function));
    act(() => fake.handlers.relocated({ start: { cfi: 'epubcfi(/6/4!/4/2/1:0)', percentage: 0.42, href: 'ch2.xhtml' } }));
    expect(await screen.findByText('春望 · 42%')).toBeInTheDocument();
    await userEvent.keyboard('{ArrowRight}');
    expect(fake.rendition.next).toHaveBeenCalled();
    unmount();
    await waitFor(async () => expect(await saved(calls)).toEqual([{ position: 'epubcfi(/6/4!/4/2/1:0)', percent: 0.42 }]));
  });

  it('EPUB：目录、字号、底色', async () => {
    backend(doc({}));
    render();
    await userEvent.click(await screen.findByRole('button', { name: '目录' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: '注释' }));
    expect(fake.rendition.display).toHaveBeenLastCalledWith('ch2.xhtml#n');
    await userEvent.click(screen.getByRole('button', { name: '放大字号' }));
    expect(fake.rendition.themes.fontSize).toHaveBeenLastCalledWith('110%');
    await userEvent.click(screen.getByRole('button', { name: '夜间' }));
    expect(fake.rendition.themes.select).toHaveBeenLastCalledWith('dark');
    expect(localStorage.getItem('kept-reader-theme')).toBe('dark');
  });

  it('PDF：从上次的页开始，显示页码并保存；Esc 返回详情页', async () => {
    const calls = backend(doc({ mimeType: 'application/pdf', hasArchive: true }), 'page:3');
    render();
    await screen.findByTestId('pdf');
    expect(pdfProps.current?.initialPage).toBe(3);
    act(() => pdfProps.current?.onPage?.(5, 10));
    expect(screen.getByText('第 5 / 10 页')).toBeInTheDocument();
    await userEvent.keyboard('{Escape}');
    expect(await screen.findByText('详情页')).toBeInTheDocument();
    await waitFor(async () => expect(await saved(calls)).toEqual([{ position: 'page:5', percent: 0.5 }]));
  });

  it('不能阅读的文档', async () => {
    backend(doc({ mimeType: 'text/plain' }));
    render();
    expect(await screen.findByText('这份文档不能在阅读器中打开')).toBeInTheDocument();
  });

  it('EPUB：画出已有的高亮；选中文字后选颜色就高亮；点高亮可以改颜色、删除；批注面板跳转', async () => {
    const calls = backend(doc({}), '', [mark]);
    render();
    await waitFor(() => expect(fake.rendition.annotations.highlight).toHaveBeenCalledWith(mark.locator, { id: 31 }, expect.any(Function), 'kept-highlight', expect.objectContaining({ fill: expect.any(String) })));
    // 选中
    const contents = { window: { getSelection: () => ({ toString: () => '床前明月光', rangeCount: 1, getRangeAt: () => ({ getBoundingClientRect: () => new DOMRect(10, 10, 50, 20) }), removeAllRanges: vi.fn() }), frameElement: null } };
    act(() => fake.handlers.selected('epubcfi(/6/2!/4/4,/1:0,/1:5)', contents as never));
    await userEvent.click(await screen.findByRole('radio', { name: '绿色高亮' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    expect(await calls.find((c) => c.method === 'POST')!.clone().json()).toEqual({ locator: 'epubcfi(/6/2!/4/4,/1:0,/1:5)', quote: '床前明月光', color: 'green' });
    // 点已有的高亮
    const onClick = fake.rendition.annotations.highlight.mock.calls[0][2] as (e: unknown) => void;
    act(() => onClick({ target: { getBoundingClientRect: () => new DOMRect(100, 100, 40, 20) } }));
    expect(await screen.findByText('安史之乱')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('radio', { name: '蓝色高亮' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    expect(await calls.find((c) => c.method === 'PATCH')!.clone().json()).toEqual({ color: 'blue' });
    await userEvent.click(screen.getByRole('button', { name: '删除高亮' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE')).toBe(true));
    // 面板
    await userEvent.click(screen.getByRole('button', { name: /^批注（/ }));
    await userEvent.click(within(await screen.findByRole('list', { name: '这本书的批注' })).getByText('感时花溅泪'));
    expect(fake.rendition.display).toHaveBeenLastCalledWith(mark.locator);
  });

  it('PDF：给当前页写批注；从“我的批注”打开时跳到那一页', async () => {
    const calls = backend(doc({ mimeType: 'application/pdf', hasArchive: true }), 'page:3');
    renderWithProviders(<Routes><Route path="/read/:id" element={<ReaderPage />} /></Routes>, { route: '/read/7?at=page:8' });
    await screen.findByTestId('pdf');
    expect(pdfProps.current?.initialPage).toBe(8);
    act(() => pdfProps.current?.onPage?.(8, 10));
    await userEvent.click(screen.getByRole('button', { name: '给这一页写批注' }));
    const dlg = await screen.findByRole('dialog', { name: '给第 8 页写批注' });
    await userEvent.type(within(dlg).getByRole('textbox', { name: '批注' }), '这一页的图很清楚');
    await userEvent.click(within(dlg).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    expect(await calls.find((c) => c.method === 'POST')!.clone().json()).toEqual({ locator: 'page:8', note: '这一页的图很清楚' });
  });
});


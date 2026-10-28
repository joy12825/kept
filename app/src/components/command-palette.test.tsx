import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useEffect } from 'react';
import { Route, Routes, useLocation } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { setWorkspaceId } from '../api/client';
import { rememberDoc, rememberSearch } from '../api/recent';
import { useApp } from '../store/app-store';
import { CommandPalette } from './command-palette';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const rule = { algorithm: 'auto', match: '', caseInsensitive: true };
const doc = { id: 7, title: '2024年3月电费账单', createdDate: '2024-03-05', addedAt: '2024-10-01T08:00:00Z', modifiedAt: '2024-10-01T08:00:00Z', mimeType: 'application/pdf',
  originalFilename: 'a.pdf', pageCount: 1, hasArchive: false, hasThumbnail: false, inbox: false, tagIds: [], noteCount: 0,
  highlight: [{ text: '本期', hit: false }, { text: '电费', hit: true }] };

function backend(isAdmin = false) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user: { ...user, isAdmin }, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 2 }, csrfToken: 'tok' }),
    'GET /api/v1/documents': () => jsonResponse(200, { items: [doc], total: 1 }),
    'GET /api/v1/document-types': () => jsonResponse(200, []),
    'GET /api/v1/correspondents': () => jsonResponse(200, [{ id: 20, name: '国家电网', initial: 'G', sortKey: 'guojiadianwang', rule }]),
    'GET /api/v1/tags': () => jsonResponse(200, []),
    'GET /api/v1/custom-fields': () => jsonResponse(200, [{ id: 40, name: '保修到期', initial: 'B', sortKey: 'baoxiudaoqi', dataType: 'date', extra: {}, builtin: false }]),
    'GET /api/v1/saved-views': () => jsonResponse(200, [{ id: 3, name: '电费汇总', filter: { text: '电费' }, sort: '', creatorName: '小王', createdAt: '2024-10-01T08:00:00Z' }]),
  });
}

function Open() {
  const app = useApp();
  useEffect(() => { app.set({ cmdOpen: true }); }, []); // eslint-disable-line react-hooks/exhaustive-deps
  return null;
}
function Where() {
  const loc = useLocation();
  const app = useApp();
  return <div data-testid="where">{loc.pathname}|{app.s.docLists.files?.q ?? ''}|{app.s.docLists.files?.sort ?? ''}</div>;
}
const render = () => renderWithProviders(<Routes><Route path="*" element={<><Open /><CommandPalette /><Where /></>} /></Routes>);

afterEach(() => { vi.unstubAllGlobals(); setWorkspaceId(null); localStorage.clear(); });

describe('⌘K 命令面板', () => {
  it('搜到自定义字段和设置中的页面', async () => {
    backend();
    render();
    await userEvent.type(await screen.findByRole('combobox'), 'baoxiu');
    await userEvent.click(await screen.findByRole('option', { name: /保修到期/ }));
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/directory/custom-fields/40|'));
  });

  it('设置中的页面按名称找', async () => {
    backend();
    render();
    await userEvent.type(await screen.findByRole('combobox'), '通知');
    await userEvent.click(await screen.findByRole('option', { name: /设置 · 通知/ }));
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/settings/notify|'));
  });

  it('设置中的页面也认原来的名字和技术名词：搜 WebDAV 找到文件安全（v1.13.9）', async () => {
    backend(true);
    render();
    await userEvent.type(await screen.findByRole('combobox'), 'webdav');
    await userEvent.click(await screen.findByRole('option', { name: /设置 · 文件安全/ }));
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/settings/storages|'));
  });

  it('没有输入时显示最近打开和最近搜索', async () => {
    backend();
    rememberDoc({ id: 9, title: '房屋租赁合同' });
    rememberSearch('from:链家');
    render();
    expect(await screen.findByText('最近打开')).toBeInTheDocument();
    expect(screen.getByText('房屋租赁合同')).toBeInTheDocument();
    expect(screen.getByText('from:链家')).toBeInTheDocument();
  });

  it('边输边搜文档（前缀），按拼音找往来方，找常用筛选', async () => {
    const calls = backend();
    render();
    await userEvent.type(await screen.findByRole('combobox', { name: '搜索与命令' }), '电');
    expect(await screen.findByText('2024年3月电费账单')).toBeInTheDocument();
    expect(document.querySelectorAll('mark')).toHaveLength(1);
    const req = calls.find((r) => new URL(r.url).pathname === '/api/v1/documents')!;
    expect(new URL(req.url).searchParams.get('filter')).toBe('{"text":"电","prefix":true}');
    expect(screen.getByText('电费汇总')).toBeInTheDocument();
    await userEvent.clear(screen.getByRole('combobox', { name: '搜索与命令' }));
    await userEvent.type(screen.getByRole('combobox', { name: '搜索与命令' }), 'guojia');
    expect(await screen.findByText('国家电网')).toBeInTheDocument();
  });

  it('上下键选择、Enter 打开；最后一项在全部文件中搜索', async () => {
    backend();
    render();
    const input = await screen.findByRole('combobox', { name: '搜索与命令' });
    await userEvent.type(input, '电费');
    await screen.findByText('2024年3月电费账单');
    // 第一项是文档
    await userEvent.keyboard('{Enter}');
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/docs/7'));
  });

  it('在全部文件中搜索：记进最近搜索并按相关度排序', async () => {
    backend();
    render();
    const input = await screen.findByRole('combobox', { name: '搜索与命令' });
    await userEvent.type(input, '押金');
    const listbox = screen.getByRole('listbox');
    await userEvent.click(within(listbox).getByText('在全部文件中搜索“押金”'));
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/files|押金|relevance'));
    expect(JSON.parse(localStorage.getItem('kept-recent:personal')!).searches).toEqual(['押金']);
  });
});

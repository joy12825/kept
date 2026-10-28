import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setWorkspaceId } from '../../api/client';
import DirectoryPage from './directory-page';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const ws = (role = 'editor') => ({ id: 5, name: '王家', kind: 'family', color: 'green', role, memberCount: 2 });
const rule = { algorithm: 'auto', match: '', caseInsensitive: true };
const locations = [
  { id: 70, name: '文件盒 A', initial: 'W', note: '书房柜子第 2 层', documentCount: 47 },
  { id: 71, name: '银行保险箱', initial: 'Y', note: '', documentCount: 0 },
];

function backend(role = 'editor', extra: Parameters<typeof stubFetch>[0] = {}) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws(role), csrfToken: 'tok' }),
    'GET /api/v1/persons': () => jsonResponse(200, [{ id: 60, name: '爸爸', initial: 'B', sortKey: 'baba', rule, documentCount: 5, lastDocumentDate: '2024-04-12' }]),
    'GET /api/v1/locations': () => jsonResponse(200, locations),
    'GET /api/v1/tags': () => jsonResponse(200, []),
    ...extra,
  });
}

afterEach(() => { vi.unstubAllGlobals(); setWorkspaceId(null); });

const render = (route: string) => renderWithProviders(<Routes><Route path="/directory" element={<DirectoryPage />} /><Route path="/files" element={<p>文件列表</p>} /></Routes>, { route });

describe('目录 → 家人、存放位置（v1.6.3）', () => {
  it('家人和往来方一样按首字母列出', async () => {
    backend();
    render('/directory?tab=persons');
    expect(await screen.findByText('爸爸')).toBeInTheDocument();
    expect(screen.getByText('5 份 · 最近 2024-04-12 · 匹配：自动学习')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '新建家人' })).toBeInTheDocument();
  });

  it('存放位置：列出说明和文档数，按原件编号放入，新建', async () => {
    const calls = backend('editor', {
      'POST /api/v1/locations/assign-asn': () => jsonResponse(200, { updated: 12 }),
      'POST /api/v1/locations': async (r) => jsonResponse(201, { id: 72, initial: 'S', documentCount: 0, ...(await r.clone().json()) }),
    });
    render('/directory?tab=locations');
    expect(await screen.findByText('47 份文档 · 书房柜子第 2 层')).toBeInTheDocument();
    expect(screen.getByText('还没有文档')).toBeInTheDocument();
    await userEvent.click(screen.getAllByRole('button', { name: '按原件编号放入' })[0]);
    const dlg = await screen.findByRole('dialog', { name: '放入“文件盒 A”' });
    await userEvent.type(within(dlg).getByRole('textbox', { name: '从' }), '1');
    await userEvent.type(within(dlg).getByRole('textbox', { name: '到' }), '12');
    await userEvent.click(within(dlg).getByRole('button', { name: '放入' }));
    await waitFor(() => expect(calls.some((r) => r.url.endsWith('/assign-asn'))).toBe(true));
    expect(await calls.find((r) => r.url.endsWith('/assign-asn'))!.clone().json()).toEqual({ locationId: 70, from: 1, to: 12 });
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '放入“文件盒 A”' })).not.toBeInTheDocument());

    await userEvent.click(screen.getByRole('button', { name: '新建存放位置' }));
    const create = await screen.findByRole('dialog', { name: '新建存放位置' });
    await userEvent.type(within(create).getByRole('textbox', { name: '名称' }), '书柜');
    await userEvent.type(within(create).getByRole('textbox', { name: '说明' }), '客厅');
    await userEvent.click(within(create).getByRole('button', { name: '新建' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/api/v1/locations'))).toBe(true));
    expect(await calls.find((r) => r.method === 'POST' && r.url.endsWith('/api/v1/locations'))!.clone().json()).toEqual({ name: '书柜', note: '客厅' });
  });

  it('查看文档：打开文件列表并按位置筛选；只看的成员不能改', async () => {
    backend('viewer');
    render('/directory?tab=locations');
    await screen.findByText('文件盒 A');
    expect(screen.queryByRole('button', { name: '按原件编号放入' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '新建存放位置' })).not.toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: '查看文档' })[1]).toBeDisabled();
    await userEvent.click(screen.getAllByRole('button', { name: '查看文档' })[0]);
    expect(await screen.findByText('文件列表')).toBeInTheDocument();
  });
});

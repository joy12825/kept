import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Link, Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { setCsrfToken, setWorkspaceId, type Workspace, type WorkspaceRole } from '../api/client';
import Layout from './layout';

const user = { id: 1, username: 'wang', displayName: '小王', email: 'wang@example.com', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const home = (role: WorkspaceRole): Workspace => ({ id: 5, name: '王家', kind: 'family', color: 'green', role, memberCount: 3 });

function renderLayout(role: WorkspaceRole) {
  stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: home(role), csrfToken: 'tok-1' }),
    'GET /api/v1/workspaces': () => jsonResponse(200, [home(role)]),
  });
  return renderWithProviders(<Routes><Route element={<Layout />}><Route index element={<div>页面内容</div>} /></Route></Routes>);
}

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

describe('Layout 上传入口', () => {
  it('成员可以上传', async () => {
    renderLayout('editor');
    const upload = await screen.findByRole('button', { name: '上传' });
    expect(upload).toBeEnabled();
    await userEvent.click(upload);
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
  });

  it('只看的成员不能上传：按钮不可用，快捷键也打不开上传对话框', async () => {
    renderLayout('viewer');
    await screen.findByText('页面内容');
    await waitFor(() => expect(screen.getByRole('button', { name: '上传' })).toBeDisabled());
    await userEvent.keyboard('u');
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(await screen.findByText('只看的成员不能上传')).toBeInTheDocument();
  });
});

describe('常用筛选的顺序（v1.6.5）', () => {
  it('菜单里下移：保存新的顺序，侧栏马上换过来', async () => {
    const view = (id: number, name: string) => ({ id, name, filter: { text: name }, sort: '', createdBy: 1, creatorName: '小王', createdAt: '2024-03-05T10:00:00Z' });
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: home('owner'), csrfToken: 'tok-1' }),
      'GET /api/v1/workspaces': () => jsonResponse(200, [home('owner')]),
      'GET /api/v1/saved-views': () => jsonResponse(200, [view(1, '保单'), view(2, '电费'), view(3, '账单')]),
      'PUT /api/v1/saved-views/order': () => new Response(null, { status: 204 }),
    });
    renderWithProviders(<Routes><Route element={<Layout />}><Route index element={<div>页面内容</div>} /></Route></Routes>);
    await screen.findByText('页面内容');
    const menu = screen.queryByRole('button', { name: '打开菜单' });
    if (menu) await userEvent.click(menu);
    await userEvent.click(await screen.findByRole('button', { name: '“保单”的更多操作' }));
    expect(screen.queryByRole('menuitem', { name: '上移' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('menuitem', { name: '下移' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'PUT')).toBe(true));
    expect(await calls.find((r) => r.method === 'PUT')!.clone().json()).toEqual({ ids: [2, 1, 3] });
  });
});

describe('从列表进详情页（v1.9.6）', () => {
  it('点开一份文档不白屏：页头的 hook 每次都一样多', async () => {
    stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: home('editor'), csrfToken: 'tok-1' }),
      'GET /api/v1/workspaces': () => jsonResponse(200, [home('editor')]),
    });
    renderWithProviders(
      <Routes><Route element={<Layout />}>
        <Route path="files" element={<Link to="/docs/7">说明</Link>} />
        <Route path="docs/:id" element={<div>文档详情</div>} />
      </Route></Routes>,
      { route: '/files' },
    );
    await userEvent.click(await screen.findByRole('link', { name: '说明' }));
    expect(await screen.findByText('文档详情')).toBeInTheDocument();
  });
});

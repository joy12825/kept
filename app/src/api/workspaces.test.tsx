import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { jsonResponse, newQueryClient, stubFetch } from '../test/utils';
import { getWorkspaceId, setCsrfToken, setWorkspaceId, type Workspace } from './client';
import { meKey, useLogout, useMe } from './session';
import { useCreateWorkspace, useRole, useSwitchWorkspace, useWorkspaces, workspacesKey } from './workspaces';

const personal: Workspace = { id: 1, name: '小王 的空间', kind: 'personal', color: 'blue', role: 'owner', memberCount: 1 };
const home: Workspace = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'viewer', memberCount: 3 };
const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };

// /me 按请求头返回对应的空间，和真实后端一致
const meRoute = (req: Request) =>
  jsonResponse(200, { user, workspace: req.headers.get('X-Workspace-ID') === '5' ? home : personal, csrfToken: 'tok-1' });

function setup() {
  const client = newQueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  return { client, wrapper };
}

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

describe('useSwitchWorkspace', () => {
  it('切换后重新取当前用户，并清除旧空间的数据缓存', async () => {
    stubFetch({ 'GET /api/v1/me': meRoute, 'GET /api/v1/workspaces': () => jsonResponse(200, [personal, home]) });
    const { client, wrapper } = setup();
    const { result } = renderHook(() => ({ me: useMe(), list: useWorkspaces(), sw: useSwitchWorkspace(), role: useRole() }), { wrapper });
    await waitFor(() => expect(result.current.me.data?.workspace.id).toBe(1));
    await waitFor(() => expect(result.current.list.data).toHaveLength(2));
    expect(result.current.role).toEqual({ role: 'owner', canWrite: true, isOwner: true });
    client.setQueryData(['documents', 'list'], ['个人空间里的文档']);

    await act(() => result.current.sw(home));
    expect(getWorkspaceId()).toBe(5);
    await waitFor(() => expect(result.current.me.data?.workspace.id).toBe(5));
    expect(result.current.role).toEqual({ role: 'viewer', canWrite: false, isOwner: false });
    expect(client.getQueryData(['documents', 'list'])).toBeUndefined();
    // 空间列表属于账号，不随切换丢弃
    expect(client.getQueryData(workspacesKey)).toHaveLength(2);

    // 切回个人空间：不再带请求头
    await act(() => result.current.sw(personal));
    expect(getWorkspaceId()).toBeNull();
    await waitFor(() => expect(result.current.me.data?.workspace.id).toBe(1));
  });
});

describe('useCreateWorkspace', () => {
  it('提交名称、类型和颜色，成功后刷新空间列表', async () => {
    let created = false;
    const calls = stubFetch({
      'GET /api/v1/workspaces': () => jsonResponse(200, created ? [personal, home] : [personal]),
      'POST /api/v1/workspaces': () => { created = true; return jsonResponse(201, { ...home, role: 'owner', memberCount: 1 }); },
    });
    const { wrapper } = setup();
    const { result } = renderHook(() => ({ list: useWorkspaces(), create: useCreateWorkspace() }), { wrapper });
    await waitFor(() => expect(result.current.list.data).toHaveLength(1));
    await act(() => result.current.create.mutateAsync({ name: '王家', kind: 'family', color: 'green' }));
    const req = calls.find((r) => r.method === 'POST')!;
    expect(await req.clone().json()).toEqual({ name: '王家', kind: 'family', color: 'green' });
    await waitFor(() => expect(result.current.list.data).toHaveLength(2));
  });
});

describe('退出登录', () => {
  it('清除保存的空间', async () => {
    stubFetch({ 'GET /api/v1/me': meRoute, 'POST /api/v1/auth/logout': () => new Response(null, { status: 204 }) });
    const { client, wrapper } = setup();
    setWorkspaceId(5);
    const { result } = renderHook(() => ({ me: useMe(), logout: useLogout() }), { wrapper });
    await waitFor(() => expect(result.current.me.data?.workspace.id).toBe(5));
    await act(() => result.current.logout.mutateAsync());
    expect(getWorkspaceId()).toBeNull();
    expect(client.getQueryData(meKey)).toBeNull();
  });
});

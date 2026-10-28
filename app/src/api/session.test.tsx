import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, screen, waitFor } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { jsonResponse, newQueryClient, renderWithProviders, stubFetch } from '../test/utils';
import { setCsrfToken } from './client';
import type { SetupStatus } from './client';
import { meKey, setupKey, useLogin, useLoginTwoFactor, useLogout, useMe, useSetup } from './session';
import { AuthGate } from '../auth/auth-gate';
import { api } from './client';

const me = {
  user: { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z' },
  workspace: { id: 1, name: '小王 的空间', kind: 'personal', role: 'owner' },
  csrfToken: 'tok-1',
};

function setup() {
  const client = newQueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  return { client, wrapper };
}

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); });

describe('useMe', () => {
  it('未登录时返回 null 而不是错误', async () => {
    stubFetch({ 'GET /api/v1/me': () => jsonResponse(401, { code: 'unauthorized', message: '请先登录' }) });
    const { wrapper } = setup();
    const { result } = renderHook(() => useMe(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toBeNull();
  });

  it('已登录时返回当前用户，并记住 CSRF 令牌', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, me),
      'POST /api/v1/auth/logout': () => new Response(null, { status: 204 }),
    });
    const { wrapper } = setup();
    const { result } = renderHook(() => ({ me: useMe(), logout: useLogout() }), { wrapper });
    await waitFor(() => expect(result.current.me.data?.user.username).toBe('wang'));
    await act(() => result.current.logout.mutateAsync());
    const logoutReq = calls.find((r) => r.method === 'POST')!;
    expect(logoutReq.headers.get('X-CSRF-Token')).toBe('tok-1');
  });
});

describe('useLogin', () => {
  it('登录成功后写入当前用户缓存', async () => {
    stubFetch({ 'POST /api/v1/auth/login': () => jsonResponse(200, me) });
    const { client, wrapper } = setup();
    const { result } = renderHook(() => useLogin(), { wrapper });
    const outcome = await act(() => result.current.mutateAsync({ login: 'wang', password: 'password-123', remember: true }));
    expect(outcome).toEqual({ kind: 'signedIn', me });
    expect(client.getQueryData(meKey)).toEqual(me);
  });

  it('返回两步验证挑战时不重置用户态', async () => {
    stubFetch({ 'POST /api/v1/auth/login': () => jsonResponse(202, { challenge: 'chal-1', expiresAt: '2024-03-05T10:05:00Z' }) });
    const { client, wrapper } = setup();
    const { result } = renderHook(() => useLogin(), { wrapper });
    const outcome = await act(() => result.current.mutateAsync({ login: 'wang', password: 'password-123', remember: true }));
    expect(outcome).toEqual({ kind: 'challenge', challenge: 'chal-1' });
    expect(client.getQueryData(meKey)).toBeUndefined();
  });

  it('失败时给出服务端的中文说明', async () => {
    stubFetch({ 'POST /api/v1/auth/login': () => jsonResponse(401, { code: 'unauthorized', message: '用户名或密码不正确' }) });
    const { wrapper } = setup();
    const { result } = renderHook(() => useLogin(), { wrapper });
    await act(async () => { await result.current.mutateAsync({ login: 'wang', password: 'x', remember: true }).catch(() => {}); });
    await waitFor(() => expect(result.current.error?.message).toBe('用户名或密码不正确'));
  });

  it('登录成功后清除上一个用户遗留的缓存（F2）', async () => {
    stubFetch({ 'POST /api/v1/auth/login': () => jsonResponse(200, me) });
    const { client, wrapper } = setup();
    client.setQueryData(['tokens'], [{ id: 1, name: '旧 token' }]);
    const { result } = renderHook(() => useLogin(), { wrapper });
    await act(() => result.current.mutateAsync({ login: 'wang', password: 'password-123', remember: true }));
    expect(client.getQueryData(['tokens'])).toBeUndefined();
  });
});

describe('useLoginTwoFactor', () => {
  it('验证成功后写入当前用户缓存', async () => {
    stubFetch({ 'POST /api/v1/auth/login/2fa': () => jsonResponse(200, me) });
    const { client, wrapper } = setup();
    const { result } = renderHook(() => useLoginTwoFactor(), { wrapper });
    await act(() => result.current.mutateAsync({ challenge: 'chal-1', code: '123456' }));
    expect(client.getQueryData(meKey)).toEqual(me);
  });
});

describe('401 回调', () => {
  it('会话过期时清除除 me/setup 之外的缓存（F2）', async () => {
    let expired = false;
    stubFetch({
      'GET /api/v1/setup': () => jsonResponse(200, { needsSetup: false }),
      'GET /api/v1/me': () => (expired ? jsonResponse(401, { code: 'unauthorized', message: '请先登录' }) : jsonResponse(200, me)),
      'GET /api/v1/admin/users': () => jsonResponse(401, { code: 'unauthorized', message: '请先登录' }),
    });
    const client = newQueryClient();
    renderWithProviders(<AuthGate><div>应用本体</div></AuthGate>, { client });
    await screen.findByText('应用本体');
    client.setQueryData(['tokens'], [{ id: 1, name: '旧 token' }]);
    expired = true;
    // 模拟任意接口返回 401（不是 GET /api/v1/me 本身）触发全局回调。
    await act(async () => { await api.GET('/api/v1/admin/users'); });
    await waitFor(() => expect(client.getQueryData(['tokens'])).toBeUndefined());
  });
});

describe('useLogout', () => {
  it('退出后把当前用户置为 null', async () => {
    stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, me),
      'POST /api/v1/auth/logout': () => new Response(null, { status: 204 }),
    });
    const { client, wrapper } = setup();
    const { result } = renderHook(() => ({ me: useMe(), logout: useLogout() }), { wrapper });
    await waitFor(() => expect(result.current.me.data).toBeTruthy());
    await act(() => result.current.logout.mutateAsync());
    expect(client.getQueryData(meKey)).toBeNull();
  });
});

describe('useSetup', () => {
  it('初始化成功后把 setup 缓存标记为不再需要初始化，而不是重新请求（裁决 R2）', async () => {
    stubFetch({ 'POST /api/v1/setup': () => jsonResponse(200, me) });
    const { client, wrapper } = setup();
    client.setQueryData<SetupStatus>(setupKey, { needsSetup: true });
    const { result } = renderHook(() => useSetup(), { wrapper });
    await act(() => result.current.mutateAsync({ email: 'wang@example.com', username: 'wang', password: 'password-123', displayName: '小王' }));
    expect(client.getQueryData<SetupStatus>(setupKey)?.needsSetup).toBe(false);
  });
});

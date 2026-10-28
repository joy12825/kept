import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, getWorkspaceId, makeClient, setCsrfToken, setUnauthorizedHandler, setWorkspaceGoneHandler, setWorkspaceId, unwrap } from './client';

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });

afterEach(() => {
  setCsrfToken(null);
  setUnauthorizedHandler(() => {});
  setWorkspaceGoneHandler(() => {});
  setWorkspaceId(null);
});

describe('api client', () => {
  it('只在修改类请求上附带 X-CSRF-Token', async () => {
    const seen: Request[] = [];
    const c = makeClient(async (req) => { seen.push(req); return json(200, {}); });
    setCsrfToken('abc');
    await c.GET('/api/v1/me');
    await c.POST('/api/v1/auth/logout');
    expect(seen[0].headers.get('X-CSRF-Token')).toBeNull();
    expect(seen[1].headers.get('X-CSRF-Token')).toBe('abc');
  });

  it('没有令牌时不附带请求头', async () => {
    const seen: Request[] = [];
    const c = makeClient(async (req) => { seen.push(req); return json(200, {}); });
    await c.POST('/api/v1/auth/logout');
    expect(seen[0].headers.has('X-CSRF-Token')).toBe(false);
  });

  it('unwrap 把错误响应转成带中文说明的 ApiError', async () => {
    const c = makeClient(async () => json(409, { code: 'conflict', message: '这个用户名已经被使用' }));
    const r = await c.POST('/api/v1/admin/users', { body: { email: 'wang@example.com', username: 'wang', password: 'password-123' } });
    expect(() => unwrap(r)).toThrowError(ApiError);
    try { unwrap(r); } catch (e) {
      expect((e as ApiError).status).toBe(409);
      expect((e as ApiError).code).toBe('conflict');
      expect((e as ApiError).message).toBe('这个用户名已经被使用');
    }
  });

  it('任何请求返回 401 时调用未登录回调', async () => {
    const onUnauthorized = vi.fn();
    setUnauthorizedHandler(onUnauthorized);
    const c = makeClient(async () => json(401, { code: 'unauthorized', message: '请先登录' }));
    await c.GET('/api/v1/me/tokens');
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it('保存了当前空间时，每个请求都带 X-Workspace-ID；没有保存时不带', async () => {
    const seen: Request[] = [];
    const c = makeClient(async (req) => { seen.push(req); return json(200, {}); });
    await c.GET('/api/v1/me');
    setWorkspaceId(42);
    await c.GET('/api/v1/me');
    await c.POST('/api/v1/auth/logout');
    expect(seen[0].headers.has('X-Workspace-ID')).toBe(false);
    expect(seen[1].headers.get('X-Workspace-ID')).toBe('42');
    expect(seen[2].headers.get('X-Workspace-ID')).toBe('42');
    expect(window.localStorage.getItem('kept-workspace')).toBe('42');
  });

  it('收到 workspace_not_found 时清除保存的空间并通知一次；其他 404 不受影响', async () => {
    const gone = vi.fn();
    setWorkspaceGoneHandler(gone);
    setWorkspaceId(42);
    const notFound = makeClient(async () => json(404, { code: 'not_found', message: '找不到这个空间' }));
    await notFound.GET('/api/v1/workspaces');
    expect(getWorkspaceId()).toBe(42);
    expect(gone).not.toHaveBeenCalled();

    const stale = makeClient(async () => json(404, { code: 'workspace_not_found', message: '找不到这个空间，或你已不是它的成员' }));
    // 两个并发请求都带着失效的空间：只清除、通知一次
    await Promise.all([stale.GET('/api/v1/me'), stale.GET('/api/v1/workspaces')]);
    expect(getWorkspaceId()).toBeNull();
    expect(window.localStorage.getItem('kept-workspace')).toBeNull();
    expect(gone).toHaveBeenCalledTimes(1);
  });

  it('localStorage 不可用时退回内存，不影响使用', async () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    try {
      setWorkspaceId(7);
      expect(getWorkspaceId()).toBe(7);
    } finally {
      spy.mockRestore();
    }
  });
});

import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useLocation } from 'react-router-dom';
import { jsonResponse, newQueryClient, renderWithProviders, stubFetch } from '../test/utils';
import { AuthGate } from './auth-gate';
import { useLogout } from '../api/session';
import { api } from '../api/client';

const me = {
  user: { id: 1, username: 'admin', displayName: '管理员', isAdmin: true, disabled: false, createdAt: '2024-03-05T10:00:00Z' },
  workspace: { id: 1, name: '管理员 的空间', kind: 'personal', role: 'owner' },
  csrfToken: 't',
};
const unauthorized = () => jsonResponse(401, { code: 'unauthorized', message: '请先登录' });

afterEach(() => vi.unstubAllGlobals());

describe('AuthGate', () => {
  it('没有任何账号时显示创建管理员表单，提交后进入应用', async () => {
    let created = false;
    const calls = stubFetch({
      'GET /api/v1/setup': () => jsonResponse(200, { needsSetup: !created }),
      'GET /api/v1/me': () => (created ? jsonResponse(200, me) : unauthorized()),
      'POST /api/v1/setup': () => { created = true; return jsonResponse(200, me); },
    });
    renderWithProviders(<AuthGate><div>应用本体</div></AuthGate>);
    expect(await screen.findByText('创建管理员账号')).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('名字'), '管理员');
    await userEvent.type(screen.getByLabelText('邮箱'), 'admin@example.com');
    await userEvent.type(screen.getByLabelText('密码'), 'abcdefg1');
    await userEvent.type(screen.getByLabelText('确认密码'), 'abcdefg1');
    await userEvent.click(screen.getByRole('button', { name: '创建并进入' }));
    expect(await screen.findByText('应用本体')).toBeInTheDocument();
    const req = calls.find((r) => r.method === 'POST' && new URL(r.url).pathname === '/api/v1/setup')!;
    expect(await req.clone().json()).toEqual({ displayName: '管理员', email: 'admin@example.com', password: 'abcdefg1' });
  });

  it('未登录时显示登录表单，并显示服务端的错误说明', async () => {
    stubFetch({
      'GET /api/v1/setup': () => jsonResponse(200, { needsSetup: false }),
      'GET /api/v1/me': unauthorized,
      'GET /healthz': () => jsonResponse(200, { status: 'ok', checks: {} }),
      'GET /api/v1/system/version': () => jsonResponse(200, { version: '0.1.0', commit: 'x', buildTime: 't' }),
      'POST /api/v1/auth/login': () => jsonResponse(401, { code: 'unauthorized', message: '用户名或密码不正确' }),
    });
    renderWithProviders(<AuthGate><div>应用本体</div></AuthGate>);
    await userEvent.type(await screen.findByLabelText('用户名或邮箱'), 'admin');
    await userEvent.type(screen.getByLabelText('密码'), 'wrong-password');
    await userEvent.click(screen.getByRole('button', { name: '登录' }));
    expect(await screen.findByText('用户名或密码不正确')).toBeInTheDocument();
    expect(screen.queryByText('注册账号')).not.toBeInTheDocument();
  });

  it('未登录时的 401（登录页密码错误）不清空缓存：[\'healthz\'] 查询仍在', async () => {
    const calls = stubFetch({
      'GET /api/v1/setup': () => jsonResponse(200, { needsSetup: false }),
      'GET /api/v1/me': unauthorized,
      'GET /healthz': () => jsonResponse(200, { status: 'ok', checks: {} }),
      'GET /api/v1/system/version': () => jsonResponse(200, { version: '0.1.0', commit: 'x', buildTime: 't' }),
      'POST /api/v1/auth/login': () => jsonResponse(401, { code: 'unauthorized', message: '用户名或密码不正确', details: { remainingAttempts: 4 } }),
    });
    const client = newQueryClient();
    renderWithProviders(<AuthGate><div>应用本体</div></AuthGate>, { client });
    await userEvent.type(await screen.findByLabelText('用户名或邮箱'), 'admin');
    await waitFor(() => expect(client.getQueryData(['healthz'])).toBe(true));
    await userEvent.type(screen.getByLabelText('密码'), 'wrong-password');
    await userEvent.click(screen.getByRole('button', { name: '登录' }));
    expect(await screen.findByText('用户名或密码不正确。再错 4 次，账号会被锁定 15 分钟。')).toBeInTheDocument();
    // 缓存没被清空：健康检查仍是最初那一次，没有因为查询被移除而重新请求。
    const healthzQuery = client.getQueryCache().find({ queryKey: ['healthz'] });
    expect(healthzQuery?.state.data).toBe(true);
    expect(healthzQuery?.state.dataUpdateCount).toBe(1);
    expect(calls.filter((r) => new URL(r.url).pathname === '/healthz')).toHaveLength(1);
  });

  it('已登录时直接显示应用本体', async () => {
    stubFetch({
      'GET /api/v1/setup': () => jsonResponse(200, { needsSetup: false }),
      'GET /api/v1/me': () => jsonResponse(200, me),
    });
    renderWithProviders(<AuthGate><div>应用本体</div></AuthGate>);
    await waitFor(() => expect(screen.getByText('应用本体')).toBeInTheDocument());
  });

  it('点击退出登录后回到登录页（F1）', async () => {
    let loggedOut = false;
    stubFetch({
      'GET /api/v1/setup': () => jsonResponse(200, { needsSetup: false }),
      'GET /api/v1/me': () => (loggedOut ? unauthorized() : jsonResponse(200, me)),
      'POST /api/v1/auth/logout': () => { loggedOut = true; return new Response(null, { status: 204 }); },
    });
    function LogoutButton() {
      const logout = useLogout();
      return <button onClick={() => logout.mutate()}>退出登录</button>;
    }
    renderWithProviders(<AuthGate><LogoutButton /></AuthGate>);
    await userEvent.click(await screen.findByRole('button', { name: '退出登录' }));
    expect(await screen.findByRole('heading', { name: '登录' })).toBeInTheDocument();
  });

  it('会话在使用中过期：提示登录已过期并预填用户名，再次登录后回到原网址', async () => {
    let loggedIn = true;
    function FilesPage() {
      const loc = useLocation();
      return (
        <div>
          <div>文件页：{loc.pathname}</div>
          <button onClick={() => { void api.GET('/api/v1/system/version'); }}>触发请求</button>
        </div>
      );
    }
    stubFetch({
      'GET /api/v1/setup': () => jsonResponse(200, { needsSetup: false }),
      'GET /api/v1/me': () => (loggedIn ? jsonResponse(200, me) : unauthorized()),
      'GET /healthz': () => jsonResponse(200, { status: 'ok', checks: {} }),
      'GET /api/v1/system/version': () => { loggedIn = false; return unauthorized(); },
      'POST /api/v1/auth/login': () => { loggedIn = true; return jsonResponse(200, me); },
    });
    renderWithProviders(<AuthGate><FilesPage /></AuthGate>, { route: '/files' });
    expect(await screen.findByText('文件页：/files')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: '触发请求' }));

    expect(await screen.findByText('登录已过期，请重新登录。你刚才的页面会在登录后恢复。')).toBeInTheDocument();
    const loginField = await screen.findByLabelText<HTMLInputElement>('用户名或邮箱');
    expect(loginField.value).toBe('admin');
    expect(screen.getByLabelText('密码')).toHaveFocus();

    await userEvent.type(screen.getByLabelText('密码'), 'password-123');
    await userEvent.click(screen.getByRole('button', { name: '登录' }));

    expect(await screen.findByText('文件页：/files')).toBeInTheDocument();
  });
});

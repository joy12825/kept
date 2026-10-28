import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { PasswordForm, ProfileForm } from './account';
import { TokensPanel } from './tokens';
import { AccountsPanel } from './accounts';
import { Toast } from '../../components/overlays';

const me = {
  user: { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z' },
  workspace: { id: 1, name: '小王 的空间', kind: 'personal', role: 'owner' },
  csrfToken: 't',
};

afterEach(() => vi.unstubAllGlobals());

describe('ProfileForm', () => {
  it('以当前资料预填，保存时提交修改', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { ...me, user: { ...me.user, email: 'wang@example.com' } }),
      'PATCH /api/v1/me': () => jsonResponse(200, { ...me, user: { ...me.user, displayName: '王小明', email: 'wang@example.com' } }),
    });
    renderWithProviders(<ProfileForm />);
    const name = await screen.findByLabelText(/^显示名称/);
    await waitFor(() => expect(name).toHaveValue('小王'));
    await userEvent.clear(name);
    await userEvent.type(name, '王小明');
    await userEvent.click(screen.getByRole('button', { name: '保存修改' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'PATCH')).toBe(true));
    const body = await calls.find((r) => r.method === 'PATCH')!.clone().json();
    expect(body.displayName).toBe('王小明');
  });

  it('清空邮箱后“保存”不可用', async () => {
    stubFetch({ 'GET /api/v1/me': () => jsonResponse(200, { ...me, user: { ...me.user, email: 'wang@example.com' } }) });
    renderWithProviders(<ProfileForm />);
    const email = await screen.findByLabelText(/^邮箱/);
    await waitFor(() => expect(email).toHaveValue('wang@example.com'));
    expect(screen.getByRole('button', { name: '保存修改' })).toBeEnabled();
    await userEvent.clear(email);
    expect(screen.getByRole('button', { name: '保存修改' })).toBeDisabled();
    await userEvent.type(email, '不是邮箱');
    expect(screen.getByRole('button', { name: '保存修改' })).toBeDisabled();
  });
});

describe('PasswordForm', () => {
  it('两次输入不一致时不提交', async () => {
    const calls = stubFetch({});
    renderWithProviders(<PasswordForm />);
    await userEvent.type(screen.getByLabelText(/^当前密码/), 'password-123');
    await userEvent.type(screen.getByLabelText(/^新密码/), 'new-password-1');
    await userEvent.type(screen.getByLabelText(/^再输入一次新密码/), 'new-password-2');
    await userEvent.click(screen.getByRole('button', { name: '修改密码' }));
    expect(screen.getByText('两次输入的新密码不一致')).toBeInTheDocument();
    expect(calls).toHaveLength(0);
  });

  it('显示服务端的错误说明', async () => {
    stubFetch({ 'POST /api/v1/me/password': () => jsonResponse(400, { code: 'invalid_request', message: '当前密码不正确' }) });
    renderWithProviders(<PasswordForm />);
    await userEvent.type(screen.getByLabelText(/^当前密码/), 'bad-password');
    await userEvent.type(screen.getByLabelText(/^新密码/), 'new-password-1');
    await userEvent.type(screen.getByLabelText(/^再输入一次新密码/), 'new-password-1');
    await userEvent.click(screen.getByRole('button', { name: '修改密码' }));
    expect(await screen.findByText('当前密码不正确')).toBeInTheDocument();
  });
});

describe('TokensPanel', () => {
  it('新建后只显示一次完整 Token，列表中只显示前缀', async () => {
    let tokens: unknown[] = [];
    stubFetch({
      'GET /api/v1/me/tokens': () => jsonResponse(200, tokens),
      'POST /api/v1/me/tokens': () => {
        const t = { id: 7, name: '扫描仪', prefix: 'kept_abc1234', createdAt: '2024-03-05T10:00:00Z' };
        tokens = [t];
        return jsonResponse(201, { token: 'kept_abc1234secretsecretsecret', apiToken: t });
      },
    });
    renderWithProviders(<TokensPanel />);
    expect(await screen.findByText('还没有 API Token')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '新建 Token' }));
    await userEvent.type(screen.getByLabelText(/^名称/), '扫描仪');
    await userEvent.click(screen.getByRole('button', { name: '创建' }));
    expect(await screen.findByText('kept_abc1234secretsecretsecret')).toBeInTheDocument();
    expect(screen.getByText(/只显示这一次/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '关闭' }));
    expect(await screen.findByText('kept_abc1234…')).toBeInTheDocument();
    expect(screen.queryByText('kept_abc1234secretsecretsecret')).not.toBeInTheDocument();
  });

  it('复制失败时提示手动复制，而不是“已复制”（F4）', async () => {
    const originalClipboard = navigator.clipboard;
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true });
    stubFetch({
      'GET /api/v1/me/tokens': () => jsonResponse(200, []),
      'POST /api/v1/me/tokens': () => jsonResponse(201, { token: 'kept_abc1234secretsecretsecret', apiToken: { id: 7, name: '扫描仪', prefix: 'kept_abc1234', createdAt: '2024-03-05T10:00:00Z' } }),
    });
    renderWithProviders(<><TokensPanel /><Toast /></>);
    await userEvent.click(await screen.findByRole('button', { name: '新建 Token' }));
    await userEvent.type(screen.getByLabelText(/^名称/), '扫描仪');
    await userEvent.click(screen.getByRole('button', { name: '创建' }));
    await screen.findByText('kept_abc1234secretsecretsecret');
    await userEvent.click(screen.getByRole('button', { name: '复制' }));
    expect(await screen.findByText('无法自动复制，请手动选中复制')).toBeInTheDocument();
    expect(screen.queryByText('已复制')).not.toBeInTheDocument();
    Object.defineProperty(navigator, 'clipboard', { value: originalClipboard, configurable: true });
  });

  it('吊销前需要确认', async () => {
    const calls = stubFetch({
      'GET /api/v1/me/tokens': () => jsonResponse(200, [{ id: 7, name: '扫描仪', prefix: 'kept_abc1234', createdAt: '2024-03-05T10:00:00Z' }]),
      'DELETE /api/v1/me/tokens/7': () => new Response(null, { status: 204 }),
    });
    renderWithProviders(<TokensPanel />);
    await userEvent.click(await screen.findByRole('button', { name: '吊销 扫描仪' }));
    expect(screen.getByText('吊销后，使用这个 Token 的脚本将无法再访问。')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '吊销' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'DELETE')).toBe(true));
  });
});

const users = [
  { id: 1, username: 'admin', displayName: '王管理', isAdmin: true, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: true },
  { id: 2, username: 'bob', displayName: 'Bob', isAdmin: false, disabled: true, createdAt: '2024-03-06T10:00:00Z', twoFactorEnabled: true },
];

describe('AccountsPanel', () => {
  it('列出账号并标出管理员和已停用', async () => {
    stubFetch({
      'GET /api/v1/admin/users': () => jsonResponse(200, users),
    });
    renderWithProviders(<AccountsPanel />);
    expect(await screen.findByText('管理员')).toBeInTheDocument();
    expect(screen.getByText('已停用')).toBeInTheDocument();
  });

  it('说明账号由管理员创建，没有开放注册的开关', async () => {
    const calls = stubFetch({ 'GET /api/v1/admin/users': () => jsonResponse(200, users) });
    renderWithProviders(<AccountsPanel />);
    expect(await screen.findByText(/账号由管理员创建，不提供注册/)).toBeInTheDocument();
    expect(screen.queryByLabelText('允许自行注册')).not.toBeInTheDocument();
    expect(calls.some((r) => new URL(r.url).pathname === '/api/v1/admin/settings')).toBe(false);
  });

  it('新建账号，失败时显示服务端说明', async () => {
    stubFetch({
      'GET /api/v1/admin/users': () => jsonResponse(200, users),
      'POST /api/v1/admin/users': () => jsonResponse(409, { code: 'conflict', message: '这个用户名已经被使用' }),
    });
    renderWithProviders(<AccountsPanel />);
    await userEvent.click(await screen.findByRole('button', { name: '新建账号' }));
    await userEvent.type(screen.getByLabelText(/^邮箱/), 'bob@example.com');
    await userEvent.type(screen.getByLabelText(/^用户名/), 'bob');
    await userEvent.type(screen.getByLabelText(/^初始密码/), 'password-123');
    await userEvent.click(screen.getByRole('button', { name: '创建' }));
    expect(await screen.findByText('这个用户名已经被使用')).toBeInTheDocument();
  });

  it('新建账号时用户名留空，请求体中不包含 username', async () => {
    const calls = stubFetch({
      'GET /api/v1/admin/users': () => jsonResponse(200, users),
      'POST /api/v1/admin/users': () => jsonResponse(201, { id: 3, username: 'bob-example-com', displayName: '', isAdmin: false, disabled: false, createdAt: '2024-03-07T10:00:00Z', twoFactorEnabled: false }),
    });
    renderWithProviders(<AccountsPanel />);
    await userEvent.click(await screen.findByRole('button', { name: '新建账号' }));
    await userEvent.type(screen.getByLabelText(/^邮箱/), 'bob@example.com');
    await userEvent.type(screen.getByLabelText(/^初始密码/), 'password-123');
    await userEvent.click(screen.getByRole('button', { name: '创建' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'POST')).toBe(true));
    const body = await calls.find((r) => r.method === 'POST')!.clone().json();
    expect('username' in body).toBe(false);
  });

  it('自己那一行的菜单看不到“重置密码”，他人那一行能看到（F3）', async () => {
    stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { ...me, user: { ...me.user, id: 1, isAdmin: true } }),
      'GET /api/v1/admin/users': () => jsonResponse(200, users),
    });
    renderWithProviders(<AccountsPanel />);
    await screen.findByText('管理员');
    await userEvent.click(screen.getByLabelText('管理 admin'));
    expect(screen.queryByRole('menuitem', { name: '重置密码' })).not.toBeInTheDocument();
  });

  it('他人那一行的菜单能看到“重置密码”（F3）', async () => {
    stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { ...me, user: { ...me.user, id: 1, isAdmin: true } }),
      'GET /api/v1/admin/users': () => jsonResponse(200, users),
    });
    renderWithProviders(<AccountsPanel />);
    await screen.findByText('Bob');
    await userEvent.click(screen.getByLabelText('管理 bob'));
    expect(screen.getByRole('menuitem', { name: '重置密码' })).toBeInTheDocument();
  });

  it('已开启两步验证的账号显示“两步验证”标签', async () => {
    stubFetch({
      'GET /api/v1/admin/users': () => jsonResponse(200, users),
    });
    renderWithProviders(<AccountsPanel />);
    await screen.findByText('Bob');
    expect(screen.getAllByText('两步验证')).toHaveLength(2);
  });

  it('自己那一行看不到“关闭两步验证”，他人那一行能看到并可以关闭（F3）', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { ...me, user: { ...me.user, id: 1, isAdmin: true } }),
      'GET /api/v1/admin/users': () => jsonResponse(200, users),
      'DELETE /api/v1/admin/users/2/2fa': () => new Response(null, { status: 204 }),
    });
    renderWithProviders(<><AccountsPanel /><Toast /></>);
    await screen.findByText('Bob');

    await userEvent.click(screen.getByLabelText('管理 admin'));
    expect(screen.queryByRole('menuitem', { name: '关闭两步验证' })).not.toBeInTheDocument();
    await userEvent.click(document.body);

    await userEvent.click(screen.getByLabelText('管理 bob'));
    await userEvent.click(screen.getByRole('menuitem', { name: '关闭两步验证' }));
    expect(screen.getByRole('heading', { name: '关闭两步验证' })).toBeInTheDocument();
    expect(screen.getByText('关闭 Bob 的两步验证后，对方下次登录只需要密码。')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '关闭两步验证' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'DELETE')).toBe(true));
    expect(await screen.findByText('已关闭两步验证')).toBeInTheDocument();
  });
});

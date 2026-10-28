import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { LoginPage } from './login-page';
import { AuthGate } from './auth-gate';

const me = {
  user: { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: true },
  workspace: { id: 1, name: '小王 的空间', kind: 'personal', role: 'owner' },
  csrfToken: 'tok-1',
};

const okHealth = () => jsonResponse(200, { status: 'ok', checks: {} });
const okVersion = () => jsonResponse(200, { version: '0.1.0', commit: 'abc', buildTime: '2024-01-01T00:00:00Z' });

afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); });

describe('LoginPage 品牌区', () => {
  it('渲染大标题，版本加载后显示自托管标签', async () => {
    stubFetch({ 'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion });
    renderWithProviders(<LoginPage />);
    expect(screen.getByText('文件放进抽屉，需要时一搜就有')).toBeInTheDocument();
    expect(screen.queryByText(/自托管版/)).not.toBeInTheDocument();
    expect(await screen.findByText('v0.1.0 · 自托管版')).toBeInTheDocument();
  });
});

describe('LoginPage 输入框尺寸', () => {
  // 设计稿中登录页的输入框都是 medium，主题默认的 small 在这里太矮。
  const smallInputs = () => document.querySelectorAll('.MuiInputBase-sizeSmall');

  it('登录、两步验证、初始化各步骤的输入框都不是 small', async () => {
    stubFetch({
      'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion,
      'POST /api/v1/auth/login': () => jsonResponse(202, { challenge: 'c-1', expiresAt: '2030-01-01T00:00:00Z' }),
    });
    const { unmount } = renderWithProviders(<LoginPage />);
    expect(screen.getByLabelText('用户名或邮箱')).toBeInTheDocument();
    expect(smallInputs()).toHaveLength(0);

    await userEvent.type(screen.getByLabelText('用户名或邮箱'), 'wang');
    await userEvent.type(screen.getByLabelText('密码'), 'password-123');
    await userEvent.click(screen.getByRole('button', { name: '登录' }));
    expect(await screen.findByRole('heading', { name: '两步验证' })).toBeInTheDocument();
    expect(smallInputs()).toHaveLength(0);
    await userEvent.click(screen.getByRole('button', { name: '改用恢复码' }));
    expect(screen.getByLabelText('恢复码')).toBeInTheDocument();
    expect(smallInputs()).toHaveLength(0);
    unmount();

    renderWithProviders(<LoginPage initialStep="setup" />);
    expect(screen.getByLabelText('邮箱')).toBeInTheDocument();
    expect(smallInputs()).toHaveLength(0);
  });
});

describe('LoginPage 登录', () => {
  it('请求体默认 remember 为 true', async () => {
    const calls = stubFetch({
      'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion,
      'POST /api/v1/auth/login': () => jsonResponse(200, me),
    });
    renderWithProviders(<LoginPage />);
    await userEvent.type(screen.getByLabelText('用户名或邮箱'), 'wang');
    await userEvent.type(screen.getByLabelText('密码'), 'password-123');
    await userEvent.click(screen.getByRole('button', { name: '登录' }));
    const req = await vi.waitUntil(() => calls.find((r) => r.method === 'POST' && new URL(r.url).pathname === '/api/v1/auth/login'));
    expect(await req.clone().json()).toEqual({ login: 'wang', password: 'password-123', remember: true });
  });

  it('取消勾选“30 天内记住我”后 remember 为 false', async () => {
    const calls = stubFetch({
      'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion,
      'POST /api/v1/auth/login': () => jsonResponse(200, me),
    });
    renderWithProviders(<LoginPage />);
    await userEvent.type(screen.getByLabelText('用户名或邮箱'), 'wang');
    await userEvent.type(screen.getByLabelText('密码'), 'password-123');
    await userEvent.click(screen.getByLabelText('30 天内记住我'));
    await userEvent.click(screen.getByRole('button', { name: '登录' }));
    const req = await vi.waitUntil(() => calls.find((r) => r.method === 'POST' && new URL(r.url).pathname === '/api/v1/auth/login'));
    expect(await req.clone().json()).toEqual({ login: 'wang', password: 'password-123', remember: false });
  });

  it('401 且带剩余次数时显示对应提示', async () => {
    stubFetch({
      'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion,
      'POST /api/v1/auth/login': () => jsonResponse(401, { code: 'unauthorized', message: '用户名或密码不正确', details: { remainingAttempts: 3 } }),
    });
    renderWithProviders(<LoginPage />);
    await userEvent.type(screen.getByLabelText('用户名或邮箱'), 'wang');
    await userEvent.type(screen.getByLabelText('密码'), 'wrong');
    await userEvent.click(screen.getByRole('button', { name: '登录' }));
    expect(await screen.findByText('用户名或密码不正确。再错 3 次，账号会被锁定 15 分钟。')).toBeInTheDocument();
  });

  it('429 时进入锁定倒计时，密码框和登录按钮不可用，一秒后减一（假计时器）', async () => {
    stubFetch({
      'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion,
      'POST /api/v1/auth/login': () => jsonResponse(429, { code: 'locked', message: '账号已锁定', details: { retryAfterSeconds: 900 } }),
    });
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    renderWithProviders(<LoginPage />);
    await user.type(screen.getByLabelText('用户名或邮箱'), 'wang');
    await user.type(screen.getByLabelText('密码'), 'wrong');
    await user.click(screen.getByRole('button', { name: '登录' }));
    await vi.waitFor(() => expect(screen.getByText(/15:00/)).toBeInTheDocument());
    expect(screen.getByLabelText('密码')).toBeDisabled();
    expect(screen.getByRole('button', { name: '登录' })).toBeDisabled();
    // 锁定按账号：用户名或邮箱仍可编辑，以便换一个账号登录。
    expect(screen.getByLabelText('用户名或邮箱')).toBeEnabled();
    await act(async () => { vi.advanceTimersByTime(1000); });
    expect(screen.getByText(/14:59/)).toBeInTheDocument();
  });

  it('锁定后修改用户名或邮箱：倒计时提示消失，密码框恢复可用', async () => {
    stubFetch({
      'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion,
      'POST /api/v1/auth/login': () => jsonResponse(429, { code: 'locked', message: '账号已锁定', details: { retryAfterSeconds: 900 } }),
    });
    renderWithProviders(<LoginPage />);
    await userEvent.type(screen.getByLabelText('用户名或邮箱'), 'wang');
    await userEvent.type(screen.getByLabelText('密码'), 'wrong');
    await userEvent.click(screen.getByRole('button', { name: '登录' }));
    expect(await screen.findByText(/账号已锁定/)).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText('用户名或邮箱'), '2');
    expect(screen.queryByText(/账号已锁定/)).not.toBeInTheDocument();
    expect(screen.getByLabelText('密码')).toBeEnabled();
    expect(screen.getByRole('button', { name: '登录' })).toBeEnabled();
  });
});

describe('LoginPage 两步验证', () => {
  it('登录返回 202 后进入两步验证，验证码输满 6 位自动提交，成功后渲染应用本体', async () => {
    const calls = stubFetch({
      'GET /api/v1/setup': () => jsonResponse(200, { needsSetup: false }),
      'GET /api/v1/me': () => jsonResponse(401, { code: 'unauthorized', message: '请先登录' }),
      'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion,
      'POST /api/v1/auth/login': () => jsonResponse(202, { challenge: 'chal-1', expiresAt: '2024-03-05T10:05:00Z' }),
      'POST /api/v1/auth/login/2fa': () => jsonResponse(200, me),
    });
    renderWithProviders(<AuthGate><div>应用本体</div></AuthGate>);
    await userEvent.type(await screen.findByLabelText('用户名或邮箱'), 'wang');
    await userEvent.type(screen.getByLabelText('密码'), 'password-123');
    await userEvent.click(screen.getByRole('button', { name: '登录' }));
    expect(await screen.findByRole('heading', { name: '两步验证' })).toBeInTheDocument();
    await userEvent.type(screen.getByPlaceholderText('000000'), '123456');
    expect(await screen.findByText('应用本体')).toBeInTheDocument();
    const req = calls.find((r) => r.method === 'POST' && new URL(r.url).pathname === '/api/v1/auth/login/2fa')!;
    expect(await req.clone().json()).toEqual({ challenge: 'chal-1', code: '123456' });
  });

  it('challenge_expired 时回到登录步骤，并显示服务端返回的说明', async () => {
    stubFetch({
      'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion,
      'POST /api/v1/auth/login': () => jsonResponse(202, { challenge: 'chal-1', expiresAt: '2024-03-05T10:05:00Z' }),
      'POST /api/v1/auth/login/2fa': () => jsonResponse(401, { code: 'challenge_expired', message: '验证已过期，请重新登录' }),
    });
    renderWithProviders(<LoginPage />);
    await userEvent.type(screen.getByLabelText('用户名或邮箱'), 'wang');
    await userEvent.type(screen.getByLabelText('密码'), 'password-123');
    await userEvent.click(screen.getByRole('button', { name: '登录' }));
    await userEvent.type(await screen.findByPlaceholderText('000000'), '123456');
    expect(await screen.findByRole('heading', { name: '登录' })).toBeInTheDocument();
    expect(screen.getByText('验证已过期，请重新登录')).toBeInTheDocument();
  });

  it('点击“改用恢复码”后用 recoveryCode 提交', async () => {
    const calls = stubFetch({
      'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion,
      'POST /api/v1/auth/login': () => jsonResponse(202, { challenge: 'chal-1', expiresAt: '2024-03-05T10:05:00Z' }),
      'POST /api/v1/auth/login/2fa': () => jsonResponse(200, me),
    });
    renderWithProviders(<LoginPage />);
    await userEvent.type(screen.getByLabelText('用户名或邮箱'), 'wang');
    await userEvent.type(screen.getByLabelText('密码'), 'password-123');
    await userEvent.click(screen.getByRole('button', { name: '登录' }));
    await userEvent.click(await screen.findByRole('button', { name: '改用恢复码' }));
    await userEvent.type(screen.getByLabelText('恢复码'), 'ABCD-1234');
    await userEvent.click(screen.getByRole('button', { name: '验证' }));
    const req = await vi.waitUntil(() => calls.find((r) => r.method === 'POST' && new URL(r.url).pathname === '/api/v1/auth/login/2fa'));
    expect(await req.clone().json()).toEqual({ challenge: 'chal-1', recoveryCode: 'ABCD-1234' });
  });
});

describe('LoginPage 忘记密码', () => {
  it('点击“忘记密码？”后能看到服务器端命令说明', async () => {
    stubFetch({ 'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion });
    renderWithProviders(<LoginPage />);
    await userEvent.click(screen.getByText('忘记密码？'));
    expect(screen.getByText(/kept user set-password/)).toBeInTheDocument();
  });
});

describe('LoginPage 服务器状态', () => {
  it('/healthz 返回 200 时显示已连接', async () => {
    stubFetch({ 'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion });
    renderWithProviders(<LoginPage />);
    expect(await screen.findByText('已连接')).toBeInTheDocument();
  });

  it('fetch 抛出异常时显示无法连接，并出现登录页的连接错误提示', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('network error'); }));
    renderWithProviders(<LoginPage />);
    expect(await screen.findByText('无法连接')).toBeInTheDocument();
    expect(await screen.findByText('无法连接到服务器。请检查服务器地址，或确认服务正在运行。')).toBeInTheDocument();
  });
});

describe('LoginPage 初始化', () => {
  it('需要初始化时显示“创建管理员账号”', async () => {
    stubFetch({ 'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion });
    renderWithProviders(<LoginPage initialStep="setup" />);
    expect(screen.getByRole('heading', { name: '创建管理员账号' })).toBeInTheDocument();
    expect(screen.getByText('这是第一次启动。创建的账号拥有全部权限，之后可以在“账号管理”中为其他成员创建账号。')).toBeInTheDocument();
  });

  it('密码为 abcdefgh 时强度为“较弱”，按钮不可用；为 abcdefg1 时强度为“中等”，按钮可用', async () => {
    stubFetch({ 'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion });
    renderWithProviders(<LoginPage initialStep="setup" />);
    await userEvent.type(screen.getByLabelText('名字'), '管理员');
    await userEvent.type(screen.getByLabelText('邮箱'), 'admin@example.com');
    await userEvent.type(screen.getByLabelText('密码'), 'abcdefgh');
    expect(screen.getByText(/较弱/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '创建并进入' })).toBeDisabled();

    await userEvent.clear(screen.getByLabelText('密码'));
    await userEvent.type(screen.getByLabelText('密码'), 'abcdefg1');
    expect(screen.getByText(/中等/)).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('确认密码'), 'abcdefg1');
    expect(screen.getByRole('button', { name: '创建并进入' })).toBeEnabled();
  });

  it('两次密码不一致时显示提示', async () => {
    stubFetch({ 'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion });
    renderWithProviders(<LoginPage initialStep="setup" />);
    await userEvent.type(screen.getByLabelText('密码'), 'abcdefg1');
    await userEvent.type(screen.getByLabelText('确认密码'), 'abcdefg2');
    expect(screen.getByText('两次输入的密码不一致')).toBeInTheDocument();
  });
});

describe('LoginPage 不做的功能', () => {
  it('页面中没有 SSO、简体中文、原型状态、演示账号、注册入口', async () => {
    stubFetch({ 'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion });
    renderWithProviders(<LoginPage />);
    expect(screen.queryByText(/SSO/)).not.toBeInTheDocument();
    expect(screen.queryByText('简体中文')).not.toBeInTheDocument();
    expect(screen.queryByText('原型状态')).not.toBeInTheDocument();
    expect(screen.queryByText(/演示账号/)).not.toBeInTheDocument();
    expect(screen.queryByText(/注册/)).not.toBeInTheDocument();
  });
});

describe('单点登录', () => {
  it('配置了时显示按钮，带上“记住我”和当前页面', async () => {
    stubFetch({ 'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion, 'GET /api/v1/setup': () => jsonResponse(200, { needsSetup: false, sso: { name: '飞书' } }) });
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, href: 'http://localhost/drawers/3?x=1', pathname: '/drawers/3', search: '?x=1', hash: '', assign });
    renderWithProviders(<LoginPage />);
    await userEvent.click(screen.getByLabelText('30 天内记住我'));
    await userEvent.click(await screen.findByRole('button', { name: '使用飞书登录' }));
    expect(assign).toHaveBeenCalledWith('/api/v1/auth/sso/start?remember=false&next=%2Fdrawers%2F3%3Fx%3D1');
  });

  it('默认名称时按钮就叫“单点登录”；没配置时没有按钮', async () => {
    stubFetch({ 'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion, 'GET /api/v1/setup': () => jsonResponse(200, { needsSetup: false, sso: { name: '单点登录', default: true } }) });
    const { unmount } = renderWithProviders(<LoginPage />);
    expect(await screen.findByRole('button', { name: '单点登录' })).toBeInTheDocument();
    unmount();
    stubFetch({ 'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion, 'GET /api/v1/setup': () => jsonResponse(200, { needsSetup: false }) });
    renderWithProviders(<LoginPage />);
    await screen.findByLabelText('用户名或邮箱');
    await act(() => new Promise((r) => setTimeout(r, 20)));
    expect(screen.queryByRole('button', { name: /单点登录|使用/ })).not.toBeInTheDocument();
  });

  it('身份服务转回来带着原因时显示提示，并从地址栏去掉', async () => {
    stubFetch({ 'GET /healthz': okHealth, 'GET /api/v1/system/version': okVersion, 'GET /api/v1/setup': () => jsonResponse(200, { needsSetup: false, sso: { name: '飞书' } }) });
    window.history.replaceState(null, '', '/?sso=no_account');
    renderWithProviders(<LoginPage />);
    expect(screen.getByText(/没有对应的账号/)).toBeInTheDocument();
    expect(window.location.search).toBe('');
  });
});

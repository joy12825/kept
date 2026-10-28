import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { TwoFactorPanel } from './two-factor';
import { Toast } from '../../components/overlays';
import SettingsPage from '../settings';

const me = {
  user: { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z' },
  workspace: { id: 1, name: '小王 的空间', kind: 'personal', role: 'owner' },
  csrfToken: 't',
};

vi.mock('qrcode', () => ({ toDataURL: vi.fn().mockResolvedValue('data:image/png;base64,xxx') }));

afterEach(() => vi.unstubAllGlobals());

const setup = { secret: 'ABCD1234EFGH5678', otpauthUri: 'otpauth://totp/kept:wang?secret=ABCD1234EFGH5678&issuer=kept' };
const recoveryCodes = Array.from({ length: 10 }, (_, i) => `code-${i}`);

describe('TwoFactorPanel', () => {
  it('未开启时，完整开启一次后显示“已开启 · 剩余 10 个恢复码”', async () => {
    let enabled = false;
    stubFetch({
      'GET /api/v1/me/2fa': () => jsonResponse(200, enabled ? { enabled: true, recoveryCodesLeft: 10 } : { enabled: false, recoveryCodesLeft: 0 }),
      'POST /api/v1/me/2fa/setup': () => jsonResponse(200, setup),
      'POST /api/v1/me/2fa/enable': () => { enabled = true; return jsonResponse(200, { codes: recoveryCodes }); },
    });
    renderWithProviders(<TwoFactorPanel />);
    await userEvent.click(await screen.findByRole('button', { name: '开启两步验证' }));
    expect(await screen.findByAltText('两步验证二维码')).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText(/^6 位验证码/), '123456');
    await userEvent.click(screen.getByRole('button', { name: '验证并开启' }));
    for (const c of recoveryCodes) expect(await screen.findByText(c)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '完成' }));
    expect(await screen.findByText('已开启 · 剩余 10 个恢复码')).toBeInTheDocument();
  });

  it('输入验证码错误时在输入框下方显示服务端说明', async () => {
    stubFetch({
      'GET /api/v1/me/2fa': () => jsonResponse(200, { enabled: false, recoveryCodesLeft: 0 }),
      'POST /api/v1/me/2fa/setup': () => jsonResponse(200, setup),
      'POST /api/v1/me/2fa/enable': () => jsonResponse(400, { code: 'invalid_request', message: '验证码不正确' }),
    });
    renderWithProviders(<TwoFactorPanel />);
    await userEvent.click(await screen.findByRole('button', { name: '开启两步验证' }));
    await screen.findByAltText('两步验证二维码');
    await userEvent.type(screen.getByLabelText(/^6 位验证码/), '000000');
    await userEvent.click(screen.getByRole('button', { name: '验证并开启' }));
    expect(await screen.findByText('验证码不正确')).toBeInTheDocument();
  });

  it('6 位验证码输入框去掉非数字并截断为 6 位', async () => {
    const calls = stubFetch({
      'GET /api/v1/me/2fa': () => jsonResponse(200, { enabled: false, recoveryCodesLeft: 0 }),
      'POST /api/v1/me/2fa/setup': () => jsonResponse(200, setup),
      'POST /api/v1/me/2fa/enable': () => jsonResponse(200, { codes: recoveryCodes }),
    });
    renderWithProviders(<TwoFactorPanel />);
    await userEvent.click(await screen.findByRole('button', { name: '开启两步验证' }));
    await screen.findByAltText('两步验证二维码');
    const input = screen.getByLabelText(/^6 位验证码/);
    await userEvent.type(input, '12 3-45a6789');
    expect(input).toHaveValue('123456');
    await userEvent.click(screen.getByRole('button', { name: '验证并开启' }));
    const req = await vi.waitUntil(() => calls.find((r) => r.method === 'POST' && new URL(r.url).pathname === '/api/v1/me/2fa/enable'));
    expect(await req.clone().json()).toEqual({ code: '123456' });
  });

  describe('关闭两步验证', () => {
    it('密码错误时显示“当前密码不正确”', async () => {
      stubFetch({
        'GET /api/v1/me/2fa': () => jsonResponse(200, { enabled: true, recoveryCodesLeft: 8 }),
        'POST /api/v1/me/2fa/disable': () => jsonResponse(400, { code: 'invalid_request', message: '当前密码不正确' }),
      });
      renderWithProviders(<TwoFactorPanel />);
      await userEvent.click(await screen.findByRole('button', { name: '关闭两步验证' }));
      await userEvent.type(screen.getByLabelText(/^当前密码/), 'bad-password');
      await userEvent.click(screen.getByRole('button', { name: '关闭两步验证' }));
      expect(await screen.findByText('当前密码不正确')).toBeInTheDocument();
    });

    it('密码正确时提示“已关闭两步验证”', async () => {
      stubFetch({
        'GET /api/v1/me/2fa': () => jsonResponse(200, { enabled: true, recoveryCodesLeft: 8 }),
        'POST /api/v1/me/2fa/disable': () => new Response(null, { status: 204 }),
      });
      renderWithProviders(<><TwoFactorPanel /><Toast /></>);
      await userEvent.click(await screen.findByRole('button', { name: '关闭两步验证' }));
      await userEvent.type(screen.getByLabelText(/^当前密码/), 'good-password');
      const dialogButtons = screen.getAllByRole('button', { name: '关闭两步验证' });
      await userEvent.click(dialogButtons[dialogButtons.length - 1]);
      expect(await screen.findByText('已关闭两步验证')).toBeInTheDocument();
    });
  });
});

describe('设置导航', () => {
  it('出现“密码与安全”标签', async () => {
    stubFetch({ 'GET /api/v1/me': () => jsonResponse(200, me) });
    renderWithProviders(<SettingsPage />, { route: '/settings/profile' });
    expect(await screen.findByText('密码与安全')).toBeInTheDocument();
  });
});

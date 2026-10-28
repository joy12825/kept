import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken } from '../../api/client';
import { ConfirmDialog } from '../../components/overlays';
import { AccountsPanel } from './accounts';

const user = (id: number, username: string, extra: object = {}) => ({ id, username, displayName: username, email: `${username}@example.com`, isAdmin: false, disabled: false,
  createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false, ...extra });

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); });

describe('账号管理', () => {
  it('已绑定单点登录的账号带标记，可以解除绑定', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user: user(1, 'admin', { isAdmin: true }), workspace: { id: 1, name: 'a', kind: 'personal', color: 'green', role: 'owner', memberCount: 1 }, csrfToken: 't' }),
      'GET /api/v1/admin/users': () => jsonResponse(200, [user(1, 'admin', { isAdmin: true }), user(2, 'wang', { sso: true })]),
      'DELETE /api/v1/admin/users/2/sso': () => new Response(null, { status: 204 }),
    });
    renderWithProviders(<><AccountsPanel /><ConfirmDialog /></>);
    expect(await screen.findByText('单点登录')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '管理 wang' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '解除单点登录绑定' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: '解除' }));
    await vi.waitUntil(() => calls.find((r) => r.method === 'DELETE'));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await userEvent.click(screen.getByRole('button', { name: '管理 admin' }));
    expect(screen.queryByRole('menuitem', { name: '解除单点登录绑定' })).not.toBeInTheDocument();
  });
});

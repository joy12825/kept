import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken, setWorkspaceId, type Workspace } from '../../api/client';
import { NotificationsPanel } from './notifications';
import { AlertNotifier } from '../../components/alert-notifier';
import { settingsTabs } from '../settings';

const home: Workspace = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'viewer', memberCount: 2 };
const user = { id: 1, username: 'wang', displayName: '小王', email: 'wang@example.com', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const me = () => jsonResponse(200, { user, workspace: home, csrfToken: 'tok-1', features: { smtp: true } });
const alert = (id: number, text: string) => ({ id, drawerId: 3, factId: 1, kind: 'missing', text, warn: true, createdAt: '2026-10-01T00:00:00Z' });

// jsdom 没有 Notification：换成记录下来的假实现
const shown: { title: string; body?: string }[] = [];
class FakeNotification {
  static permission: NotificationPermission = 'default';
  static requestPermission = vi.fn(async () => { FakeNotification.permission = 'granted'; return 'granted' as NotificationPermission; });
  onclick: (() => void) | null = null;
  constructor(title: string, opts?: NotificationOptions) { shown.push({ title, body: opts?.body }); }
  close() {}
}

beforeEach(() => { shown.length = 0; FakeNotification.permission = 'default'; localStorage.clear(); vi.stubGlobal('Notification', FakeNotification); });
afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

describe('NotificationsPanel', () => {
  it('所有成员都有“通知”', () => {
    expect(settingsTabs({ isAdmin: false, role: 'viewer' }).map(([k]) => k)).toContain('notify');
  });

  it('开启邮件提醒', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': me,
      'GET /api/v1/alerts': () => jsonResponse(200, []),
      'GET /api/v1/me/notifications': () => jsonResponse(200, { emailAlerts: false, email: 'wang@example.com', smtp: true }),
      'PUT /api/v1/me/notifications': () => jsonResponse(200, { emailAlerts: true, email: 'wang@example.com', smtp: true }),
    });
    renderWithProviders(<NotificationsPanel />);
    expect(await screen.findByText(/发到 wang@example.com/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('switch', { name: '邮件提醒' }));
    await waitFor(() => expect(screen.getByRole('switch', { name: '邮件提醒' })).toBeChecked());
    const body = await calls.find((r) => r.method === 'PUT')!.clone().json();
    expect(body).toEqual({ emailAlerts: true });
  });

  it('没有配置发信或没有邮箱时不能开启', async () => {
    stubFetch({
      'GET /api/v1/me': me,
      'GET /api/v1/alerts': () => jsonResponse(200, []),
      'GET /api/v1/me/notifications': () => jsonResponse(200, { emailAlerts: false, smtp: false }),
    });
    renderWithProviders(<NotificationsPanel />);
    expect(await screen.findByText(/管理员还没有配置发信/)).toBeInTheDocument();
    expect(screen.getByRole('switch', { name: '邮件提醒' })).toBeDisabled();
  });

  it('开启浏览器通知后，只为之后新出现的提醒弹出通知', async () => {
    let alerts = [alert(1, '电费账单：9 月的还没有存入')];
    stubFetch({
      'GET /api/v1/me': me,
      'GET /api/v1/alerts': () => jsonResponse(200, alerts),
      'GET /api/v1/me/notifications': () => jsonResponse(200, { emailAlerts: false, email: 'wang@example.com', smtp: true }),
    });
    const { unmount } = renderWithProviders(<NotificationsPanel />);
    await screen.findByText(/发到 wang@example.com/);
    await waitFor(() => expect(screen.getByRole('switch', { name: '浏览器通知' })).toBeEnabled());
    await userEvent.click(screen.getByRole('switch', { name: '浏览器通知' }));
    await waitFor(() => expect(screen.getByRole('switch', { name: '浏览器通知' })).toBeChecked());
    expect(FakeNotification.requestPermission).toHaveBeenCalled();
    unmount();

    // 已有的那条不通知；新出现的通知一次
    alerts = [...alerts, alert(2, '宽带合同：到期日 2026-10-20，还剩 17 天')];
    renderWithProviders(<AlertNotifier />);
    await waitFor(() => expect(shown).toEqual([{ title: '收好提醒', body: '宽带合同：到期日 2026-10-20，还剩 17 天' }]));
  });
});

import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import SettingsPage, { settingsGroups, settingsTabs } from './settings';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';

afterEach(() => vi.unstubAllGlobals());

describe('settingsGroups', () => {
  it('普通成员只看到“个人”和“空间”两组，没有管理员才有的项', () => {
    const groups = settingsGroups({ isAdmin: false, role: 'editor' });
    expect(groups.map((g) => g.key)).toEqual(['me', 'space']);
    const space = groups[1].items.map(([k]) => k);
    expect(space).toContain('storage');
    expect(space).not.toContain('activity');
    expect(space).not.toContain('workflows');
    expect(space).toContain('links'); // 成员也能分享，也能看、作废分享链接
    expect(settingsGroups({ isAdmin: false, role: 'viewer' })[1].items.map(([k]) => k)).not.toContain('links');
  });

  it('空间管理员与实例管理员多出对应的项，“实例管理”放在最后', () => {
    const groups = settingsGroups({ isAdmin: true, role: 'owner' });
    expect(groups.map((g) => g.key)).toEqual(['me', 'space', 'instance']);
    expect(groups[1].items.map(([k]) => k)).toEqual(['members', 'private', 'activity', 'storage', 'classifier', 'scenes', 'ai', 'workflows', 'mail', 'sharing', 'links', 'upload-links', 'remote-inbox', 'emergency']);
    expect(groups[2].items.map(([k]) => k)).toEqual(['accounts', 'admin-activity', 'brand', 'storages', 'edition', 'system', 'logs']);
  });

  it('settingsTabs 是各组按顺序连起来的', () => {
    const who = { isAdmin: true, role: 'owner' } as const;
    expect(settingsTabs(who)).toEqual(settingsGroups(who).flatMap((g) => g.items));
  });
});

describe('设置的分组页签', () => {
  const me = {
    user: { id: 1, username: 'wang', displayName: '小王', isAdmin: true, disabled: false, createdAt: '2024-03-05T10:00:00Z' },
    workspace: { id: 2, name: '王家', kind: 'family', role: 'owner' },
    csrfToken: 't',
  };
  const page = (route: string) => renderWithProviders(<Routes><Route path="/settings/:tab" element={<SettingsPage />} /></Routes>, { route });

  it('按地址选中分组，左侧只列这一组的项；切到别的分组再切回来，回到原来那一项', async () => {
    stubFetch({ 'GET /api/v1/me': () => jsonResponse(200, me) });
    page('/settings/storage');
    await screen.findByRole('tab', { name: '实例管理' }); // 读到账号信息之后才有
    const space = screen.getByRole('tab', { name: '空间' });
    expect(space).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('button', { name: '存储' })).toHaveClass('Mui-selected');
    expect(screen.queryByRole('button', { name: '个人资料' })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('tab', { name: '实例管理' }));
    expect(await screen.findByRole('button', { name: '账号管理' })).toHaveClass('Mui-selected');
    expect(screen.queryByRole('button', { name: '存储' })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('tab', { name: '空间' }));
    expect(await screen.findByRole('button', { name: '存储' })).toHaveClass('Mui-selected');
  });
});

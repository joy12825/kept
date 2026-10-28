import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken, setWorkspaceId, type Workspace } from '../../api/client';
import { ActivityPanel } from './activity';
import { settingsTabs } from '../settings';

const home: Workspace = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'owner', memberCount: 2 };
const user = { id: 1, username: 'wang', displayName: '小王', email: 'wang@example.com', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const event = (id: number, action: string, summary: string, extra: object = {}) => ({
  id, action, actorId: 1, actorName: '小王', actorVia: 'session', targetType: 'user', targetId: '2', summary, changes: {},
  createdAt: new Date(Date.now() - id * 60_000).toISOString(), ...extra,
});
const members = [
  { userId: 1, username: 'wang', displayName: '小王', email: 'wang@example.com', role: 'owner', joinedAt: '2024-03-05T10:00:00Z' },
  { userId: 2, username: 'li', displayName: '小李', email: 'li@example.com', role: 'editor', joinedAt: '2024-03-06T10:00:00Z' },
];

function backend(audit: (req: Request) => Response, path = '/api/v1/audit') {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: home, csrfToken: 'tok-1' }),
    'GET /api/v1/workspaces/5/members': () => jsonResponse(200, members),
    [`GET ${path}`]: audit,
  });
}
const auditCalls = (calls: Request[], path = '/api/v1/audit') => calls.filter((r) => new URL(r.url).pathname === path);

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

describe('ActivityPanel', () => {
  it('列出当前空间的记录，API Token 的操作有来源标记', async () => {
    backend(() => jsonResponse(200, { items: [
      event(1, 'member.role_change', '小王 将 小李 的角色从 只看 改为 成员'),
      event(2, 'member.add', '小王 添加了成员 小李（只看）', { actorVia: 'token' }),
    ] }));
    renderWithProviders(<ActivityPanel scope="workspace" />);
    expect(await screen.findByText('小王 将 小李 的角色从 只看 改为 成员')).toBeInTheDocument();
    expect(screen.getByText('小王 添加了成员 小李（只看）')).toBeInTheDocument();
    expect(screen.getAllByText('通过 API Token')).toHaveLength(1);
    expect(screen.getByText('1 分钟前')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument();
  });

  it('按操作类型和成员筛选时带上对应的参数', async () => {
    const calls = backend(() => jsonResponse(200, { items: [event(1, 'member.add', '小王 添加了成员 小李（只看）')] }));
    renderWithProviders(<ActivityPanel scope="workspace" />);
    await screen.findByText('小王 添加了成员 小李（只看）');
    expect(new URL(auditCalls(calls)[0].url).searchParams.get('action')).toBeNull();

    await userEvent.click(screen.getByRole('combobox', { name: '操作类型' }));
    await userEvent.click(screen.getByRole('option', { name: '成员' }));
    await waitFor(() => expect(auditCalls(calls).some((r) => new URL(r.url).searchParams.get('action') === 'member.')).toBe(true));

    await userEvent.click(screen.getByRole('combobox', { name: '成员' }));
    await userEvent.click(await screen.findByRole('option', { name: '小李' }));
    await waitFor(() => expect(auditCalls(calls).some((r) => {
      const q = new URL(r.url).searchParams;
      return q.get('action') === 'member.' && q.get('actorId') === '2';
    })).toBe(true));

    await userEvent.click(screen.getByRole('combobox', { name: '时间' }));
    await userEvent.click(screen.getByRole('option', { name: '最近 7 天' }));
    await waitFor(() => expect(auditCalls(calls).some((r) => new URL(r.url).searchParams.has('from'))).toBe(true));
  });

  it('“加载更多”带上一页的 cursor，结果接在后面', async () => {
    const calls = backend((req) => new URL(req.url).searchParams.get('cursor') === '2'
      ? jsonResponse(200, { items: [event(3, 'workspace.create', '小王 新建了家庭空间“王家”')] })
      : jsonResponse(200, { items: [event(1, 'member.add', '第一条'), event(2, 'member.add', '第二条')], nextCursor: '2' }));
    renderWithProviders(<ActivityPanel scope="workspace" />);
    await screen.findByText('第二条');
    await userEvent.click(screen.getByRole('button', { name: '加载更多' }));
    expect(await screen.findByText('小王 新建了家庭空间“王家”')).toBeInTheDocument();
    expect(screen.getByText('第一条')).toBeInTheDocument();
    expect(new URL(auditCalls(calls)[1].url).searchParams.get('cursor')).toBe('2');
    await waitFor(() => expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument());
  });

  it('没有记录时显示空状态；出错时可以重试', async () => {
    let fail = true;
    backend(() => (fail ? jsonResponse(500, { code: 'internal', message: '服务器内部错误' }) : jsonResponse(200, { items: [] })));
    renderWithProviders(<ActivityPanel scope="workspace" />);
    const retry = await screen.findByRole('button', { name: '重新加载' });
    fail = false;
    await userEvent.click(retry);
    expect(await screen.findByText('还没有活动记录')).toBeInTheDocument();
  });

  it('账号活动读取实例级记录，没有成员筛选', async () => {
    const calls = backend(() => jsonResponse(200, { items: [event(1, 'user.create', '管理员 创建了账号 li')] }), '/api/v1/admin/audit');
    renderWithProviders(<ActivityPanel scope="instance" />);
    expect(await screen.findByText('管理员 创建了账号 li')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: '账号活动' })).toBeInTheDocument();
    expect(screen.queryByRole('combobox', { name: '成员' })).not.toBeInTheDocument();
    expect(screen.queryByRole('combobox', { name: '操作类型' })).not.toBeInTheDocument();
    expect(auditCalls(calls, '/api/v1/audit')).toHaveLength(0);
  });
});

describe('settingsTabs', () => {
  const keys = (isAdmin: boolean, role: 'owner' | 'editor' | 'viewer') => settingsTabs({ isAdmin, role }).map(([k]) => k);
  it('活动记录只给空间管理员，账号管理和账号活动只给实例管理员', () => {
    expect(keys(false, 'owner')).toContain('activity');
    expect(keys(false, 'editor')).not.toContain('activity');
    expect(keys(false, 'viewer')).not.toContain('activity');
    expect(keys(false, 'owner')).not.toContain('accounts');
    expect(keys(false, 'owner')).not.toContain('admin-activity');
    expect(keys(true, 'viewer')).toEqual(expect.arrayContaining(['accounts', 'admin-activity']));
    expect(keys(true, 'viewer')).not.toContain('activity');
    expect(keys(false, 'viewer')).toContain('members');
  });
});

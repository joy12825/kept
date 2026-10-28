import { eeBuild } from '@ee';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { getWorkspaceId, setCsrfToken, setWorkspaceId, type Workspace } from '../../api/client';
import { MembersPanel } from './members';

const personal: Workspace = { id: 1, name: '小王 的空间', kind: 'personal', color: 'blue', role: 'owner', memberCount: 1 };
const home: Workspace = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'owner', memberCount: 3 };
const wang = { id: 1, username: 'wang', displayName: '小王', email: 'wang@example.com', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const members = [
  { userId: 1, username: 'wang', displayName: '小王', email: 'wang@example.com', role: 'owner', joinedAt: '2024-03-05T10:00:00Z' },
  { userId: 2, username: 'li', displayName: '小李', email: 'li@example.com', role: 'editor', joinedAt: '2024-03-06T10:00:00Z' },
  { userId: 3, username: 'mama', displayName: '王妈妈', email: 'mama@example.com', role: 'viewer', joinedAt: '2024-03-07T10:00:00Z' },
];

function backend(workspace: Workspace, extra: Parameters<typeof stubFetch>[0] = {}, user = wang) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace, csrfToken: 'tok-1' }),
    'GET /api/v1/workspaces': () => jsonResponse(200, [personal, home]),
    [`GET /api/v1/workspaces/${workspace.id}/members`]: () => jsonResponse(200, workspace.kind === 'personal' ? members.slice(0, 1) : members),
    ...extra,
  });
}

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

describe('MembersPanel', () => {
  it('管理员：每个成员有角色下拉，可以添加成员', async () => {
    backend(home);
    renderWithProviders(<MembersPanel />);
    expect(await screen.findByText('小李')).toBeInTheDocument();
    expect(screen.getByText('家庭 · 3 位成员')).toBeInTheDocument();
    expect(screen.getByText('li@example.com')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '添加成员' })).toBeInTheDocument();
    expect(screen.getAllByRole('combobox')).toHaveLength(3);
    expect(screen.getByRole('combobox', { name: '小李 的角色' })).toHaveTextContent('成员');
    expect(screen.getByRole('combobox', { name: '王妈妈 的角色' })).toHaveTextContent('只看');
  });

  it('成员和只看的成员：只看到角色文字，没有添加成员，可以退出空间', async () => {
    backend({ ...home, role: 'editor' }, {}, { ...wang, id: 2, username: 'li', displayName: '小李' });
    renderWithProviders(<MembersPanel />);
    expect(await screen.findByText('王妈妈')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '添加成员' })).not.toBeInTheDocument();
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    expect(screen.getByText('管理员')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '退出空间' })).toBeInTheDocument();
  });

  it('个人空间：没有添加成员，说明只有自己', async () => {
    backend(personal);
    renderWithProviders(<MembersPanel />);
    expect(await screen.findByText(/个人空间只有你自己/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '添加成员' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '退出空间' })).not.toBeInTheDocument();
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
  });

  it('修改角色；服务端拒绝时显示它的说明', async () => {
    const calls = backend(home, {
      'PATCH /api/v1/workspaces/5/members/2': () => jsonResponse(200, { ...members[1], role: 'viewer' }),
      'PATCH /api/v1/workspaces/5/members/1': () => jsonResponse(409, { code: 'conflict', message: '至少需要保留一位管理员' }),
    });
    renderWithProviders(<MembersPanel />);
    await userEvent.click(await screen.findByRole('combobox', { name: '小李 的角色' }));
    await userEvent.click(screen.getByRole('option', { name: '只看' }));
    const patch = await vi.waitUntil(() => calls.find((r) => r.method === 'PATCH'));
    expect(new URL(patch.url).pathname).toBe('/api/v1/workspaces/5/members/2');
    expect(await patch.clone().json()).toEqual({ role: 'viewer' });

    await userEvent.click(screen.getByRole('combobox', { name: '小王 的角色' }));
    await userEvent.click(screen.getByRole('option', { name: '成员' }));
    expect(await screen.findByText('至少需要保留一位管理员')).toBeInTheDocument();
  });

  it('添加成员：提交用户名或邮箱和角色；找不到账号时显示说明', async () => {
    const calls = backend(home, {
      'POST /api/v1/workspaces/5/members': () => jsonResponse(404, { code: 'not_found', message: '找不到这个账号。账号需要先由管理员创建' }),
    });
    renderWithProviders(<MembersPanel />);
    await userEvent.click(await screen.findByRole('button', { name: '添加成员' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(/只能添加已有的账号/)).toBeInTheDocument();
    const submit = within(dialog).getByRole('button', { name: '添加成员' });
    expect(submit).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/用户名或邮箱/), ' zhang@example.com ');
    await userEvent.click(within(dialog).getByRole('combobox', { name: /角色/ }));
    await userEvent.click(screen.getByRole('option', { name: '只看' }));
    await userEvent.click(submit);
    const post = await vi.waitUntil(() => calls.find((r) => r.method === 'POST'));
    expect(await post.clone().json()).toEqual({ login: 'zhang@example.com', role: 'viewer' });
    expect(await within(dialog).findByText('找不到这个账号。账号需要先由管理员创建')).toBeInTheDocument();
  });

  it('移除成员需要确认，确认按钮与标题用同一个动词', async () => {
    const calls = backend(home, { 'DELETE /api/v1/workspaces/5/members/3': () => new Response(null, { status: 204 }) });
    renderWithProviders(<MembersPanel />);
    await userEvent.click(await screen.findByRole('button', { name: '王妈妈 的更多操作' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '移除' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('heading', { name: '移除王妈妈？' })).toBeInTheDocument();
    expect(calls.some((r) => r.method === 'DELETE')).toBe(false);
    await userEvent.click(within(dialog).getByRole('button', { name: '移除' }));
    await vi.waitUntil(() => calls.find((r) => r.method === 'DELETE'));
  });

  it('退出空间后回到个人空间', async () => {
    setWorkspaceId(5);
    const calls = backend({ ...home, role: 'viewer' }, { 'DELETE /api/v1/workspaces/5/members/3': () => new Response(null, { status: 204 }) },
      { ...wang, id: 3, username: 'mama', displayName: '王妈妈' });
    renderWithProviders(<MembersPanel />);
    await userEvent.click(await screen.findByRole('button', { name: '退出空间' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('heading', { name: '退出“王家”？' })).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: '退出' }));
    await vi.waitUntil(() => calls.find((r) => r.method === 'DELETE'));
    await waitFor(() => expect(getWorkspaceId()).toBeNull());
  });

  it.skipIf(!eeBuild)('管理员看到每人上传了多少，可以给成员设上传额度', async () => {
    const calls = backend(home, {
      'GET /api/v1/members/uploads': () => jsonResponse(200, [
        { userId: 1, uploaded: 3 * 2 ** 30, quota: 0 }, { userId: 2, uploaded: 950 << 20, quota: 1 << 30 }, { userId: 3, uploaded: 0, quota: 0 }]),
      'PUT /api/v1/members/3/upload-quota': () => new Response(null, { status: 204 }),
    });
    renderWithProviders(<MembersPanel />);
    expect(await screen.findByText('已上传 950 MB，额度 1.0 GB')).toBeInTheDocument();
    expect(screen.getByText('已上传 3.0 GB')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '王妈妈 的更多操作' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: '上传额度…' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('radio', { name: '指定上限' }));
    await userEvent.type(within(dialog).getByRole('spinbutton', { name: '上限' }), '2');
    await userEvent.click(within(dialog).getByRole('button', { name: '保存' }));
    const put = await vi.waitUntil(() => calls.find((r) => r.method === 'PUT'));
    expect(await put.clone().json()).toEqual({ bytes: 2 * 2 ** 30 });
  });
});


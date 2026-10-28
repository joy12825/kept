import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { getWorkspaceId, setCsrfToken, setWorkspaceId, type Workspace } from '../api/client';
import { WorkspaceSwitcher } from './workspace-switcher';

const personal: Workspace = { id: 1, name: '小王 的空间', kind: 'personal', color: 'blue', role: 'owner', memberCount: 1 };
const home: Workspace = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'owner', memberCount: 3 };
const studio: Workspace = { id: 6, name: '某某工作室', kind: 'team', color: 'purple', role: 'editor', memberCount: 8 };
const user = { id: 1, username: 'wang', displayName: '小王', email: 'wang@example.com', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };

/** 和真实后端一样：/me 按 X-Workspace-ID 返回当前空间。 */
function backend(list: Workspace[], extra: Parameters<typeof stubFetch>[0] = {}) {
  return stubFetch({
    'GET /api/v1/me': (req) => {
      const id = Number(req.headers.get('X-Workspace-ID') ?? list[0].id);
      return jsonResponse(200, { user, workspace: list.find((w) => w.id === id) ?? list[0], csrfToken: 'tok-1' });
    },
    'GET /api/v1/workspaces': () => jsonResponse(200, list),
    ...extra,
  });
}

const open = async () => userEvent.click(await screen.findByRole('button', { name: /当前空间/ }));

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

describe('WorkspaceSwitcher', () => {
  it('显示当前空间，菜单列出全部空间并标出当前的一个', async () => {
    backend([personal, home, studio]);
    setWorkspaceId(5);
    renderWithProviders(<WorkspaceSwitcher collapsed={false} go={() => {}} />);
    const trigger = await screen.findByRole('button', { name: /当前空间：王家/ });
    expect(within(trigger).getByText('家庭 · 3 位成员')).toBeInTheDocument();

    await open();
    const items = screen.getAllByRole('menuitemradio');
    expect(items.map((i) => i.getAttribute('aria-checked'))).toEqual(['false', 'true', 'false']);
    expect(within(items[0]).getByText('个人空间')).toBeInTheDocument();
    expect(within(items[2]).getByText('团队 · 8 位成员')).toBeInTheDocument();
  });

  it('点击另一个空间后切换过去，之后的请求带它的 ID', async () => {
    const calls = backend([personal, home, studio]);
    const go = vi.fn();
    renderWithProviders(<WorkspaceSwitcher collapsed={false} go={go} />);
    await open();
    await userEvent.click(screen.getByRole('menuitemradio', { name: /某某工作室/ }));
    await screen.findByRole('button', { name: /当前空间：某某工作室/ });
    expect(getWorkspaceId()).toBe(6);
    expect(go).toHaveBeenCalledWith('/');
    const meCalls = calls.filter((r) => new URL(r.url).pathname === '/api/v1/me');
    const last = meCalls[meCalls.length - 1];
    expect(last.headers.get('X-Workspace-ID')).toBe('6');
  });

  it('新建空间：名称为空时不能提交；提交后切换到新空间', async () => {
    const list = [personal];
    const created: Workspace = { id: 9, name: '王家', kind: 'family', color: 'red', role: 'owner', memberCount: 1 };
    const calls = backend(list, { 'POST /api/v1/workspaces': () => { list.push(created); return jsonResponse(201, created); } });
    renderWithProviders(<WorkspaceSwitcher collapsed={false} go={() => {}} />);
    await open();
    await userEvent.click(screen.getByRole('menuitem', { name: '新建空间' }));

    const dialog = await screen.findByRole('dialog');
    const submit = within(dialog).getByRole('button', { name: '创建空间' });
    expect(submit).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/名称/), '  王家 ');
    await userEvent.click(within(dialog).getByRole('radio', { name: '红' }));
    await userEvent.click(submit);

    const req = await vi.waitUntil(() => calls.find((r) => r.method === 'POST'));
    expect(await req.clone().json()).toEqual({ name: '王家', kind: 'family', color: 'red' });
    await waitFor(() => expect(getWorkspaceId()).toBe(9));
    await screen.findByRole('button', { name: /当前空间：王家/ });
  });

  it('新建失败时显示服务端的说明', async () => {
    backend([personal], { 'POST /api/v1/workspaces': () => jsonResponse(400, { code: 'invalid_request', message: '空间名称需要 1 到 32 个字符' }) });
    renderWithProviders(<WorkspaceSwitcher collapsed={false} go={() => {}} />);
    await open();
    await userEvent.click(screen.getByRole('menuitem', { name: '新建空间' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText(/名称/), '王家');
    await userEvent.click(within(dialog).getByRole('radio', { name: '团队' }));
    await userEvent.click(within(dialog).getByRole('button', { name: '创建空间' }));
    expect(await within(dialog).findByText('空间名称需要 1 到 32 个字符')).toBeInTheDocument();
  });

  it('只有空间管理员能看到“空间设置”', async () => {
    backend([personal, home, studio]);
    setWorkspaceId(6); // 在工作室里是“成员”
    renderWithProviders(<WorkspaceSwitcher collapsed={false} go={() => {}} />);
    await screen.findByRole('button', { name: /当前空间：某某工作室/ });
    await open();
    expect(screen.getByRole('menuitem', { name: '空间成员' })).toBeInTheDocument();
    expect(screen.queryByRole('menuitem', { name: '空间设置' })).not.toBeInTheDocument();
  });

  it('空间设置：改名；删除需要输入空间名称，删除后回到个人空间', async () => {
    const list = [personal, home];
    const calls = backend(list, {
      'PATCH /api/v1/workspaces/5': () => jsonResponse(200, { ...home, name: '王家大院' }),
      'DELETE /api/v1/workspaces/5': () => { list.pop(); return new Response(null, { status: 204 }); },
    });
    setWorkspaceId(5);
    renderWithProviders(<WorkspaceSwitcher collapsed={false} go={() => {}} />);
    await screen.findByRole('button', { name: /当前空间：王家/ });
    await open();
    await userEvent.click(screen.getByRole('menuitem', { name: '空间设置' }));
    const dialog = await screen.findByRole('dialog');

    const name = within(dialog).getByLabelText(/名称/);
    await userEvent.clear(name);
    await userEvent.type(name, '王家大院');
    await userEvent.click(within(dialog).getByRole('button', { name: '保存' }));
    const patch = await vi.waitUntil(() => calls.find((r) => r.method === 'PATCH'));
    expect(await patch.clone().json()).toEqual({ name: '王家大院', color: 'green' });
    // 保存后对话框关闭；重新打开再删除
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await open();
    await userEvent.click(screen.getByRole('menuitem', { name: '空间设置' }));
    const again = await screen.findByRole('dialog');

    await userEvent.click(within(again).getByRole('button', { name: '删除空间…' }));
    const confirm = within(again).getByRole('button', { name: '删除空间' });
    expect(confirm).toBeDisabled();
    await userEvent.type(within(again).getByLabelText(/输入空间名称/), '王家');
    expect(confirm).toBeEnabled();
    await userEvent.click(confirm);
    const del = await vi.waitUntil(() => calls.find((r) => r.method === 'DELETE'));
    expect(await del.clone().json()).toEqual({ name: '王家' });
    await waitFor(() => expect(getWorkspaceId()).toBeNull());
    await screen.findByRole('button', { name: /当前空间：小王 的空间/ });
  });

  it('个人空间的设置里没有删除', async () => {
    backend([personal]);
    renderWithProviders(<WorkspaceSwitcher collapsed={false} go={() => {}} />);
    await open();
    await userEvent.click(screen.getByRole('menuitem', { name: '空间设置' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).queryByRole('button', { name: '删除空间…' })).not.toBeInTheDocument();
  });
});

import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { getWorkspaceId, setCsrfToken, setWorkspaceId } from '../api/client';
import { MoveDialog } from './move-dialog';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const personal = { id: 1, name: '小王的空间', kind: 'personal', color: 'blue', role: 'owner', memberCount: 1 };
const home = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 3 };
const studio = { id: 9, name: '某某工作室', kind: 'team', color: 'orange', role: 'viewer', memberCount: 2 };

afterEach(() => { vi.unstubAllGlobals(); setWorkspaceId(null); setCsrfToken(null); });

describe('移到其他空间', () => {
  it('默认选个人空间，只列可以写入的空间；移完说明结果，可以切过去看', async () => {
    setWorkspaceId(5);
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: home, csrfToken: 'tok' }),
      'GET /api/v1/workspaces': () => jsonResponse(200, [personal, home, studio]),
      'POST /api/v1/documents/move': () => jsonResponse(200, { moved: 1, workspaceId: 1, workspaceName: '小王的空间', asnCleared: [7], created: ['标签“医疗”'] }),
    });
    const onClose = vi.fn();
    renderWithProviders(<Routes><Route path="*" element={<MoveDialog open docs={[{ id: 7, title: '2024 年体检报告' }]} onClose={onClose} />} /></Routes>);
    const mine = await screen.findByRole('radio', { name: /小王的空间/ });
    await waitFor(() => expect(mine).toBeChecked());
    expect(screen.queryByRole('radio', { name: /王家/ })).not.toBeInTheDocument(); // the current one
    expect(screen.queryByRole('radio', { name: /某某工作室/ })).not.toBeInTheDocument(); // only looks there
    await userEvent.click(screen.getByRole('button', { name: '移过去' }));
    await waitFor(() => expect(calls.find((r) => r.method === 'POST')).toBeTruthy());
    expect(await calls.find((r) => r.method === 'POST')!.clone().json()).toEqual({ ids: [7], workspaceId: 1 });
    expect(await screen.findByText('“2024 年体检报告”已移到“小王的空间”。')).toBeInTheDocument();
    expect(screen.getByText('在“小王的空间”里新建了标签“医疗”。')).toBeInTheDocument();
    expect(screen.getByText(/原件编号.*已清空/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '去看看' }));
    expect(onClose).toHaveBeenCalled();
    await waitFor(() => expect(getWorkspaceId()).toBeNull()); // the personal space has no header
  });

  it('移动失败时在对话框里说明', async () => {
    setWorkspaceId(5);
    stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: home, csrfToken: 'tok' }),
      'GET /api/v1/workspaces': () => jsonResponse(200, [personal, home]),
      'POST /api/v1/documents/move': () => jsonResponse(409, { code: 'duplicate', message: '空间“小王的空间”里已经有同一个文件：《体检报告》' }),
    });
    renderWithProviders(<Routes><Route path="*" element={<MoveDialog open docs={[{ id: 7, title: '体检报告' }]} onClose={() => {}} />} /></Routes>);
    await waitFor(() => expect(screen.getByRole('radio', { name: /小王的空间/ })).toBeChecked());
    await userEvent.click(screen.getByRole('button', { name: '移过去' }));
    expect(await screen.findByText('空间“小王的空间”里已经有同一个文件：《体检报告》')).toBeInTheDocument();
  });
});

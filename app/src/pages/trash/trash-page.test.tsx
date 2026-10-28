import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setWorkspaceId } from '../../api/client';
import TrashPage from './trash-page';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const ws = (role: string) => ({ id: 5, name: '王家', kind: 'family', color: 'green', role, memberCount: 2 });
const rows = [
  { id: 1, title: '2022年12月燃气账单（重复）', deletedAt: '2024-09-20T08:00:00Z', daysLeft: 18 },
  { id: 2, title: '扫描件 2024-08-01', deletedAt: '2024-09-02T08:00:00Z', daysLeft: 1 },
];

function backend(role: string, list = rows) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws(role), csrfToken: 'tok' }),
    'GET /api/v1/trash': () => jsonResponse(200, list),
    'POST /api/v1/documents/1/restore': () => new Response(null, { status: 204 }),
    'GET /api/v1/document-types': () => jsonResponse(200, []),
    'GET /api/v1/correspondents': () => jsonResponse(200, []),
    'GET /api/v1/tags': () => jsonResponse(200, []),
  });
}

afterEach(() => { vi.unstubAllGlobals(); setWorkspaceId(null); });

describe('回收站', () => {
  it('显示还剩几天；成员可以恢复，但不能彻底删除', async () => {
    const calls = backend('editor');
    renderWithProviders(<TrashPage />);
    expect(await screen.findByText('还剩 18 天自动清除')).toBeInTheDocument();
    expect(screen.getByText('还剩 1 天自动清除')).toBeInTheDocument();
    expect(screen.getByText(/只有空间管理员可以彻底删除/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '彻底删除' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '清空回收站' })).not.toBeInTheDocument();
    await userEvent.click(screen.getAllByRole('button', { name: '恢复' })[0]);
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/documents/1/restore'))).toBe(true));
  });

  it('空间管理员可以彻底删除和清空', async () => {
    backend('owner');
    renderWithProviders(<TrashPage />);
    expect(await screen.findAllByRole('button', { name: '彻底删除' })).toHaveLength(2);
    expect(screen.getByRole('button', { name: '清空回收站' })).toBeInTheDocument();
  });

  it('只看的成员只能看；空的回收站', async () => {
    backend('viewer', []);
    renderWithProviders(<TrashPage />);
    expect(await screen.findByText('回收站是空的')).toBeInTheDocument();
  });
});

import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { Route, Routes, useLocation } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import TasksPage, { OldTasksRedirect } from './tasks-page';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const ws = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 2 };

function Where() {
  const l = useLocation();
  return <div data-testid="where">{l.pathname + l.search}</div>;
}

afterEach(() => { vi.unstubAllGlobals(); });

describe('任务记录页（v1.7.18）', () => {
  it('旧地址 /settings/tasks 带着筛选条件跳到 /tasks', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws, csrfToken: 't' }),
      'GET /api/v1/tasks': () => jsonResponse(200, []),
    });
    renderWithProviders(
      <Routes>
        <Route path="/tasks" element={<><TasksPage /><Where /></>} />
        <Route path="/settings/tasks" element={<OldTasksRedirect />} />
      </Routes>,
      { route: '/settings/tasks?group=storage' },
    );
    expect(await screen.findByTestId('where')).toHaveTextContent('/tasks?group=storage');
    expect(await screen.findByText('没有符合条件的任务')).toBeInTheDocument();
    const q = new URL(calls.find((r) => new URL(r.url).pathname === '/api/v1/tasks')!.url).searchParams;
    expect(q.get('group')).toBe('storage');
  });
});

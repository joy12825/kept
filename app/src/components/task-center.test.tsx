import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes, useLocation } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { TaskButton } from './task-center';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const ws = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'owner', memberCount: 2 };
const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString();

function Where() {
  const l = useLocation();
  return <div data-testid="where">{l.pathname + l.search}</div>;
}

afterEach(() => { vi.unstubAllGlobals(); });

describe('TaskButton（v1.7.14）', () => {
  it('有进行中的任务时显示数量；点开看进度和最近的任务，进入任务记录', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws, csrfToken: 't' }),
      'GET /api/v1/tasks': () => jsonResponse(200, [
        { id: 4, kind: 'storage.migrate', label: '移动文档', group: 'storage', status: 'running', summary: '204/812 份', progress: 25, attempts: 1, creatorName: '小王', createdAt: ago(1) },
        { id: 3, kind: 'ingest.document', label: '上传', group: 'upload', status: 'queued', summary: '电费.pdf', attempts: 0, createdAt: ago(1) },
        { id: 2, kind: 'ingest.document', label: '上传', group: 'upload', status: 'succeeded', summary: '水费.pdf', documentId: 9, attempts: 1, creatorName: '小王', createdAt: ago(30) },
        { id: 1, kind: 'ingest.document', label: '上传', group: 'upload', status: 'failed', summary: 'x.pdf', error: '文件是空的', attempts: 1, createdAt: ago(60) },
      ]),
    });
    renderWithProviders(<Routes><Route path="*" element={<><TaskButton /><Where /></>} /></Routes>);
    const btn = await screen.findByRole('button', { name: '任务，2 个进行中' });
    // 进行中时图标的横条动起来（v1.7.17）
    expect(btn.querySelector('svg[data-busy]')).not.toBeNull();
    const q = new URL(calls.find((r) => new URL(r.url).pathname === '/api/v1/tasks')!.url).searchParams;
    expect([q.get('days'), q.get('limit'), q.get('people')]).toEqual(['7', '20', 'true']);

    await userEvent.click(btn);
    const panel = await screen.findByRole('dialog', { name: '任务' });
    expect(within(panel).getByRole('progressbar', { name: '移动文档 25%' })).toBeInTheDocument();
    expect(within(panel).getByText('25%')).toBeInTheDocument();
    expect(within(panel).getByRole('progressbar', { name: '上传排队中' })).toBeInTheDocument();
    expect(within(panel).getByText('文件是空的')).toBeInTheDocument();
    expect(within(panel).getByText('已完成')).toBeInTheDocument();

    await userEvent.click(within(panel).getByText(/水费\.pdf/));
    expect(screen.getByTestId('where')).toHaveTextContent('/docs/9');
    await userEvent.click(btn);
    await userEvent.click(within(await screen.findByRole('dialog', { name: '任务' })).getByRole('button', { name: '查看全部任务' }));
    expect(screen.getByTestId('where')).toHaveTextContent('/tasks');
  });

  it('没有任务时说明会出现什么', async () => {
    stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws, csrfToken: 't' }),
      'GET /api/v1/tasks': () => jsonResponse(200, []),
    });
    renderWithProviders(<TaskButton />);
    const idle = await screen.findByRole('button', { name: '任务' });
    expect(idle.querySelector('svg')).not.toBeNull();
    expect(idle.querySelector('svg[data-busy]')).toBeNull();
    await userEvent.click(idle);
    expect(await screen.findByText(/最近 7 天没有任务/)).toBeInTheDocument();
  });
});

describe('失败的上传（v1.9.12）', () => {
  it('文件还在时“重试”；超过 7 天写明要重新上传', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws, csrfToken: 't' }),
      'GET /api/v1/tasks': () => jsonResponse(200, [
        { id: 6, kind: 'ingest.document', label: '上传', group: 'upload', status: 'failed', summary: '扫描.pdf', error: '处理失败，请稍后重试', retryable: true, attempts: 3, creatorName: '小王', createdAt: ago(5) },
        { id: 5, kind: 'ingest.document', label: '上传', group: 'upload', status: 'failed', summary: '旧.pdf', error: '处理失败，请稍后重试', retryable: false, expired: true, attempts: 3, creatorName: '小王', createdAt: ago(60 * 24 * 8) },
      ]),
      'POST /api/v1/jobs/6/retry': () => new Response(null, { status: 204 }),
    });
    renderWithProviders(<TaskButton />);
    await userEvent.click(await screen.findByRole('button', { name: /任务/ }));
    const panel = await screen.findByRole('dialog');
    expect(within(panel).getByText('文件已清理（失败的上传保留 7 天），请重新上传')).toBeInTheDocument();
    await userEvent.click(within(panel).getByRole('button', { name: '重试' }));
    expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/api/v1/jobs/6/retry'))).toBe(true);
  });
});

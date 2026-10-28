import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken, setWorkspaceId, type Workspace } from '../../api/client';
import { bytes, LogsPanel, SystemPanel, TasksPanel, uptime } from './system';
import { settingsTabs } from '../settings';

const home: Workspace = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 2 };
const user = { id: 1, username: 'wang', displayName: '小王', email: 'wang@example.com', isAdmin: true, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const me = () => jsonResponse(200, { user, workspace: home, csrfToken: 'tok-1' });
const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString();
const query = (calls: Request[], path: string) => calls.filter((r) => new URL(r.url).pathname === path).map((r) => new URL(r.url).searchParams);
const last = <T,>(xs: T[]) => xs[xs.length - 1];

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

describe('TasksPanel', () => {
  it('列出任务、失败原因和文档链接，按状态筛选', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': me,
      'GET /api/v1/tasks': () => jsonResponse(200, [
        { id: 2, kind: 'document.process', label: '处理上传的文档', status: 'failed', summary: '电费.pdf', error: 'ocrmypdf 退出码 2', attempts: 3, documentId: 9, createdAt: ago(5) },
        { id: 1, kind: 'drawer.reassign', label: '整理抽屉', status: 'succeeded', summary: '共 95 份，改动 3 份', attempts: 1, createdAt: ago(90) },
      ]),
    });
    renderWithProviders(<TasksPanel />);
    expect(await screen.findByText('ocrmypdf 退出码 2')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '电费.pdf' })).toHaveAttribute('href', '/docs/9');
    expect(screen.getByText('已尝试 3 次')).toBeInTheDocument();
    expect(screen.getByText('失败')).toBeInTheDocument();
    expect(screen.getByText(/共 95 份，改动 3 份/)).toBeInTheDocument();
    expect(query(calls, '/api/v1/tasks')[0].get('days')).toBe('30');

    await userEvent.click(screen.getByRole('combobox', { name: '状态' }));
    await userEvent.click(screen.getByRole('option', { name: '失败' }));
    await waitFor(() => expect(last(query(calls, '/api/v1/tasks'))?.get('status')).toBe('failed'));
  });

  it('按种类筛选、只看我的；进行中的显示进度和发起人（v1.7.14）', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': me,
      'GET /api/v1/tasks': () => jsonResponse(200, [
        { id: 3, kind: 'storage.migrate', label: '移动文档', group: 'storage', status: 'running', summary: '204/812 份', progress: 25, attempts: 1, creatorName: '小王', createdAt: ago(1) },
      ]),
    });
    renderWithProviders(<TasksPanel />, { route: '/tasks?group=storage' });
    expect(await screen.findByRole('progressbar', { name: '移动文档 25%' })).toBeInTheDocument();
    expect(screen.getByText('小王')).toBeInTheDocument();
    expect(query(calls, '/api/v1/tasks')[0].get('group')).toBe('storage');
    expect(screen.getByRole('combobox', { name: '种类' })).toHaveTextContent('存储');

    await userEvent.click(screen.getByRole('switch', { name: '只看我的' }));
    await waitFor(() => expect(last(query(calls, '/api/v1/tasks'))?.get('mine')).toBe('true'));
    await userEvent.click(screen.getByRole('combobox', { name: '种类' }));
    await userEvent.click(screen.getByRole('option', { name: '上传' }));
    await waitFor(() => expect(last(query(calls, '/api/v1/tasks'))?.get('group')).toBe('upload'));
  });

  it('没有任务时说明会出现什么', async () => {
    stubFetch({ 'GET /api/v1/me': me, 'GET /api/v1/tasks': () => jsonResponse(200, []) });
    renderWithProviders(<TasksPanel />);
    expect(await screen.findByText('最近没有后台任务')).toBeInTheDocument();
  });
});

describe('SystemPanel', () => {
  const status = {
    version: '0.1.0', commit: 'abcdef1234567', buildTime: '2026-10-02T08:00:00Z', goVersion: 'go1.25.0', startedAt: ago(3 * 60 + 5),
    storage: { database: 3 * 1024 * 1024, index: 1024, media: 2 * 1024 ** 3, backups: 0, free: 1024 ** 3 },
    users: 2, workspaces: 3, documents: 120, trash: 4, indexDocuments: 120, indexBehind: 0, indexReady: true,
    jobsQueued: 0, jobsRunning: 1, jobsFailed: 2,
    tools: [{ name: 'ocrmypdf', available: true, version: '16.0.0', purpose: '给扫描件加文字层' }, { name: 'gotenberg', available: false, purpose: '转换 Office 文档' }],
    integrity: [{ workspaceId: 5, workspaceName: '王家', documents: 120, missingCount: 1, orphanCount: 0, missingFiles: [{ documentId: 7, title: '租房合同', file: 'originals/0007.pdf' }], checkedAt: ago(60) }],
    settings: { KEPT_GOTENBERG_URL: '' },
    backup: { snapshots: 0 },
    watch: { folders: [], waiting: 0, failed: 0 },
    smtp: { configured: false },
  };

  it('显示版本、存储、工具和完整性检查的问题', async () => {
    stubFetch({ 'GET /api/v1/admin/system': () => jsonResponse(200, status) });
    renderWithProviders(<SystemPanel />);
    expect(await screen.findByText('0.1.0（abcdef12）')).toBeInTheDocument();
    expect(screen.getByText('3 小时 5 分钟')).toBeInTheDocument();
    expect(screen.getByText('120 份，回收站 4 份')).toBeInTheDocument();
    expect(screen.getByText('2.0 GB')).toBeInTheDocument();
    expect(screen.getByText('可用 · 16.0.0')).toBeInTheDocument();
    expect(screen.getByText('不可用')).toBeInTheDocument();
    expect(screen.getByText('1 份文档缺少文件')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '租房合同' })).toHaveAttribute('href', '/docs/7');
    expect(screen.getByText('（未设置）')).toBeInTheDocument();
    expect(screen.getByText(/没有设置每天备份/)).toBeInTheDocument();
    expect(screen.getByText(/没有配置监听目录/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '立即备份' })).not.toBeInTheDocument();
  });

  it('监听目录：各空间文件夹、等待中与未能入库', async () => {
    const watch = { dir: '/consume', folders: [{ workspaceId: 5, workspaceName: '王家', name: '5-王家' }], lastScan: new Date().toISOString(), waiting: 2, failed: 1 };
    stubFetch({ 'GET /api/v1/admin/system': () => jsonResponse(200, { ...status, watch }) });
    renderWithProviders(<SystemPanel />);
    expect(await screen.findByText('/consume')).toBeInTheDocument();
    expect(screen.getByText('5-王家/')).toBeInTheDocument();
    expect(screen.getByText('2 个文件')).toBeInTheDocument();
    expect(screen.getByText(/1 个文件，原因写在 _未能入库/)).toBeInTheDocument();
  });

  it('监听目录：同一路径再次放入的文件可以作为新版本（v1.7.13）', async () => {
    let saved: unknown;
    const watch = { dir: '/consume', folders: [], waiting: 0, failed: 0 };
    stubFetch({
      'GET /api/v1/admin/system': () => jsonResponse(200, { ...status, watch }),
      'PUT /api/v1/admin/watch': async (req) => { saved = await req.json(); return new Response(null, { status: 204 }); },
    });
    renderWithProviders(<SystemPanel />);
    const sw = await screen.findByRole('switch', { name: /作为新版本/ });
    expect(sw).not.toBeChecked();
    await userEvent.click(sw);
    await waitFor(() => expect(saved).toEqual({ versions: true }));
  });

  it('备份：目录、最近一次的结果，立即备份', async () => {
    const backup = {
      dir: '/backups', snapshots: 3,
      last: { status: 'succeeded', at: '2026-10-02T18:30:00Z', snapshot: 'app-20261003-023000.db', documents: 120, files: 236, copied: 4, bytes: 2 * 1024 * 1024, missing: 1 },
    };
    const calls = stubFetch({
      'GET /api/v1/admin/system': () => jsonResponse(200, { ...status, backup }),
      'POST /api/v1/admin/backup': () => jsonResponse(202, { jobId: 9 }),
    });
    renderWithProviders(<SystemPanel />);
    expect(await screen.findByText('/backups')).toBeInTheDocument();
    expect(screen.getByText('3 份（保留最近 7 份）')).toBeInTheDocument();
    expect(screen.getByText('120 份文档、236 个文件，这次复制 4 个（2.0 MB）')).toBeInTheDocument();
    expect(screen.getByText('有 1 个文件在数据目录中缺失，没有备份')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '立即备份' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && new URL(r.url).pathname === '/api/v1/admin/backup')).toBe(true));
  });

  it('没有配置时说明怎么配置', async () => {
    stubFetch({ 'GET /api/v1/admin/system': () => jsonResponse(200, status) });
    renderWithProviders(<SystemPanel />);
    expect(await screen.findByText(/没有配置发信/)).toBeInTheDocument();
  });

  it('显示设置，发送测试邮件，失败时写明原因', async () => {
    const smtp = { configured: true, host: 'smtp.example.com', port: 465, security: 'tls', username: 'kept', from: '收好 <kept@example.com>', maxAttachments: 20 * 1024 * 1024 };
    let fail = true;
    const calls = stubFetch({
      'GET /api/v1/me': me,
      'GET /api/v1/admin/system': () => jsonResponse(200, { ...status, smtp }),
      'POST /api/v1/admin/smtp/test': () => (fail ? jsonResponse(400, { code: 'smtp_failed', message: '登录邮件服务器失败，请检查用户名和密码：535' }) : new Response(null, { status: 204 })),
    });
    renderWithProviders(<SystemPanel />);
    expect(await screen.findByText('smtp.example.com:465')).toBeInTheDocument();
    expect(screen.getByText('TLS（SSL）')).toBeInTheDocument();
    expect(screen.getByText('每封邮件 20.0 MB')).toBeInTheDocument();
    const to = screen.getByRole('textbox', { name: '测试收件人' });
    await userEvent.clear(to);
    await userEvent.type(to, 'wang@example.com');
    await userEvent.click(screen.getByRole('button', { name: '发送测试邮件' }));
    expect(await screen.findByText(/登录邮件服务器失败/)).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole('button', { name: '发送测试邮件' }));
    expect(await screen.findByText('已发送到 wang@example.com，请查收')).toBeInTheDocument();
    const body = await calls.filter((r) => r.method === 'POST').pop()!.clone().json();
    expect(body).toEqual({ to: 'wang@example.com' });
  });
});

describe('LogsPanel', () => {
  it('列出日志，按级别和关键字筛选', async () => {
    const calls = stubFetch({
      'GET /api/v1/admin/logs': () => jsonResponse(200, [
        { time: ago(1), level: 'ERROR', message: 'job failed', attrs: { kind: 'document.process', err: 'boom' } },
        { time: ago(2), level: 'INFO', message: 'kept started', attrs: {} },
      ]),
    });
    renderWithProviders(<LogsPanel />);
    expect(await screen.findByText('job failed')).toBeInTheDocument();
    expect(screen.getByText('kind=document.process err=boom')).toBeInTheDocument();
    expect(screen.getByText('错误')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('combobox', { name: '级别' }));
    await userEvent.click(screen.getByRole('option', { name: '警告及以上' }));
    await userEvent.type(screen.getByLabelText('关键字'), 'job');
    await waitFor(() => {
      const q = last(query(calls, '/api/v1/admin/logs'));
      expect(q.get('level')).toBe('warn');
      expect(q.get('q')).toBe('job');
    });
  });
});

describe('格式', () => {
  it('字节数与运行时长', () => {
    expect(bytes(-1)).toBe('未知');
    expect(bytes(512)).toBe('512 B');
    expect(bytes(1536)).toBe('1.5 KB');
    expect(bytes(200 * 1024 ** 3)).toBe('200 GB');
    const now = Date.parse('2026-10-02T12:00:00Z');
    expect(uptime('2026-10-02T11:55:00Z', now)).toBe('5 分钟');
    expect(uptime('2026-09-29T08:00:00Z', now)).toBe('3 天 4 小时');
  });
});

describe('设置页签', () => {
  it('系统状态和日志只给实例管理员；任务记录不在设置里（v1.7.18 起是单独的页面）', () => {
    const keys = (isAdmin: boolean) => settingsTabs({ isAdmin, role: 'viewer' }).map(([k]) => k);
    expect(keys(true)).toEqual(expect.arrayContaining(['system', 'logs']));
    expect(keys(true)).not.toContain('tasks');
    expect(keys(false)).not.toContain('system');
    expect(keys(false)).not.toContain('logs');
  });
});

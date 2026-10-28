import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { setCsrfToken } from '../api/client';
import HomePage from './home';
import { Toast } from '../components/overlays';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const me = (role = 'editor') => ({ user, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role, memberCount: 2 }, csrfToken: 'tok-1' });
const house = {
  id: 1, name: '房子', icon: 'home', color: 'blue', description: '', documentCount: 51, alertCount: 1, createdAt: '2024-01-01T00:00:00Z',
  primary: { fact: { id: 9, drawerId: 1, kind: 'due', label: '租约到期', config: {} }, warn: false, dueDate: '2025-08-31', daysLeft: 327, document: { id: 3, title: '房屋租赁合同', date: '2023-09-01' } },
};

function backend(drawers: object[], extra: Parameters<typeof stubFetch>[0] = {}) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, me()),
    'GET /api/v1/drawers': () => jsonResponse(200, { items: drawers, unfiled: 3 }),
    'GET /api/v1/alerts': () => jsonResponse(200, [{ id: 7, drawerId: 1, factId: 9, kind: 'missing', text: '电费账单：10 月的还没有存入', warn: true, createdAt: '2024-10-08T00:00:00Z' }]),
    'GET /api/v1/documents': () => jsonResponse(200, { items: [], total: 2 }),
    'GET /api/v1/document-types': () => jsonResponse(200, []),
    'GET /api/v1/correspondents': () => jsonResponse(200, []),
    'GET /api/v1/tags': () => jsonResponse(200, []),
    'POST /api/v1/alerts/7/dismiss': () => new Response(null, { status: 204 }),
    'POST /api/v1/alerts/7/restore': () => new Response(null, { status: 204 }),
    ...extra,
  });
}

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); });

describe('首页', () => {
  it('提醒、抽屉卡片的首要要点和未入抽屉；忽略提醒可以撤销', async () => {
    const calls = backend([house]);
    renderWithProviders(<><HomePage /><Toast /></>);
    expect(await screen.findByText('电费账单：10 月的还没有存入')).toBeInTheDocument();
    const card = screen.getByRole('region', { name: '我的抽屉' });
    expect(within(card).getByText('2025-08-31')).toBeInTheDocument();
    expect(within(card).getByText('还剩 327 天 · 房屋租赁合同')).toBeInTheDocument();
    expect(within(card).getByLabelText('有提醒')).toBeInTheDocument();
    expect(screen.getByText('还有 3 份文档未入抽屉')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '忽略' }));
    await userEvent.click(await screen.findByRole('button', { name: '撤销' }));
    await vi.waitUntil(() => calls.find((r) => r.url.endsWith('/alerts/7/restore')));
    expect(calls.some((r) => r.url.endsWith('/alerts/7/dismiss'))).toBe(true);
  });

  it('还没有抽屉：按场景勾选模板一次建好', async () => {
    const keys: string[][] = [];
    backend([], {
      'GET /api/v1/drawer-templates': () => jsonResponse(200, {
        scenes: [{ key: 'family', name: '家庭', icon: 'home', description: '家庭事务', templates: ['house', 'car'] }],
        templates: [
          { key: 'house', name: '房子', icon: 'home', color: 'blue', description: '租房买房', facts: ['合同到期'] },
          { key: 'car', name: '车', icon: 'car', color: 'green', description: '车险年检', facts: [] },
        ],
      }),
      'POST /api/v1/drawers/from-templates': async (req) => { keys.push(((await req.json()) as { keys: string[] }).keys); return jsonResponse(201, { drawers: [], jobId: 1 }); },
    });
    renderWithProviders(<HomePage />);
    await userEvent.click(await screen.findByText('家庭'));
    await userEvent.click(screen.getByRole('checkbox', { name: /车/ }));
    await userEvent.click(screen.getByRole('button', { name: '创建 1 个抽屉' }));
    await vi.waitUntil(() => keys.length > 0);
    expect(keys[0]).toEqual(['house']);
  });

  it('没有抽屉但有文档：首页照样显示待整理、最近添加，抽屉区里选场景；有上手清单', async () => {
    backend([], {
      'GET /api/v1/alerts': () => jsonResponse(200, []),
      'GET /api/v1/devices': () => jsonResponse(200, []),
      'GET /api/v1/documents': () => jsonResponse(200, { items: [{ id: 3, title: '9 月电费账单', createdDate: '2026-09-28', addedAt: '2026-10-01T00:00:00Z', modifiedAt: '2026-10-01T00:00:00Z', mimeType: 'application/pdf', originalFilename: 'a.pdf', pageCount: 1, hasArchive: true, hasThumbnail: true, inbox: true, tagIds: [], noteCount: 0, drawerManual: false, isMilestone: false, isBook: false }], total: 1 }),
      'GET /api/v1/drawer-templates': () => jsonResponse(200, { scenes: [{ key: 'family', name: '家庭', icon: 'home', description: '家庭事务', templates: ['house'] }], templates: [{ key: 'house', name: '房子', icon: 'home', color: 'blue', description: '', facts: [] }] }),
    });
    renderWithProviders(<HomePage />);
    expect(await screen.findByText('9 月电费账单')).toBeInTheDocument();
    expect(screen.getByText('1 份文档待整理')).toBeInTheDocument();
    const drawers = screen.getByRole('region', { name: '我的抽屉' });
    expect(await within(drawers).findByText('家庭')).toBeInTheDocument();
    const start = screen.getByRole('region', { name: '开始使用' });
    expect(within(start).getByText('1 / 3')).toBeInTheDocument(); // 已有文档，上传这一步算完成
    expect(screen.queryByRole('button', { name: '上传文档' })).not.toBeInTheDocument(); // 已经有文档，不显示大的上传区
  });

  it('已有抽屉：按场景添加，已建过的模板标为已有，只创建还没有的', async () => {
    const keys: string[][] = [];
    backend([{ ...house, templateKey: 'house' }], {
      'GET /api/v1/drawer-templates': () => jsonResponse(200, {
        scenes: [{ key: 'family', name: '家庭', icon: 'home', description: '家庭事务', templates: ['house', 'car'] }],
        templates: [
          { key: 'house', name: '房子', icon: 'home', color: 'blue', description: '租房买房', facts: [] },
          { key: 'car', name: '车', icon: 'car', color: 'green', description: '车险年检', facts: [] },
        ],
      }),
      'POST /api/v1/drawers/from-templates': async (req) => { keys.push(((await req.json()) as { keys: string[] }).keys); return jsonResponse(201, { drawers: [], jobId: 1 }); },
    });
    renderWithProviders(<><HomePage /><Toast /></>);
    await userEvent.click(await screen.findByRole('button', { name: '按场景添加' }));
    expect(await screen.findByText('已有 1 / 2 个')).toBeInTheDocument();
    await userEvent.click(screen.getByText('家庭'));
    expect(screen.getByText('已有')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '创建 1 个抽屉' }));
    await vi.waitUntil(() => keys.length > 0);
    expect(keys[0]).toEqual(['car']);
    expect(await screen.findByRole('button', { name: '按场景添加' })).toBeInTheDocument();
  });

  it('提醒下方显示抽屉管家写的说明，带 AI 标记', async () => {
    backend([house], {
      'GET /api/v1/alerts': () => jsonResponse(200, [{ id: 7, drawerId: 1, factId: 9, kind: 'missing', text: '电费账单：10 月的还没有存入', note: '10 月电费还没存，可以在网上国网下载电子账单。', warn: true, createdAt: '2024-10-08T00:00:00Z' }]),
    });
    renderWithProviders(<HomePage />);
    expect(await screen.findByText('10 月电费还没存，可以在网上国网下载电子账单。')).toBeInTheDocument();
    expect(screen.getByRole('img', { name: '抽屉管家写的说明（AI 生成）' })).toBeInTheDocument();
  });

  it('空间快满时提醒，可以去存储页', async () => {
    backend([house], {
      'GET /api/v1/storage': () => jsonResponse(200, { originals: 950, searchable: 0, thumbnails: 0, versions: 0, trash: 0, total: 950 << 20, documents: 3, quota: 1000 << 20, quotaDefault: false, countedAt: '2026-10-06T00:00:00Z' }),
    });
    renderWithProviders(<HomePage />);
    expect(await screen.findByText(/空间快满了：已用 950 MB，配额 1000 MB/)).toBeInTheDocument();
  });
});

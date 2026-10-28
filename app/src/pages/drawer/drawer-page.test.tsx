import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken } from '../../api/client';
import DrawerPage from './drawer-page';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const rule = { algorithm: 'auto', match: '', caseInsensitive: true };
const fact = (id: number, kind: string, label: string, config = {}) => ({ id, drawerId: 1, kind, label, config });
const detail = {
  id: 1, name: '房子', icon: 'home', color: 'blue', description: '', documentCount: 51, alertCount: 0, createdAt: '2024-01-01T00:00:00Z',
  rule: { all: [{ field: 'type', op: 'in', value: [3] }, { field: 'correspondent', op: 'in', value: [7] }] },
  manualCount: 1, firstYear: 2023, lastYear: 2024,
  facts: [
    { fact: fact(1, 'sum', '今年水电费', { period: 'this_year' }), warn: false, total: '3812.40', money: true, count: 18, months: [], filter: { field: 'tag', op: 'has_any', value: [30], descendants: true } },
    { fact: fact(2, 'expect', '电费账单', { period: 'monthly' }), warn: true, periodLabel: '10 月', present: false },
    { fact: fact(3, 'pinned', '固定文档'), warn: false, documents: [{ id: 8, title: '房屋租赁合同', date: '2023-09-01' }] },
  ],
};

function backend(previews: unknown[] = [], patches: unknown[] = []) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 2 }, csrfToken: 't' }),
    'GET /api/v1/drawers/1': () => jsonResponse(200, detail),
    'GET /api/v1/drawers': () => jsonResponse(200, { items: [detail], unfiled: 0 }),
    'GET /api/v1/documents': () => jsonResponse(200, { items: [], total: 0 }),
    'GET /api/v1/documents/facets': () => jsonResponse(200, {}),
    'GET /api/v1/document-types': () => jsonResponse(200, [{ id: 3, name: '账单', initial: 'Z', sortKey: 'zhangdan', rule }]),
    'GET /api/v1/correspondents': () => jsonResponse(200, [{ id: 7, name: '国家电网', initial: 'G', sortKey: 'guojiadianwang', rule }]),
    'GET /api/v1/tags': () => jsonResponse(200, []),
    'GET /api/v1/custom-fields': () => jsonResponse(200, []),
    'POST /api/v1/drawers/preview': async (req) => { previews.push(await req.json()); return jsonResponse(200, { total: 60, manual: 1, in: 2, new: 7, out: 0, unfiled: 0, moves: 9, samples: [{ documentId: 4, title: '物业费', to: 1 }] }); },
    'PATCH /api/v1/drawers/1': async (req) => { patches.push(await req.json()); return jsonResponse(200, { drawer: detail, jobId: 3 }); },
  });
}

const render = () => renderWithProviders(<Routes><Route path="/drawers/:id" element={<DrawerPage />} /></Routes>, { route: '/drawers/1' });

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); });

describe('抽屉页', () => {
  it('一句话的收纳规则和要点卡片', async () => {
    backend();
    render();
    expect(await screen.findByRole('heading', { name: '房子' })).toBeInTheDocument();
    expect(await screen.findByText(/51 份 · 2023–2024 · 收纳：类型为账单，且往来方为国家电网；另有手动放入 1 份/)).toBeInTheDocument();
    expect(within(screen.getByLabelText('要点：今年水电费（首要）')).getByText('¥3,812')).toBeInTheDocument();
    expect(within(screen.getByLabelText('要点：电费账单')).getByText('10 月未存入')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '房屋租赁合同' })).toBeInTheDocument();
  });

  it('编辑收纳规则：边改边预览，保存提交筛选条件', async () => {
    const previews: unknown[] = [];
    const patches: unknown[] = [];
    backend(previews, patches);
    render();
    await userEvent.click(await screen.findByRole('button', { name: '编辑收纳规则' }));
    expect(await screen.findByText('将收纳 60 份文档')).toBeInTheDocument();
    expect(screen.getByText(/原来未入抽屉的放进来 7 份/)).toBeInTheDocument();
    expect(previews[0]).toEqual({ drawerId: 1, rule: detail.rule });
    await userEvent.click(screen.getByRole('button', { name: '或' }));
    await userEvent.click(screen.getByRole('combobox', { name: '第 3 条的字段' }));
    await userEvent.click(screen.getByRole('option', { name: '关键词' }));
    await userEvent.type(screen.getByLabelText('第 3 条的关键词'), '物业、房租');
    await vi.waitUntil(() => JSON.stringify(previews[previews.length - 1]).includes('房租'), { timeout: 2000 });
    await userEvent.click(screen.getByRole('button', { name: '保存规则' }));
    await vi.waitUntil(() => patches.length > 0);
    expect(patches[0]).toEqual({ rule: { any: [detail.rule, { any: [{ text: '物业' }, { text: '房租' }] }] } });
  });
});

describe('同名文件（v1.7.13）', () => {
  it('修改抽屉：打开“同名文件直接作为新版本”', async () => {
    setCsrfToken('t');
    const patches: unknown[] = [];
    backend([], patches);
    render();
    await screen.findByRole('button', { name: '编辑收纳规则' });
    // 页头的“更多”在文档列表筛选条的“更多”之前
    await userEvent.click(screen.getAllByRole('button', { name: '更多' })[0]);
    await userEvent.click(await screen.findByRole('menuitem', { name: /修改抽屉/ }));
    const dialog = await screen.findByRole('dialog', { name: '修改抽屉' });
    const sw = within(dialog).getByRole('switch', { name: /同名文件直接作为新版本/ });
    expect(sw).not.toBeChecked();
    await userEvent.click(sw);
    await userEvent.click(within(dialog).getByRole('button', { name: '保存' }));
    await vi.waitFor(() => expect(patches).toEqual([{ name: '房子', icon: 'home', color: 'blue', description: '', sameNameVersions: true }]));
  });
});

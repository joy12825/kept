import { eeBuild } from '@ee';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken, setWorkspaceId, type WorkspaceRole } from '../../api/client';
import { Route, Routes } from 'react-router-dom';
import DirectoryPage from './directory-page';
import { ConfirmDialog } from '../../components/overlays';
import EntryPage from './entry-page';

const user = { id: 1, username: 'wang', displayName: '小王', email: 'wang@example.com', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const ws = (role: WorkspaceRole) => ({ id: 5, name: '王家', kind: 'family', color: 'green', role, memberCount: 2 });
const rule = (algorithm = 'auto', match = '') => ({ algorithm, match, caseInsensitive: true });
const corr = [
  { id: 1, name: '阿里云', initial: 'A', sortKey: 'aliyun', rule: rule() },
  { id: 2, name: 'Apple', initial: '#', sortKey: 'apple', rule: rule('literal', 'Apple') },
  { id: 3, name: '国家电网', initial: 'G', sortKey: 'guojiadianwang', rule: rule('any', '国家电网 国网') },
];
const tags = [
  { id: 10, name: '财务', initial: 'C', sortKey: 'caiwu', rule: rule(), color: '#3A6EA5', isInbox: false },
  { id: 11, name: '发票', initial: 'F', sortKey: 'fapiao', rule: rule(), color: '#9A6A12', isInbox: false, parentId: 10 },
  { id: 12, name: '专票', initial: 'Z', sortKey: 'zhuanpiao', rule: rule(), color: '#2F6B4F', isInbox: false, parentId: 11 },
  { id: 13, name: '税务', initial: 'S', sortKey: 'shuiwu', rule: rule(), color: '#4F6D7A', isInbox: false },
];
const fields = [
  { id: 20, key: 'due_date', name: '到期日', initial: 'D', sortKey: 'daoqiri', dataType: 'date', extra: {}, builtIn: true },
  { id: 21, key: 'amount', name: '金额', initial: 'J', sortKey: 'jine', dataType: 'monetary', extra: { currency: 'CNY' }, builtIn: true },
];

function backend(role: WorkspaceRole = 'editor', extra: Parameters<typeof stubFetch>[0] = {}) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws(role), csrfToken: 'tok-1' }),
    'GET /api/v1/correspondents': () => jsonResponse(200, corr),
    'GET /api/v1/document-types': () => jsonResponse(200, []),
    'GET /api/v1/tags': () => jsonResponse(200, tags),
    'GET /api/v1/custom-fields': () => jsonResponse(200, fields),
    'GET /api/v1/documents': () => jsonResponse(200, { items: [], total: 0 }),
    'GET /api/v1/documents/facets': () => jsonResponse(200, { year: [{ key: '2022', count: 2 }, { key: '2024', count: 5 }] }),
    ...extra,
  });
}

/** 目录页和条目页：点目录里的一行进入条目页，在那里“编辑”。 */
const renderDir = (route: string) => renderWithProviders(
  <Routes><Route path="/directory" element={<DirectoryPage />} /><Route path="/directory/:kind/:id" element={<EntryPage />} /></Routes>, { route });
const openEditor = async (name: string, button = '编辑') => {
  await userEvent.click(await screen.findByText(name));
  await screen.findByRole('heading', { level: 2 });
  await userEvent.click(screen.getByRole('button', { name: button }));
};

const rowNames = () => screen.getAllByRole('button').map((b) => b.textContent ?? '');

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

describe('目录', () => {
  it('往来方按拼音首字母分组，英文开头归入 #，并显示匹配方式', async () => {
    backend();
    renderWithProviders(<DirectoryPage />, { route: '/directory' });
    expect(await screen.findByText('国家电网')).toBeInTheDocument();
    expect(screen.getByText(/匹配：任意词/)).toBeInTheDocument();
    expect(screen.getByText(/匹配：精确匹配/)).toBeInTheDocument();
    const headings = screen.getAllByRole('heading', { level: 6 }).map((h) => h.textContent);
    expect(headings).toEqual(['A', 'G', '#']);
    expect(screen.getByRole('button', { name: '跳到 G' })).toBeEnabled();
    expect(screen.getByRole('button', { name: '跳到 B' })).toBeDisabled();
  });

  it('可以按拼音查找', async () => {
    backend();
    renderWithProviders(<DirectoryPage />, { route: '/directory' });
    await screen.findByText('国家电网');
    await userEvent.type(screen.getByRole('textbox', { name: '查找' }), 'guojia');
    expect(screen.getByText('国家电网')).toBeInTheDocument();
    expect(screen.queryByText('阿里云')).not.toBeInTheDocument();
  });

  it('标签显示为树；查找时显示完整路径', async () => {
    backend();
    renderWithProviders(<DirectoryPage />, { route: '/directory?tab=tags' });
    expect(await screen.findByText('专票')).toBeInTheDocument();
    // 子标签跟在上级后面，归在上级的首字母组里
    const order = rowNames().filter((t) => ['财务', '发票', '专票', '税务'].some((n) => t.startsWith(n)));
    expect(order.map((t) => t.slice(0, 2))).toEqual(['财务', '发票', '专票', '税务']);
    await userEvent.type(screen.getByRole('textbox', { name: '查找' }), '专票');
    expect(screen.getByText('财务 / 发票 / 专票')).toBeInTheDocument();
  });

  it('自定义字段显示类型和“内置”标记；没有类型时显示空状态', async () => {
    backend();
    renderWithProviders(<DirectoryPage />, { route: '/directory?tab=custom-fields' });
    expect(await screen.findByText('金额 · 人民币')).toBeInTheDocument();
    expect(screen.getAllByText('内置')).toHaveLength(2);
    await userEvent.click(screen.getByRole('button', { name: '类型' }));
    expect(await screen.findByText('还没有类型')).toBeInTheDocument();
  });

  it('只看的成员没有“新建”，打开的面板是只读的', async () => {
    backend('viewer');
    renderDir('/directory');
    await screen.findByText('国家电网');
    await waitFor(() => expect(screen.queryByRole('button', { name: /新建/ })).not.toBeInTheDocument());
    await openEditor('国家电网', '查看设置');
    const panel = await screen.findByRole('form', { name: '编辑往来方' });
    expect(within(panel).getByLabelText(/名称/)).toBeDisabled();
    expect(within(panel).queryByRole('button', { name: '保存' })).not.toBeInTheDocument();
  });
});

describe('条目页', () => {
  it('显示文档数、时间跨度和每年的柱状图；列表带上这一项的条件', async () => {
    const counted = corr.map((c) => (c.id === 3 ? { ...c, documentCount: 7, lastDocumentDate: '2024-03-05' } : c));
    const calls = backend('editor', { 'GET /api/v1/correspondents': () => jsonResponse(200, counted) });
    renderDir('/directory');
    expect(await screen.findByText('7 份 · 最近 2024-03-05 · 匹配：任意词')).toBeInTheDocument();
    await userEvent.click(screen.getByText('国家电网'));
    expect(await screen.findByRole('heading', { name: '国家电网' })).toBeInTheDocument();
    expect(await screen.findByText('7 份 · 2022–2024')).toBeInTheDocument();
    expect(screen.getByRole('img', { name: '每年文档数：2022 年 2 份，2023 年 0 份，2024 年 5 份' })).toBeInTheDocument();
    await waitFor(() => expect(calls.some((r) => new URL(r.url).pathname === '/api/v1/documents' && new URL(r.url).searchParams.get('filter') === '{"field":"correspondent","op":"in","value":[3]}')).toBe(true));
    // 自己这一组的筛选按钮不显示
    expect(screen.queryByRole('button', { name: '往来方' })).not.toBeInTheDocument();
  });
});

describe('编辑面板', () => {
  it('新建往来方：选择匹配方式后才需要关键词，提交名称和规则', async () => {
    const calls = backend('editor', { 'POST /api/v1/correspondents': () => jsonResponse(201, { id: 9, name: '链家', initial: 'L', sortKey: 'lianjia', rule: rule('any', '链家') }) });
    renderWithProviders(<DirectoryPage />, { route: '/directory' });
    await userEvent.click(await screen.findByRole('button', { name: '新建往来方' }));
    const panel = await screen.findByRole('form', { name: '新建往来方' });
    await userEvent.type(within(panel).getByLabelText(/名称/), '链家');
    expect(within(panel).queryByLabelText(/关键词/)).not.toBeInTheDocument();
    await userEvent.click(within(panel).getByRole('combobox', { name: /匹配方式/ }));
    await userEvent.click(screen.getByRole('option', { name: '任意词' }));
    await userEvent.type(within(panel).getByLabelText(/关键词/), '链家');
    await userEvent.click(within(panel).getByRole('button', { name: '创建' }));
    const post = await vi.waitUntil(() => calls.find((r) => r.method === 'POST'));
    expect(await post.clone().json()).toEqual({ name: '链家', rule: { algorithm: 'any', match: '链家', caseInsensitive: true } });
  });

  it('试一试：输入一段文字，用还没保存的规则测试并显示命中的词', async () => {
    const rules: unknown[] = [];
    backend('editor', {
      'POST /api/v1/classifier/test': async (req) => {
        const b = await req.json() as { rule: { match: string }; text: string };
        rules.push(b.rule);
        return jsonResponse(200, b.text.includes('国网') ? { matched: true, basis: '关键词“国网”', hits: ['国网'] } : { matched: false });
      },
    });
    renderWithProviders(<DirectoryPage />, { route: '/directory' });
    await userEvent.click(await screen.findByRole('button', { name: '新建往来方' }));
    const panel = await screen.findByRole('form', { name: '新建往来方' });
    expect(within(panel).getByText(/自动学习由模型根据已整理的文档判断/)).toBeInTheDocument();
    await userEvent.click(within(panel).getByRole('combobox', { name: /匹配方式/ }));
    await userEvent.click(screen.getByRole('option', { name: '任意词' }));
    await userEvent.type(within(panel).getByLabelText(/关键词/), '国家电网 国网');
    await userEvent.type(within(panel).getByLabelText('试一试'), '国网北京 电费');
    expect(await within(panel).findByText(/匹配：关键词“国网”/)).toBeInTheDocument();
    expect(rules[rules.length - 1]).toEqual({ algorithm: 'any', match: '国家电网 国网', caseInsensitive: true });
    await userEvent.clear(within(panel).getByLabelText('试一试'));
    await userEvent.type(within(panel).getByLabelText('试一试'), '自来水');
    expect(await within(panel).findByText('不匹配')).toBeInTheDocument();
  });

  it('重名时在名称下显示服务端的说明', async () => {
    backend('editor', { 'PATCH /api/v1/correspondents/1': () => jsonResponse(409, { code: 'conflict', message: '已经有同名的往来方', details: { field: 'name' } }) });
    renderDir('/directory');
    await openEditor('阿里云');
    const panel = await screen.findByRole('form', { name: '编辑往来方' });
    const name = within(panel).getByLabelText(/名称/);
    await userEvent.clear(name);
    await userEvent.type(name, 'Apple');
    await userEvent.click(within(panel).getByRole('button', { name: '保存' }));
    expect(await within(panel).findByText('已经有同名的往来方')).toBeInTheDocument();
  });

  it('标签：上级标签不能选自己和后代；删除时写明子标签会上移', async () => {
    const calls = backend('editor', { 'DELETE /api/v1/tags/11': () => new Response(null, { status: 204 }) });
    renderDir('/directory?tab=tags');
    await openEditor('发票');
    const panel = await screen.findByRole('form', { name: '编辑标签' });
    await userEvent.click(within(panel).getByRole('combobox', { name: /上级标签/ }));
    const options = screen.getAllByRole('option').map((o) => o.textContent);
    expect(options).toContain('财务');
    expect(options).toContain('税务');
    expect(options.some((o) => o?.includes('专票') || o === '财务 / 发票')).toBe(false);
    await userEvent.keyboard('{Escape}');

    await userEvent.click(within(panel).getByRole('button', { name: '删除' }));
    const dialog = await screen.findByRole('dialog', { name: /删除标签“发票”/ });
    expect(within(dialog).getByText(/它的 1 个子标签会上移一层/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: '删除' }));
    await vi.waitUntil(() => calls.find((r) => r.method === 'DELETE'));
  });

  it('内置字段只能改名，没有删除按钮', async () => {
    backend();
    renderWithProviders(<DirectoryPage />, { route: '/directory?tab=custom-fields' });
    await userEvent.click(await screen.findByText('金额'));
    const panel = await screen.findByRole('form', { name: '编辑自定义字段' });
    expect(within(panel).getByText(/内置字段，用于金额汇总和到期提醒/)).toBeInTheDocument();
    expect(within(panel).queryByRole('button', { name: '删除' })).not.toBeInTheDocument();
    expect(within(panel).getByRole('combobox', { name: /类型/ })).toHaveAttribute('aria-disabled', 'true');
  });

  it('新建单选字段：提交选项', async () => {
    const calls = backend('editor', { 'POST /api/v1/custom-fields': () => jsonResponse(201, { ...fields[0], id: 30, key: undefined, builtIn: false }) });
    renderWithProviders(<DirectoryPage />, { route: '/directory?tab=custom-fields' });
    await userEvent.click(await screen.findByRole('button', { name: '新建自定义字段' }));
    const panel = await screen.findByRole('form', { name: '新建自定义字段' });
    await userEvent.type(within(panel).getByLabelText(/名称/), '报销状态');
    await userEvent.click(within(panel).getByRole('combobox', { name: /类型/ }));
    await userEvent.click(screen.getByRole('option', { name: '单选' }));
    await userEvent.type(within(panel).getByLabelText('选项 1'), '未报销');
    await userEvent.click(within(panel).getByRole('button', { name: '添加选项' }));
    await userEvent.type(within(panel).getByLabelText('选项 2'), '已报销');
    await userEvent.click(within(panel).getByRole('button', { name: '创建' }));
    const post = await vi.waitUntil(() => calls.find((r) => r.method === 'POST'));
    expect(await post.clone().json()).toEqual({ name: '报销状态', dataType: 'select', extra: { options: [{ label: '未报销' }, { label: '已报销' }] } });
  });

  it.skipIf(!eeBuild)('整理建议：写明匹配规则的覆盖与误中、合并的理由，可以采纳', async () => {
    const calls = backend('editor', {
      'GET /api/v1/agents/settings': () => jsonResponse(200, { enabled: true, intake: true, dailyLimit: 200, usedToday: 0 }),
      'GET /api/v1/agents/suggestions': () => jsonResponse(200, [
        { id: 7, kind: 'merge', target: 'correspondent', targetId: 3, name: '国家电网', count: 12, merged: [{ id: 4, name: '国网北京', count: 3 }], reason: '国网北京是国家电网在北京的供电公司', confidence: 0.8, createdAt: '2026-10-06T00:00:00Z' },
        { id: 8, kind: 'set_rule', target: 'correspondent', targetId: 1, name: '阿里云', count: 5, match: '阿里云', hits: 5, total: 5, others: 1, reason: '', confidence: 1, createdAt: '2026-10-06T00:00:00Z' },
      ]),
      'POST /api/v1/proposals/7/accept': () => jsonResponse(202, { jobId: 0 }),
    });
    renderWithProviders(<DirectoryPage />, { route: '/directory' });
    const box = await screen.findByLabelText('整理建议');
    expect(await within(box).findByText('把往来方“国网北京”（3 份）合并到“国家电网”（12 份）')).toBeInTheDocument();
    expect(within(box).getByText('国网北京是国家电网在北京的供电公司')).toBeInTheDocument();
    expect(within(box).getByText('往来方“阿里云”的匹配规则设为“包含任一：阿里云”')).toBeInTheDocument();
    expect(within(box).getByText('它的 5 份文档里有 5 份含有“阿里云”，另有 1 份其他文档也含有它。')).toBeInTheDocument();
    await userEvent.click(within(box).getAllByRole('button', { name: '采纳' })[0]);
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/proposals/7/accept'))).toBe(true));
  });

  it('多选往来方：合并到文档最多的一个，或批量删除（v1.5.8）', async () => {
    const counted = corr.map((c, i) => ({ ...c, documentCount: [3, 0, 8][i] }));
    const calls = backend('editor', {
      'GET /api/v1/correspondents': () => jsonResponse(200, counted),
      'POST /api/v1/directory/correspondents/merge': () => jsonResponse(200, { documents: 3 }),
      'POST /api/v1/directory/correspondents/delete': () => jsonResponse(200, { count: 1 }),
    });
    renderWithProviders(<><Routes><Route path="/directory" element={<DirectoryPage />} /></Routes><ConfirmDialog /></>, { route: '/directory?tab=correspondents' });
    await screen.findByText('阿里云');
    await userEvent.click(screen.getByRole('button', { name: '多选' }));
    await userEvent.click(screen.getByRole('checkbox', { name: /阿里云/ }));
    await userEvent.click(screen.getByRole('checkbox', { name: /国家电网/ }));
    const bar = screen.getByRole('toolbar', { name: '批量整理' });
    expect(within(bar).getByText('已选 2 个')).toBeInTheDocument();
    await userEvent.click(within(bar).getByRole('button', { name: '合并' }));
    const dlg = await screen.findByRole('dialog', { name: '合并 2 个往来方' });
    expect(within(dlg).getByRole('radio', { name: '国家电网（8 份）' })).toBeChecked();
    await userEvent.click(within(dlg).getByRole('button', { name: '合并' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/merge'))).toBe(true));
    expect(await calls.find((r) => r.url.endsWith('/merge'))!.clone().json()).toEqual({ keep: 3, merge: [1] });
    // 合并后退出多选；再选一个删除
    await waitFor(() => expect(screen.queryByRole('toolbar', { name: '批量整理' })).not.toBeInTheDocument());
    await userEvent.click(screen.getByRole('button', { name: '多选' }));
    await userEvent.click(screen.getByRole('checkbox', { name: /Apple/ }));
    expect(within(screen.getByRole('toolbar', { name: '批量整理' })).getByRole('button', { name: '合并' })).toBeDisabled();
    await userEvent.click(within(screen.getByRole('toolbar', { name: '批量整理' })).getByRole('button', { name: '删除' }));
    expect(await screen.findByText(/它们还没有文档。删除后不能恢复。/)).toBeInTheDocument();
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '删除' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/delete'))).toBe(true));
    expect(await calls.find((r) => r.url.endsWith('/delete'))!.clone().json()).toEqual({ ids: [2] });
  });

  it('只看的成员没有“多选”', async () => {
    backend('viewer');
    renderDir('/directory?tab=correspondents');
    await screen.findByText('阿里云');
    expect(screen.queryByRole('button', { name: '多选' })).not.toBeInTheDocument();
  });

  it('文本字段可以设为多行（v1.5.8）', async () => {
    const calls = backend('editor', { 'POST /api/v1/custom-fields': () => jsonResponse(201, { ...fields[0], id: 30, name: '保修条款', dataType: 'text', extra: { multiline: true }, builtIn: false }) });
    renderDir('/directory?tab=custom-fields');
    await screen.findByText('金额');
    await userEvent.click(screen.getByRole('button', { name: '新建自定义字段' }));
    await userEvent.type(await screen.findByRole('textbox', { name: '名称' }), '保修条款');
    await userEvent.click(screen.getByRole('switch', { name: /多行/ }));
    await userEvent.click(screen.getByRole('button', { name: '创建' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/custom-fields'))).toBe(true));
    expect(await calls.find((r) => r.method === 'POST')!.clone().json()).toMatchObject({ name: '保修条款', dataType: 'text', extra: { multiline: true } });
  });

  it('没有开启 AI 时不显示整理建议', async () => {
    backend();
    renderWithProviders(<DirectoryPage />, { route: '/directory' });
    await screen.findByText('国家电网');
    expect(screen.queryByText(/整理建议/)).not.toBeInTheDocument();
  });
});

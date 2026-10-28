import { eeBuild } from '@ee';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { setWorkspaceId } from '../api/client';
import DocList from './doc-list';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const ws = (role = 'editor') => ({ id: 5, name: '王家', kind: 'family', color: 'green', role, memberCount: 2 });
const rule = { algorithm: 'auto', match: '', caseInsensitive: true };
const doc = (id: number, title: string, createdDate: string, extra: object = {}) => ({
  id, title, createdDate, addedAt: '2024-10-01T08:00:00Z', modifiedAt: '2024-10-01T08:00:00Z', mimeType: 'application/pdf', originalFilename: 'x.pdf',
  pageCount: 1, hasArchive: false, hasThumbnail: false, inbox: false, tagIds: [], noteCount: 0, ...extra,
});

function backend(role = 'editor', items = [doc(1, '3月电费账单', '2024-03-05', { correspondentId: 20, documentTypeId: 10, tagIds: [31] }), doc(2, '2月燃气账单', '2024-02-03'), doc(3, '3月水费', '2024-03-01')], extra: Parameters<typeof stubFetch>[0] = {}) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws(role), csrfToken: 'tok' }),
    'GET /api/v1/documents': (req) => {
      const f = new URL(req.url).searchParams.get('filter');
      const shown = f ? items.slice(0, 1) : items;
      return jsonResponse(200, { items: shown, total: shown.length });
    },
    'GET /api/v1/documents/facets': () => jsonResponse(200, {
      type: [{ key: '10', count: 1 }, { key: 'none', count: 2 }], correspondent: [{ key: '20', count: 1 }], tag: [{ key: '31', count: 1 }],
      year: [{ key: '2024', count: 3 }], month: [{ key: '2024-03', count: 12 }, { key: '2024-02', count: 1 }],
    }),
    'GET /api/v1/document-types': () => jsonResponse(200, [{ id: 10, name: '账单', initial: 'Z', sortKey: 'zhangdan', rule }, { id: 11, name: '合同', initial: 'H', sortKey: 'hetong', rule }]),
    'GET /api/v1/correspondents': () => jsonResponse(200, [{ id: 20, name: '国家电网', initial: 'G', sortKey: 'guojiadianwang', rule }]),
    'GET /api/v1/tags': () => jsonResponse(200, [{ id: 31, name: '水电', initial: 'S', sortKey: 'shuidian', rule, color: '#2C7A7B', isInbox: false }]),
    'POST /api/v1/documents/bulk': () => jsonResponse(200, { count: 2 }),
    'POST /api/v1/saved-views': async (req) => jsonResponse(201, { id: 1, name: (await req.clone().json()).name, filter: {}, sort: '', creatorName: '小王', createdAt: '2024-10-01T08:00:00Z' }),
    ...extra,
  });
}

afterEach(() => { vi.unstubAllGlobals(); setWorkspaceId(null); });

const render = () => renderWithProviders(<DocList listKey="files" placeholder="搜索标题、正文、备注…" backLabel="全部文件" showSave />, { route: '/files' });

describe('文档列表', () => {
  it('按月分组（份数来自月份计数），显示往来方、类型和标签', async () => {
    backend();
    render();
    expect(await screen.findByText('2024 年 3 月 · 12 份')).toBeInTheDocument();
    expect(screen.getByText('2024 年 2 月 · 1 份')).toBeInTheDocument();
    const row = screen.getByText('3月电费账单').closest('[id^="doc-row-"]') as HTMLElement;
    expect(within(row).getByText('国家电网')).toBeInTheDocument();
    expect(within(row).getByText('账单')).toBeInTheDocument();
    expect(within(row).getByText('水电')).toBeInTheDocument();
  });

  it('选择显示的列：加上金额，去掉标签；恢复默认', async () => {
    backend('editor', [doc(1, '3月电费账单', '2024-03-05', { tagIds: [31], amount: '128.50' })]);
    render();
    await screen.findByText('3月电费账单');
    await userEvent.click(screen.getByRole('button', { name: '显示的列' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: '金额' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '标签' }));
    await userEvent.keyboard('{Escape}');
    const row = screen.getByText('3月电费账单').closest('[id^="doc-row-"]') as HTMLElement;
    expect(within(row).getByText('¥128.50')).toBeInTheDocument();
    expect(within(row).queryByText('水电')).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '显示的列' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: '恢复默认' }));
    await waitFor(() => expect(within(row).getByText('水电')).toBeInTheDocument());
  });

  it('标题是链接：普通点击在当前页打开，⌘ 点交给浏览器（v1.6.0）', async () => {
    backend();
    renderWithProviders(<Routes><Route path="/files" element={<DocList listKey="files" placeholder="搜索" backLabel="全部文件" />} /><Route path="/docs/:id" element={<p>详情 1</p>} /></Routes>, { route: '/files' });
    const link = await screen.findByRole('link', { name: '3月电费账单' });
    expect(link).toHaveAttribute('href', '/docs/1');
    // ⌘ 点：不在当前页跳转（浏览器会在新标签页打开）
    fireEvent.click(link, { metaKey: true });
    expect(screen.queryByText('详情 1')).not.toBeInTheDocument();
    await userEvent.click(link);
    expect(await screen.findByText('详情 1')).toBeInTheDocument();
  });

  it('点列表里的标签、往来方就只看它的文档（v1.6.0）', async () => {
    const calls = backend();
    render();
    const row = (await screen.findByText('3月电费账单')).closest('[id^="doc-row-"]') as HTMLElement;
    await userEvent.click(within(row).getByRole('button', { name: '国家电网' }));
    await waitFor(() => expect(calls.some((r) => new URL(r.url).pathname === '/api/v1/documents' && (new URL(r.url).searchParams.get('filter') ?? '').includes('"correspondent"'))).toBe(true));
    await userEvent.click(within(screen.getByText('3月电费账单').closest('[id^="doc-row-"]') as HTMLElement).getByRole('button', { name: /水电/ }));
    await waitFor(() => expect(calls.some((r) => (new URL(r.url).searchParams.get('filter') ?? '').includes('"tag"'))).toBe(true));
    expect(screen.queryByText('详情 1')).not.toBeInTheDocument(); // 没有打开文档
  });

  it('可以显示备注数和页数两列（v1.6.0）', async () => {
    backend('editor', [doc(1, '3月电费账单', '2024-03-05', { noteCount: 2, pageCount: 3 })]);
    render();
    await screen.findByText('3月电费账单');
    await userEvent.click(screen.getByRole('button', { name: '显示的列' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: '备注' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '页数' }));
    await userEvent.keyboard('{Escape}');
    const row = screen.getByText('3月电费账单').closest('[id^="doc-row-"]') as HTMLElement;
    expect(within(row).getByTitle('2 条备注')).toHaveTextContent('2');
    expect(within(row).getByText('3')).toBeInTheDocument();
  });

  it('标签可以“全部含有”“不含”或“没有标签”，类型可以“不是”（v1.6.1）', async () => {
    const calls = backend();
    render();
    await screen.findByText('3月电费账单');
    const lastFilter = () => {
      const last = [...calls].reverse().find((r) => new URL(r.url).pathname === '/api/v1/documents');
      return JSON.parse(new URL(last!.url).searchParams.get('filter') ?? 'null');
    };
    await userEvent.click(screen.getByRole('button', { name: '标签' }));
    await userEvent.click(screen.getByRole('button', { name: '不含' }));
    await userEvent.click(screen.getByRole('option', { name: /水电/ }));
    await waitFor(() => expect(lastFilter()).toEqual({ field: 'tag', op: 'has_none', value: [31], descendants: true }));
    expect(screen.getByText('不含标签：水电')).toBeInTheDocument();
    await userEvent.click(screen.getByText('没有标签'));
    await waitFor(() => expect(lastFilter()).toEqual({ field: 'tag', op: 'is_null' }));
    await userEvent.keyboard('{Escape}');
    await userEvent.click(screen.getByRole('button', { name: '类型' }));
    await userEvent.click(screen.getByRole('button', { name: '不是' }));
    await userEvent.click(screen.getByRole('option', { name: /账单/ }));
    await waitFor(() => expect(lastFilter()).toEqual({ all: [{ field: 'type', op: 'not_in', value: [10] }, { field: 'tag', op: 'is_null' }] }));
    expect(screen.getByText('类型不是：账单')).toBeInTheDocument();
  });

  it('按自定义字段筛选，以及文件格式、原件编号（v1.6.1）', async () => {
    const calls = backend(undefined, undefined, {
      'GET /api/v1/custom-fields': () => jsonResponse(200, [
        { id: 50, key: 'amount', name: '金额', initial: 'J', sortKey: 'jine', dataType: 'monetary', extra: { currency: 'CNY' }, builtIn: true },
        { id: 60, name: '保修截止', initial: 'B', sortKey: 'baoxiujiezhi', dataType: 'date', extra: {}, builtIn: false },
      ]),
    });
    render();
    await screen.findByText('3月电费账单');
    const filterOf = (r: Request) => new URL(r.url).searchParams.get('filter') ?? '';
    await userEvent.click(screen.getByRole('button', { name: '字段' }));
    const dlg = await screen.findByRole('dialog', { name: '按字段筛选' });
    expect(within(dlg).getByRole('button', { name: '添加' })).toBeDisabled();
    // 选保修截止：默认“今天起几天内”
    await userEvent.click(within(dlg).getByLabelText('字段'));
    await userEvent.click(await screen.findByRole('option', { name: '保修截止' }));
    await userEvent.type(within(dlg).getByRole('spinbutton', { name: '天数' }), '90');
    await userEvent.click(within(dlg).getByRole('button', { name: '添加' }));
    expect(await screen.findByText('保修截止今天起 90 天内')).toBeInTheDocument();
    await waitFor(() => expect(calls.some((r) => filterOf(r).includes('"within_days"'))).toBe(true));
    await userEvent.click(screen.getByRole('button', { name: '更多' }));
    const more = await screen.findByRole('dialog', { name: '更多筛选' });
    await userEvent.click(within(more).getByRole('button', { name: '图片' }));
    await userEvent.click(within(more).getByRole('button', { name: '没有' }));
    await waitFor(() => expect(calls.some((r) => filterOf(r).includes('"kind"') && filterOf(r).includes('"is_null"'))).toBe(true));
    expect(screen.getByText('文件格式：图片')).toBeInTheDocument();
    expect(screen.getByText('原件编号：没有')).toBeInTheDocument();
  });

  it('筛选按钮显示数量；选中后带上筛选条件并出现条件胶囊', async () => {
    const calls = backend();
    render();
    await screen.findByText('3月电费账单');
    await userEvent.click(screen.getByRole('button', { name: '类型' }));
    const opt = await screen.findByRole('option', { name: /账单/ });
    expect(within(opt).getByText('1')).toBeInTheDocument();
    expect(screen.getByRole('option', { name: /没有类型/ })).toBeInTheDocument();
    await userEvent.click(opt);
    await userEvent.keyboard('{Escape}');
    expect(await screen.findByText('类型：账单')).toBeInTheDocument();
    await waitFor(() => expect(calls.some((r) => r.url.includes('/api/v1/documents?') && new URL(r.url).searchParams.get('filter') === '{"field":"type","op":"in","value":[10]}')).toBe(true));
    await waitFor(() => expect(screen.queryByText('3月水费')).not.toBeInTheDocument());
    expect(screen.getByText('3月电费账单')).toBeInTheDocument();
  });

  it('多选后改标签：显示已有几份带着，一次加上、去掉', async () => {
    const calls = backend(undefined, undefined, {
      'GET /api/v1/tags': () => jsonResponse(200, [
        { id: 31, name: '水电', initial: 'S', sortKey: 'shuidian', rule, color: '#2C7A7B', isInbox: false },
        { id: 32, name: '报销', initial: 'B', sortKey: 'baoxiao', rule, color: '#3A6EA5', isInbox: false },
      ]),
    });
    render();
    await screen.findByText('3月电费账单');
    await userEvent.click(screen.getByLabelText('选择“3月电费账单”'));
    await userEvent.click(screen.getByLabelText('选择“3月水费”'));
    const bar = screen.getByRole('toolbar', { name: '批量操作' });
    expect(within(bar).getByText('已选 2 份')).toBeInTheDocument();
    await userEvent.click(within(bar).getByRole('button', { name: '标签' }));
    const dialog = await screen.findByRole('dialog', { name: '改 2 份文档的标签' });
    // 水电：只有电费账单带着，部分
    const water = within(dialog).getByRole('checkbox', { name: '水电（1 / 2 份）' });
    expect(water).toHaveAttribute('data-indeterminate', 'true');
    expect(within(dialog).getByRole('button', { name: '应用' })).toBeDisabled();
    await userEvent.click(within(dialog).getByText('报销'));                // 没有的：加上
    await userEvent.click(within(dialog).getByText('水电'));                // 部分的：先加上
    await userEvent.click(within(dialog).getByText('水电'));                // 再点：去掉
    expect(within(dialog).getByRole('checkbox', { name: '水电（将去掉）' })).not.toBeChecked();
    expect(within(dialog).getByRole('checkbox', { name: '报销（将加上）' })).toBeChecked();
    await userEvent.type(within(dialog).getByRole('textbox', { name: '搜索标签' }), 'bao');
    expect(within(dialog).queryByText('水电')).not.toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: '应用' }));
    await waitFor(() => expect(calls.find((r) => r.method === 'POST' && r.url.endsWith('/documents/bulk'))).toBeTruthy());
    const body = await calls.find((r) => r.method === 'POST')!.clone().json();
    expect(body).toEqual({ ids: [1, 3], edit: { addTagIds: [32], removeTagIds: [31] } });
    // 完成后清除选择，操作栏收起（提示消息由布局显示）
    await waitFor(() => expect(screen.queryByRole('toolbar', { name: '批量操作' })).not.toBeInTheDocument());
  });

  it('按住 Shift 点选，选中一段', async () => {
    backend();
    render();
    await screen.findByText('3月电费账单');
    const u = userEvent.setup();
    await u.click(screen.getByLabelText('选择“2月燃气账单”'));
    await u.keyboard('{Shift>}');
    await u.click(screen.getByLabelText('选择“3月电费账单”')); // 按月分组显示：3月电费、3月水费、2月燃气
    await u.keyboard('{/Shift}');
    expect(within(screen.getByRole('toolbar', { name: '批量操作' })).getByText('已选 3 份')).toBeInTheDocument();
  });

  it('批量下载可以选原件或可搜索版', async () => {
    backend();
    const hrefs: string[] = [];
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) { hrefs.push(this.href); });
    render();
    await screen.findByText('3月电费账单');
    await userEvent.click(screen.getByLabelText('选择“3月水费”'));
    await userEvent.click(within(screen.getByRole('toolbar', { name: '批量操作' })).getByRole('button', { name: '下载' }));
    expect(screen.getByRole('menuitem', { name: /^原件/ })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('menuitem', { name: /可搜索版/ }));
    expect(hrefs).toHaveLength(1);
    expect(new URL(hrefs[0]).searchParams.get('kind')).toBe('archive');
    expect(new URL(hrefs[0]).searchParams.get('ids')).toBe('3');
    click.mockRestore();
  });

  it('自定义字段可以作为列：显示值，点表头按它排序，列表带上字段的值', async () => {
    const items = [doc(1, '3月电费账单', '2024-03-05', { fieldValues: [{ fieldId: 60, value: 'b' }, { fieldId: 61, value: '2500.00' }] }), doc(3, '3月水费', '2024-03-01')];
    const calls = backend(undefined, items, {
      'GET /api/v1/custom-fields': () => jsonResponse(200, [
        { id: 50, key: 'amount', name: '金额', initial: 'J', sortKey: 'jine', dataType: 'monetary', extra: { currency: 'CNY' }, builtIn: true },
        { id: 60, name: '报销状态', initial: 'B', sortKey: 'baoxiaozhuangtai', dataType: 'select', extra: { options: [{ id: 'a', label: '待报销' }, { id: 'b', label: '已报销' }] }, builtIn: false },
        { id: 61, name: '预算', initial: 'Y', sortKey: 'yusuan', dataType: 'monetary', extra: { currency: 'USD' }, builtIn: false },
        { id: 62, name: '关联', initial: 'G', sortKey: 'guanlian', dataType: 'document_link', extra: {}, builtIn: false },
      ]),
    });
    render();
    await screen.findByText('3月电费账单');
    await userEvent.click(screen.getByRole('button', { name: '显示的列' }));
    const menu = await screen.findByRole('menu');
    expect(within(menu).getByText('自定义字段')).toBeInTheDocument();
    expect(within(menu).queryByText('关联')).not.toBeInTheDocument(); // 文档链接不能作为列
    expect(within(menu).getAllByText('金额')).toHaveLength(1);         // 内置的金额只出现一次
    await userEvent.click(within(menu).getByText('报销状态'));
    await userEvent.click(within(menu).getByText('预算'));
    await userEvent.keyboard('{Escape}');
    expect(await screen.findByText('已报销')).toBeInTheDocument();
    expect(screen.getByText('$2500.00')).toBeInTheDocument();
    const lists = () => calls.filter((r) => r.method === 'GET' && new URL(r.url).pathname === '/api/v1/documents').map((r) => new URL(r.url).searchParams);
    await waitFor(() => expect(lists().some((p) => p.get('fields') === '60,61')).toBe(true));
    await userEvent.click(screen.getByRole('button', { name: /报销状态/ }));
    await waitFor(() => expect(lists().some((p) => p.get('sort') === 'field:60' && p.get('dir') === 'asc')).toBe(true));
    // 排序菜单里也有显示着的字段
    await userEvent.click(screen.getByRole('button', { name: '排序' }));
    expect(await screen.findByRole('menuitem', { name: /预算/ })).toBeInTheDocument();
  });

  it.skipIf(!eeBuild)('开启了 AI 时可以让 AI 整理选中的文档', async () => {
    const calls = backend(undefined, undefined, {
      'GET /api/v1/agents/settings': () => jsonResponse(200, { enabled: true, intake: true, dailyLimit: 200, usedToday: 0 }),
      'POST /api/v1/agents/organize': () => jsonResponse(202, { queued: 2 }),
    });
    render();
    await screen.findByText('3月电费账单');
    await userEvent.click(screen.getByLabelText('选择“3月电费账单”'));
    await userEvent.click(screen.getByLabelText('选择“3月水费”'));
    const bar = screen.getByRole('toolbar', { name: '批量操作' });
    await userEvent.click(await within(bar).findByRole('button', { name: '让 AI 整理' }));
    const post = await vi.waitUntil(() => calls.find((r) => r.method === 'POST' && r.url.endsWith('/agents/organize')));
    expect(await post.clone().json()).toEqual({ ids: [1, 3] });
  });

  it('只看的成员只能下载，不能保存筛选和批量修改', async () => {
    backend('viewer');
    render();
    await screen.findByText('3月电费账单');
    await userEvent.click(screen.getByLabelText('选择“3月水费”'));
    const bar = screen.getByRole('toolbar', { name: '批量操作' });
    expect(within(bar).getByRole('button', { name: '下载' })).toBeInTheDocument();
    expect(within(bar).queryByRole('button', { name: '标签' })).not.toBeInTheDocument();
    expect(within(bar).queryByRole('button', { name: '重新处理' })).not.toBeInTheDocument();
    expect(within(bar).queryByRole('button', { name: '删除' })).not.toBeInTheDocument();
  });

  it('搜索后可以保存为常用筛选', async () => {
    const calls = backend();
    render();
    await screen.findByText('3月电费账单');
    await userEvent.type(screen.getByLabelText('搜索文档'), '电费');
    await userEvent.click(await screen.findByRole('button', { name: '保存筛选' }));
    const dialog = await screen.findByRole('dialog', { name: '保存筛选' });
    await userEvent.type(within(dialog).getByLabelText('名称'), '电费');
    await userEvent.click(within(dialog).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(calls.find((r) => r.method === 'POST' && r.url.endsWith('/saved-views'))).toBeTruthy());
    const body = await calls.find((r) => r.url.endsWith('/saved-views'))!.clone().json();
    expect(body).toEqual({ name: '电费', filter: { text: '电费' }, sort: 'relevance:desc', display: { view: 'list' } });
  });

  it('输入关键词时按相关度排序并显示多处高亮，清空后恢复原来的排序', async () => {
    const calls = backend('editor', [doc(1, '3月电费账单', '2024-03-05', { highlight: [{ text: '本期', hit: false }, { text: '电费', hit: true }, { text: ' 和 ', hit: false }, { text: '电费', hit: true }] })]);
    render();
    await screen.findByText('3月电费账单');
    await userEvent.type(screen.getByLabelText('搜索文档'), '电费');
    await waitFor(() => expect(calls.some((r) => new URL(r.url).searchParams.get('sort') === 'relevance')).toBe(true));
    await waitFor(() => expect(document.querySelectorAll('mark')).toHaveLength(2));
    await userEvent.click(screen.getByRole('button', { name: '排序' }));
    expect(await screen.findByRole('menuitem', { name: /相关度/ })).toBeInTheDocument();
    await userEvent.keyboard('{Escape}');
    await userEvent.click(screen.getByLabelText('清除搜索'));
    await waitFor(() => {
      const lists = calls.filter((r) => new URL(r.url).pathname === '/api/v1/documents');
      const last = lists[lists.length - 1];
      expect(new URL(last.url).searchParams.get('sort')).toBe('created');
    });
  });

  it('可以按到期日排序（没有到期日的排在最后，由服务端处理）', async () => {
    const calls = backend('editor', [doc(1, '3月电费账单', '2024-03-05')]);
    render();
    await screen.findByText('3月电费账单');
    await userEvent.click(screen.getByRole('button', { name: '排序' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: /到期日（近的在前）/ }));
    await waitFor(() => {
      const lists = calls.filter((r) => new URL(r.url).pathname === '/api/v1/documents');
      const q = new URL(lists[lists.length - 1].url).searchParams;
      expect([q.get('sort'), q.get('dir')]).toEqual(['due', 'asc']);
    });
  });

  it('日期筛选：选“上个月”按相对时间筛选，并显示胶囊', async () => {
    const calls = backend('editor', [doc(1, '3月电费账单', '2024-03-05')]);
    render();
    await screen.findByText('3月电费账单');
    await userEvent.click(screen.getAllByText('日期').find((el) => el.closest('[aria-haspopup="dialog"]'))!);
    const pop = await screen.findByLabelText('日期筛选');
    await userEvent.click(within(pop).getByText('按添加时间'));
    await userEvent.click(within(pop).getByText('上个月'));
    await waitFor(() => {
      const lists = calls.filter((r) => new URL(r.url).pathname === '/api/v1/documents');
      const filter = new URL(lists[lists.length - 1].url).searchParams.get('filter');
      expect(JSON.parse(filter!)).toEqual({ field: 'added', op: 'period', value: 'last_month' });
    });
    expect(screen.getByText('添加时间：上个月')).toBeInTheDocument();
  });

  it('鼠标停在列表的缩略图上，弹出第一页的大图', async () => {
    backend('editor', [doc(1, '3月电费账单', '2024-03-05', { hasThumbnail: true })]);
    render();
    await screen.findByText('3月电费账单');
    const thumb = document.querySelector('img[src*="/thumbnail"]')!;
    await userEvent.hover(thumb);
    expect(await screen.findByRole('img', { name: '“3月电费账单”的第一页' }, { timeout: 2000 })).toBeInTheDocument();
    await userEvent.unhover(thumb);
    await waitFor(() => expect(screen.queryByRole('img', { name: '“3月电费账单”的第一页' })).not.toBeInTheDocument());
  });

  it('搜索索引正在准备时给出提示', async () => {
    backend();
    vi.stubGlobal('fetch', vi.fn(async (req: Request) => {
      const path = new URL(req.url).pathname;
      if (path === '/api/v1/documents') return jsonResponse(503, { code: 'search_unavailable', message: '搜索索引正在重建，请稍后再试' });
      if (path === '/api/v1/me') return jsonResponse(200, { user, workspace: ws('editor'), csrfToken: 'tok' });
      return jsonResponse(200, path.endsWith('facets') ? {} : []);
    }));
    render();
    expect(await screen.findByText('搜索索引正在重建，请稍后再试。几秒后会自动重试。')).toBeInTheDocument();
  });

  it('没有文档时显示空状态', async () => {
    backend('editor', []);
    render();
    expect(await screen.findByText('还没有文档')).toBeInTheDocument();
  });
});

describe('v1.6.4：敏感文档', () => {
  it('列表里显示锁，不请求缩略图', async () => {
    backend('editor', [doc(1, '身份证', '2024-03-05', { hasThumbnail: true, sensitive: true }), doc(2, '电费', '2024-03-04', { hasThumbnail: true })]);
    const { container } = render();
    await screen.findByText('身份证');
    const row = screen.getByText('身份证').closest('[id^="doc-row-"]') as HTMLElement;
    expect(within(row).getByLabelText('敏感文档')).toBeInTheDocument();
    expect(row.querySelector('img')).toBeNull();
    const imgs = [...container.querySelectorAll('img')].map((i) => i.getAttribute('src') ?? '');
    expect(imgs.some((s) => s.includes('/documents/2/thumbnail'))).toBe(true);
    expect(imgs.some((s) => s.includes('/documents/1/thumbnail'))).toBe(false);
  });
});

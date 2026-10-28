import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setWorkspaceId } from '../../api/client';
import DetailPage from './detail-page';

vi.mock('../../components/pdf-preview', () => ({ PdfPreview: ({ url }: { url: string }) => <div data-testid="pdf">{url}</div> }));

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const ws = (role = 'editor') => ({ id: 5, name: '王家', kind: 'family', color: 'green', role, memberCount: 2 });
const rule = { algorithm: 'auto', match: '', caseInsensitive: true };
const doc = {
  id: 7, title: '2024年3月电费账单', createdDate: '2024-03-05', addedAt: '2024-10-01T08:00:00Z', modifiedAt: '2024-10-01T08:00:00Z',
  mimeType: 'application/pdf', originalFilename: 'a.pdf', pageCount: 1, hasArchive: true, hasThumbnail: true, inbox: true, tagIds: [31], noteCount: 0,
  documentTypeId: 10, asn: 123, amount: '326.40', content: '国家电网 本期电费', checksum: 'ab'.repeat(32), originalSize: 2048,
  fields: [{ fieldId: 50, value: '326.40' }],
};

function backend(role = 'editor', patch: (body: unknown) => Response = (b) => jsonResponse(200, { ...doc, ...(b as object), modifiedAt: '2024-10-02T08:00:00Z' }), extra: Parameters<typeof stubFetch>[0] = {}) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws(role), csrfToken: 'tok' }),
    'GET /api/v1/documents/7': () => jsonResponse(200, doc),
    'PATCH /api/v1/documents/7': async (req) => patch(await req.clone().json()),
    'GET /api/v1/documents': () => jsonResponse(200, { items: [doc], total: 1 }),
    'GET /api/v1/documents/7/notes': () => jsonResponse(200, []),
    'GET /api/v1/documents/7/history': () => jsonResponse(200, { items: [] }),
    'GET /api/v1/document-types': () => jsonResponse(200, [{ id: 10, name: '账单', initial: 'Z', sortKey: 'zhangdan', rule }]),
    'GET /api/v1/correspondents': () => jsonResponse(200, [{ id: 20, name: '国家电网', initial: 'G', sortKey: 'g', rule }]),
    'GET /api/v1/tags': () => jsonResponse(200, [{ id: 31, name: '水电', initial: 'S', sortKey: 'shuidian', rule, color: '#2C7A7B', isInbox: false }, { id: 32, name: '税务', initial: 'S', sortKey: 'shuiwu', rule, color: '#3A6EA5', isInbox: false }]),
    'GET /api/v1/custom-fields': () => jsonResponse(200, [
      { id: 50, name: '金额', initial: 'J', sortKey: 'jine', dataType: 'monetary', extra: { currency: 'CNY' }, builtIn: true },
      { id: 51, name: '到期日', initial: 'D', sortKey: 'daoqiri', dataType: 'date', extra: {}, builtIn: true },
      { id: 52, name: '发票号', initial: 'F', sortKey: 'fapiaohao', dataType: 'text', extra: {}, builtIn: false },
    ]),
    ...extra,
  });
}

afterEach(() => { vi.unstubAllGlobals(); setWorkspaceId(null); });

const render = (route = '/docs/7') => renderWithProviders(<Routes><Route path="/docs/:id" element={<DetailPage />} /><Route path="/files" element={<div>列表页</div>} /></Routes>, { route });

describe('详情页', () => {
  it('修改后保存：只发改过的字段和读取时的修改时间', async () => {
    const calls = backend();
    render();
    expect(await screen.findByRole('heading', { name: '2024年3月电费账单' })).toBeInTheDocument();
    expect(screen.getByText('#000123')).toBeInTheDocument();
    expect(screen.getByText('待整理')).toBeInTheDocument();
    const title = screen.getByLabelText('标题');
    await userEvent.clear(title);
    await userEvent.type(title, '3月电费');
    await userEvent.click(screen.getByLabelText('添加标签'));
    await userEvent.click(await screen.findByRole('option', { name: /税务/ }));
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByLabelText('发票号')).not.toBeInTheDocument(); // 没有值的自定义字段不显示
    expect(screen.getByText('有未保存的修改')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '保存' }));
    await waitFor(() => expect(calls.find((r) => r.method === 'PATCH')).toBeTruthy());
    const body = await calls.find((r) => r.method === 'PATCH')!.clone().json();
    expect(body).toEqual({ modifiedAt: '2024-10-01T08:00:00Z', title: '3月电费', tagIds: [31, 32] });
    await waitFor(() => expect(screen.queryByText('有未保存的修改')).not.toBeInTheDocument());
  });

  it('别人刚改过时提示刷新', async () => {
    backend('editor', () => jsonResponse(409, { code: 'modified', message: '这份文档刚被其他人修改过，请刷新后再改' }));
    render();
    const title = await screen.findByLabelText('标题');
    await userEvent.type(title, '！');
    await userEvent.click(screen.getByRole('button', { name: '保存' }));
    expect(await screen.findByText('这份文档刚被其他人修改过。刷新后会丢掉你的修改。')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '刷新' }));
    await waitFor(() => expect(screen.queryByText('有未保存的修改')).not.toBeInTheDocument());
  });

  it('识别文字可以改正：只发文字和读取时的修改时间，改过的标为已人工修改', async () => {
    let cur: object = doc;
    const calls = backend('editor', (b) => jsonResponse(200, (cur = { ...doc, ...(b as object), contentEditedAt: '2024-10-02T08:00:00Z', modifiedAt: '2024-10-02T08:00:00Z' })),
      { 'GET /api/v1/documents/7': () => jsonResponse(200, cur) });
    render();
    await screen.findByRole('heading', { name: '2024年3月电费账单' });
    await userEvent.click(screen.getByRole('button', { name: '识别文字' }));
    expect(screen.getByText('国家电网 本期电费')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '编辑' }));
    const box = screen.getByLabelText('识别文字', { selector: 'textarea' });
    await userEvent.clear(box);
    await userEvent.type(box, '国家电网 本期电费 326.40 元');
    await userEvent.click(screen.getByRole('button', { name: '保存' }));
    await waitFor(() => expect(calls.find((r) => r.method === 'PATCH')).toBeTruthy());
    expect(await calls.find((r) => r.method === 'PATCH')!.clone().json()).toEqual({ content: '国家电网 本期电费 326.40 元', modifiedAt: '2024-10-01T08:00:00Z' });
    expect(await screen.findByText(/^已人工修改/)).toBeInTheDocument();
  });

  it('文件信息：文件自带的属性，照片的拍摄日期可以用作文档日期', async () => {
    backend('editor', undefined, {
      'GET /api/v1/documents/7/file-info': () => jsonResponse(200, {
        mimeType: 'image/jpeg', props: [{ label: '拍摄时间', value: '2024-03-01 18:42' }, { label: '相机', value: 'iPhone 15' }], takenOn: '2024-03-01', hasLocation: false,
      }),
    });
    render();
    await screen.findByRole('heading', { name: '2024年3月电费账单' });
    await userEvent.click(screen.getByRole('button', { name: '文件信息' }));
    expect(await screen.findByText('iPhone 15')).toBeInTheDocument();
    expect(screen.getByText('文件自带的信息')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '用拍摄日期 2024-03-01 作为文档日期' }));
    expect(screen.getByText('有未保存的修改')).toBeInTheDocument();
  });

  it('有没处理的疑似重复时提示，点击去重复文档页', async () => {
    const other = { ...doc, id: 8, title: '电费账单（扫描）' };
    backend('editor', undefined, {
      'GET /api/v1/duplicates': (req) => jsonResponse(200, new URL(req.url).searchParams.get('document') === '7'
        ? { pairs: [{ id: 1, similarity: 96, a: doc, b: other, aSnippet: '', bSnippet: '', foundAt: '2024-10-02T08:00:00Z' }], scanning: false }
        : { pairs: [], scanning: false }),
    });
    renderWithProviders(<Routes><Route path="/docs/:id" element={<DetailPage />} /><Route path="/duplicates" element={<div>重复文档页</div>} /></Routes>, { route: '/docs/7' });
    expect(await screen.findByText('可能和《电费账单（扫描）》重复（相似度 96%）')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '去处理' }));
    expect(await screen.findByText('重复文档页')).toBeInTheDocument();
  });

  it('识别到的其他日期可以一键换用', async () => {
    const calls = backend('editor', undefined, {
      'GET /api/v1/documents/7': () => jsonResponse(200, { ...doc, fieldSources: { date: { source: 'text', basis: '账单日 2024/03/05', alternatives: [
        { date: '2024-02-06', basis: '账单周期 2024/02/06' }, { date: '2024-03-01', basis: '文件名“IMG_20240301.jpg”' }] } } }),
    });
    render();
    await screen.findByRole('heading', { name: '2024年3月电费账单' });
    expect(screen.getByText('其他日期')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '改用 2024-02-06（账单周期 2024/02/06）' }));
    expect(screen.getByText('有未保存的修改')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /改用 2024-02-06/ })).not.toBeInTheDocument(); // it is the date now
    await userEvent.click(screen.getByRole('button', { name: '保存' }));
    await waitFor(() => expect(calls.find((r) => r.method === 'PATCH')).toBeTruthy());
    expect(await calls.find((r) => r.method === 'PATCH')!.clone().json()).toMatchObject({ createdDate: '2024-02-06' });
  });

  it('列出关联到这份的文档，点击打开', async () => {
    backend('editor', undefined, {
      'GET /api/v1/documents/7': () => jsonResponse(200, { ...doc, linkedFrom: [{ id: 9, title: '2024年4月电费账单', createdDate: '2024-04-05', field: '合同' }] }),
    });
    render();
    const list = await screen.findByRole('list', { name: '关联到这份的文档' });
    expect(screen.getByText('关联到这份的文档 · 1')).toBeInTheDocument();
    expect(within(list).getByRole('link', { name: '2024年4月电费账单' })).toHaveAttribute('href', '/docs/9');
    expect(within(list).getByText('2024-04-05 · 通过“合同”')).toBeInTheDocument();
  });

  it('没有关联时不显示', async () => {
    backend();
    render();
    await screen.findByRole('heading', { name: '2024年3月电费账单' });
    expect(screen.queryByText(/关联到这份的文档/)).not.toBeInTheDocument();
  });

  it('识别文字里查找：⌘F 打开，标出每一处，回车下一处，Esc 关闭', async () => {
    backend();
    render();
    await screen.findByRole('heading', { name: '2024年3月电费账单' });
    await userEvent.click(screen.getByRole('button', { name: '识别文字' }));
    await userEvent.keyboard('{Control>}f{/Control}');
    const box = screen.getByRole('textbox', { name: '查找' });
    expect(box).toHaveFocus();
    await userEvent.type(box, '电');
    expect(await screen.findByText('第 1 / 2 处')).toBeInTheDocument();
    const marks = document.querySelectorAll('mark');
    expect([...marks].map((m) => m.textContent)).toEqual(['电', '电']);
    expect(marks[0]).toHaveAttribute('data-find-current');
    await userEvent.keyboard('{Enter}');
    expect(screen.getByText('第 2 / 2 处')).toBeInTheDocument();
    expect(document.querySelectorAll('mark')[1]).toHaveAttribute('data-find-current');
    await userEvent.keyboard('{Shift>}{Enter}{/Shift}');
    expect(screen.getByText('第 1 / 2 处')).toBeInTheDocument();
    await userEvent.clear(box);
    await userEvent.type(box, '水费');
    expect(await screen.findByText('没有找到')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '下一处' })).toBeDisabled();
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('textbox', { name: '查找' })).not.toBeInTheDocument();
    expect(document.querySelectorAll('mark')).toHaveLength(0);
    expect(screen.getByRole('heading', { name: '2024年3月电费账单' })).toBeInTheDocument(); // Esc 只关查找框，不离开详情页
  });

  it('从搜索结果打开时自动带上搜索词，不抢焦点', async () => {
    backend();
    renderWithProviders(<Routes><Route path="/docs/:id" element={<DetailPage />} /></Routes>, { route: { pathname: '/docs/7', state: { find: '电费' } } });
    await screen.findByRole('heading', { name: '2024年3月电费账单' });
    const box = await screen.findByRole('textbox', { name: '查找' });
    expect(box).toHaveValue('电费');
    expect(box).not.toHaveFocus();
    await userEvent.click(screen.getByRole('button', { name: '识别文字' }));
    expect(await screen.findByText('第 1 / 1 处')).toBeInTheDocument();
  });

  it('详情页可以直接新建往来方，也能去看它的全部文档（v1.6.0）', async () => {
    const calls = backend('editor', undefined, {
      'POST /api/v1/correspondents': () => jsonResponse(201, { id: 25, name: '阳光物业', initial: 'Y', sortKey: 'yangguangwuye', rule }),
    });
    renderWithProviders(<Routes><Route path="/docs/:id" element={<DetailPage />} /><Route path="/directory/:kind/:id" element={<div>条目页</div>} /></Routes>, { route: '/docs/7' });
    await screen.findByRole('heading', { name: '2024年3月电费账单' });
    await userEvent.click(screen.getByRole('button', { name: '往来方：未设置' }));
    await userEvent.type(screen.getByRole('textbox', { name: '搜索往来方' }), '阳光物业');
    await userEvent.click(screen.getByText('新建“阳光物业”'));
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/correspondents'))).toBe(true));
    expect(await calls.find((r) => r.method === 'POST')!.clone().json()).toEqual({ name: '阳光物业' });
    expect(screen.getByText('有未保存的修改')).toBeInTheDocument();
    // 类型旁边的“查看全部”：有未保存的修改时先确认（这里不渲染确认框，只看没有直接跳走）
    await userEvent.click(screen.getByRole('button', { name: '查看“账单”的全部文档' }));
    expect(screen.queryByText('条目页')).not.toBeInTheDocument();
  });

  it('没有修改时点标签直接去看带这个标签的文档', async () => {
    backend();
    renderWithProviders(<Routes><Route path="/docs/:id" element={<DetailPage />} /><Route path="/directory/:kind/:id" element={<div>条目页</div>} /></Routes>, { route: '/docs/7' });
    await screen.findByRole('heading', { name: '2024年3月电费账单' });
    await userEvent.click(screen.getByRole('button', { name: '查看带“水电”标签的文档' }));
    expect(await screen.findByText('条目页')).toBeInTheDocument();
  });

  it('只看的成员只能看', async () => {
    backend('viewer');
    render();
    await screen.findByRole('heading', { name: '2024年3月电费账单' });
    expect(screen.queryByLabelText('标题')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('添加标签')).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText('添加备注…')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '完成整理' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '更多' }));
    expect(within(await screen.findByRole('menu')).getByRole('menuitem', { name: '删除' })).toHaveAttribute('aria-disabled', 'true');
  });

  it('链接里的文档编号不对时说明原因', async () => {
    backend();
    render('/docs/d000123-abcd');
    expect(await screen.findByText('链接里的文档编号不对。')).toBeInTheDocument();
  });
});

describe('v1.6.3：家人和存放位置', () => {
  it('显示原件在哪里；改家人和存放位置后保存', async () => {
    const calls = backend('editor', undefined, {
      'GET /api/v1/documents/7': () => jsonResponse(200, { ...doc, personId: 60, locationId: 70 }),
      'GET /api/v1/persons': () => jsonResponse(200, [{ id: 60, name: '爸爸', initial: 'B', sortKey: 'baba', rule }, { id: 61, name: '乐乐', initial: 'L', sortKey: 'lele', rule }]),
      'GET /api/v1/locations': () => jsonResponse(200, [{ id: 70, name: '文件盒 A', initial: 'W', note: '书房柜子第 2 层', documentCount: 3 }, { id: 71, name: '银行保险箱', initial: 'Y', note: '', documentCount: 1 }]),
    });
    render();
    expect(await screen.findByText('原件在：文件盒 A')).toHaveAttribute('title', '书房柜子第 2 层');
    await userEvent.click(screen.getByRole('button', { name: '家人：爸爸' }));
    await userEvent.click(await screen.findByRole('option', { name: '乐乐' }));
    await userEvent.click(screen.getByRole('button', { name: '存放位置：文件盒 A' }));
    await userEvent.click(await screen.findByRole('option', { name: '银行保险箱' }));
    await userEvent.click(screen.getByRole('button', { name: '保存' }));
    await waitFor(() => expect(calls.find((r) => r.method === 'PATCH')).toBeTruthy());
    expect(await calls.find((r) => r.method === 'PATCH')!.clone().json()).toEqual({ modifiedAt: '2024-10-01T08:00:00Z', personId: 61, locationId: 71 });
  });
});

describe('v1.6.4：敏感文档', () => {
  it('没有确认密码时锁住预览、下载；确认后重新读取', async () => {
    let locked = true;
    const calls = backend('editor', undefined, {
      'GET /api/v1/documents/7': () => jsonResponse(200, locked ? { ...doc, content: '', sensitive: true, sensitiveMarked: true, locked: true } : { ...doc, sensitive: true, sensitiveMarked: true }),
      'POST /api/v1/auth/verify': async (r) => {
        const b = await r.clone().json();
        if (b.password !== 'right') return jsonResponse(400, { code: 'invalid_request', message: '密码不正确' });
        locked = false;
        return jsonResponse(200, { verifiedUntil: '2030-01-01T00:00:00Z' });
      },
    });
    render();
    expect(await screen.findByText('这是敏感文档')).toBeInTheDocument();
    expect(screen.getByText('敏感')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '下载' })).toBeDisabled();
    expect(screen.queryByTestId('pdf')).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '确认密码' }));
    const dlg = await screen.findByRole('dialog', { name: '确认是你本人' });
    await userEvent.type(within(dlg).getByLabelText('密码'), 'nope');
    await userEvent.click(within(dlg).getByRole('button', { name: '确认' }));
    expect(await within(dlg).findByText('密码不正确')).toBeInTheDocument();
    await userEvent.clear(within(dlg).getByLabelText('密码'));
    await userEvent.type(within(dlg).getByLabelText('密码'), 'right');
    await userEvent.click(within(dlg).getByRole('button', { name: '确认' }));
    expect(await screen.findByTestId('pdf')).toBeInTheDocument();
    expect(calls.filter((r) => r.url.endsWith('/auth/verify'))).toHaveLength(2);
  });

  it('“更多”里标为敏感文档', async () => {
    const calls = backend();
    render();
    await screen.findByRole('heading', { name: '2024年3月电费账单' });
    await userEvent.click(screen.getByRole('button', { name: '更多' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: '标为敏感文档' }));
    await waitFor(() => expect(calls.find((r) => r.method === 'PATCH')).toBeTruthy());
    expect(await calls.find((r) => r.method === 'PATCH')!.clone().json()).toEqual({ sensitive: true });
  });
});

describe('详情页：家人的私密文档（v1.8.0）', () => {
  const persons = { 'GET /api/v1/persons': () => jsonResponse(200, [{ id: 60, name: '妈妈', initial: 'M', sortKey: 'mama', rule }]) };

  it('帮忙上传的人只能看：有私密标记和说明，不能编辑', async () => {
    const mine = { ...doc, personId: 60, private: true, privacy: { canEdit: false, canSetPrivate: false, asUploader: true } };
    backend('editor', undefined, { ...persons, 'GET /api/v1/documents/7': () => jsonResponse(200, mine) });
    render();
    expect(await screen.findByText('私密')).toBeInTheDocument();
    expect(await screen.findByText(/这是妈妈的私密文档。你上传了它/)).toBeInTheDocument();
    expect(screen.queryByLabelText('标题')).not.toBeInTheDocument();
  });

  it('本人把文档设为私密', async () => {
    const mine = { ...doc, personId: 60, privacy: { canEdit: true, canSetPrivate: true, asUploader: false } };
    const calls = backend('editor', (b) => jsonResponse(200, { ...mine, ...(b as object) }), { ...persons, 'GET /api/v1/documents/7': () => jsonResponse(200, mine) });
    render();
    await screen.findByLabelText('标题');
    await userEvent.click(screen.getByRole('button', { name: '更多' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: '设为家人的私密文档' }));
    await waitFor(() => expect(calls.find((r) => r.method === 'PATCH')).toBeTruthy());
    expect(await calls.find((r) => r.method === 'PATCH')!.clone().json()).toEqual({ private: true });
  });
});

describe('打印（v1.9.7）', () => {
  it('宽屏页头有打印，⌘P 也打印这份文档；取文件时带上用途', async () => {
    URL.createObjectURL = vi.fn(() => 'blob:print');
    const calls = backend('editor', undefined, {
      'GET /api/v1/documents/7/preview': () => new Response('%PDF-1.4', { status: 200, headers: { 'Content-Type': 'application/pdf' } }),
    });
    render();
    await userEvent.click(await screen.findByRole('button', { name: '打印' }));
    await waitFor(() => expect(calls.some((r) => r.url.includes('/documents/7/preview') && r.url.includes('purpose=print'))).toBe(true));
    const printed = () => calls.filter((r) => r.url.includes('purpose=print')).length;
    expect(printed()).toBe(1);
    await userEvent.keyboard('{Meta>}p{/Meta}');
    await waitFor(() => expect(printed()).toBe(2));
    expect(document.querySelectorAll('iframe[aria-hidden="true"]').length).toBeGreaterThan(0);
  });

  it('下载菜单带上下载的用途', async () => {
    const calls = backend();
    render();
    const clicked: string[] = [];
    const orig = HTMLAnchorElement.prototype.click;
    HTMLAnchorElement.prototype.click = function (this: HTMLAnchorElement) { clicked.push(this.href); };
    try {
      await userEvent.click(await screen.findByRole('button', { name: '下载' }));
      await userEvent.click(await screen.findByRole('menuitem', { name: '原件' }));
    } finally { HTMLAnchorElement.prototype.click = orig; }
    expect(clicked[0]).toMatch(/\/documents\/7\/original\?.*purpose=download|\/documents\/7\/original\?purpose=download/);
    expect(calls.length).toBeGreaterThan(0);
  });
});

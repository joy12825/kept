import { eeBuild } from '@ee';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setWorkspaceId } from '../../api/client';
import ChecklistsPage from './checklists-page';
import ChecklistPage from './checklist-page';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const ws = (role = 'editor') => ({ id: 5, name: '王家', kind: 'family', color: 'green', role, memberCount: 2 });
const doc = (id: number, title: string, extra: object = {}) => ({ id, title, createdDate: '2024-03-05', addedAt: '2024-03-05T08:00:00Z', modifiedAt: '2024-03-05T08:00:00Z', mimeType: 'application/pdf',
  originalFilename: 'a.pdf', pageCount: 1, hasArchive: true, hasThumbnail: false, inbox: false, tagIds: [], noteCount: 0, drawerManual: false, isMilestone: false, isBook: false, ...extra });
const checklist = { id: 3, name: '办签证', note: '以使馆要求为准', updatedAt: '2024-03-05T08:00:00Z', items: [
  { id: 'a', label: '护照', count: 1, filter: { text: '护照' } },
  { id: 'b', label: '近 3 个月工资条', count: 3, filter: { text: '工资条' } },
  { id: 'c', label: '户口本', count: 1, filter: { text: '户口本' }, note: '整本复印' },
] };

function backend(role = 'editor', extra: Parameters<typeof stubFetch>[0] = {}) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws(role), csrfToken: 'tok' }),
    'GET /api/v1/checklists': () => jsonResponse(200, [checklist]),
    'GET /api/v1/checklists/3/check': () => jsonResponse(200, { checklist, items: [
      { id: 'a', label: '护照', count: 1, status: 'ok', documents: [doc(10, '护照', { dueDate: '2020-01-01' })], expiredIds: [10], attachedIds: [] },
      { id: 'b', label: '近 3 个月工资条', count: 3, status: 'partial', documents: [doc(11, '工资条 2月'), doc(12, '工资条 1月')], expiredIds: [], attachedIds: [] },
      { id: 'c', label: '户口本', count: 1, status: 'missing', documents: [], expiredIds: [], attachedIds: [], note: '整本复印' },
    ] }),
    'GET /api/v1/document-types': () => jsonResponse(200, []),
    'GET /api/v1/correspondents': () => jsonResponse(200, []),
    'GET /api/v1/tags': () => jsonResponse(200, []),
    'GET /api/v1/custom-fields': () => jsonResponse(200, []),
    'GET /api/v1/drawers': () => jsonResponse(200, { items: [] }),
    ...extra,
  });
}

afterEach(() => { vi.unstubAllGlobals(); setWorkspaceId(null); });

describe('办事清单（v1.6.2）', () => {
  it('核对：齐了、不够、没有，过期的标出来；可以打包下载', async () => {
    backend();
    const hrefs: string[] = [];
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) { hrefs.push(this.href); });
    renderWithProviders(<Routes><Route path="/checklists/:id" element={<ChecklistPage />} /></Routes>, { route: '/checklists/3' });
    expect(await screen.findByRole('heading', { name: '办签证' })).toBeInTheDocument();
    expect(screen.getByText('3 项材料里，1 项齐了，2 项还缺。')).toBeInTheDocument();
    expect(screen.getByText('有 1 份已经过了到期日，记得换新的。')).toBeInTheDocument();
    expect(screen.getByRole('progressbar', { name: '齐了 1 项，共 3 项' })).toBeInTheDocument();
    const list = screen.getByRole('list', { name: '材料' });
    expect(within(list).getByText('还差 1 份')).toBeInTheDocument();
    expect(within(screen.getByRole('listitem', { name: '户口本' })).getByText('还没有')).toBeInTheDocument();
    expect(within(list).getByText('已过期 2020-01-01')).toBeInTheDocument();
    expect(within(list).getByRole('link', { name: '工资条 2月' })).toHaveAttribute('href', '/docs/11');
    await userEvent.click(screen.getByRole('button', { name: '下载全部（3 份）' }));
    expect(new URL(hrefs[0]).searchParams.get('ids')).toBe('10,11,12');
    click.mockRestore();
  });

  it('从模板新建：模板的材料都在，按名称当关键词', async () => {
    const calls = backend('editor', { 'POST /api/v1/checklists': async (r) => jsonResponse(201, { ...(await r.clone().json()), id: 9, updatedAt: '2024-03-05T08:00:00Z', items: [] }) });
    renderWithProviders(<Routes><Route path="/checklists" element={<ChecklistsPage />} /><Route path="/checklists/:id" element={<p>清单页</p>} /></Routes>, { route: '/checklists' });
    await userEvent.click(await screen.findByRole('button', { name: '新建清单' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '从模板：报销' }));
    const dlg = await screen.findByRole('dialog', { name: '新建清单' });
    expect(within(dlg).getByRole('textbox', { name: '第 1 项的名称' })).toHaveValue('发票');
    await userEvent.click(within(dlg).getByRole('button', { name: '添加一项' }));
    await userEvent.type(within(dlg).getByRole('textbox', { name: '第 4 项的名称' }), '审批单');
    await userEvent.click(within(dlg).getByRole('button', { name: '新建' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'POST')).toBe(true));
    const body = await calls.find((r) => r.method === 'POST')!.clone().json();
    expect(body.name).toBe('报销');
    expect(body.items).toHaveLength(4);
    expect(body.items[0]).toMatchObject({ label: '发票', count: 3, filter: { text: '发票' } });
    expect(body.items[3]).toMatchObject({ label: '审批单', count: 1, filter: { text: '审批单' } });
    expect(await screen.findByText('清单页')).toBeInTheDocument();
  });

  it('只看的成员没有新建、编辑、生成分享包', async () => {
    backend('viewer');
    renderWithProviders(<Routes><Route path="/checklists/:id" element={<ChecklistPage />} /></Routes>, { route: '/checklists/3' });
    await screen.findByRole('heading', { name: '办签证' });
    expect(screen.queryByRole('button', { name: '编辑' })).not.toBeInTheDocument();
    if (eeBuild) expect(screen.getByRole('button', { name: '生成分享包' })).toBeDisabled(); // 分享包是收费功能（v1.13.2）
    else expect(screen.queryByRole('button', { name: '生成分享包' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: '下载全部（3 份）' })).toBeEnabled();
  });
});

describe('办事清单列表的卡片', () => {
  it('显示齐了几项、还缺什么、过期几份，下面是模板', async () => {
    backend();
    renderWithProviders(<Routes><Route path="/checklists" element={<ChecklistsPage />} /></Routes>, { route: '/checklists' });
    expect(await screen.findByText('齐了 1/3')).toBeInTheDocument();
    expect(screen.getByRole('progressbar', { name: '办签证：齐了 1 项，共 3 项' })).toBeInTheDocument();
    expect(screen.getByText('近 3 个月工资条、户口本')).toBeInTheDocument();
    expect(screen.getByText('1 份已过期')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: '从模板开始' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '用模板新建：租房' })).toBeInTheDocument();
  });
});

describe('清单里有敏感文档时', () => {
  it('分享包不放它；下载全部先确认密码', async () => {
    backend('editor', {
      'GET /api/v1/checklists/3/check': () => jsonResponse(200, { checklist, items: [
        { id: 'a', label: '护照', count: 1, status: 'ok', documents: [doc(10, '护照', { sensitive: true })], expiredIds: [], attachedIds: [] },
        { id: 'b', label: '近 3 个月工资条', count: 3, status: 'partial', documents: [doc(11, '工资条 2月')], expiredIds: [], attachedIds: [] },
      ] }),
    });
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    renderWithProviders(<Routes><Route path="/checklists/:id" element={<ChecklistPage />} /></Routes>, { route: '/checklists/3' });
    expect(await screen.findByText('其中 1 份是敏感文档，不会放进分享包。')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '下载全部（2 份）' }));
    expect(await screen.findByRole('dialog', { name: '确认是你本人' })).toBeInTheDocument();
    expect(click).not.toHaveBeenCalled();
    click.mockRestore();
  });
});

class FakeXHR {
  static sent: string[] = [];
  upload: { onprogress?: (e: { lengthComputable: boolean; loaded: number; total: number }) => void } = {};
  onload?: () => void; onerror?: () => void;
  status = 0; responseText = ''; url = '';
  open(_m: string, url: string) { this.url = url; }
  setRequestHeader() {}
  send(form: FormData) {
    FakeXHR.sent.push(`${this.url} ${(form.get('file') as File).name}${form.get('title') ? ` title=${form.get('title')}` : ''}`);
    this.upload.onprogress?.({ lengthComputable: true, loaded: 1, total: 2 });
    this.status = 202;
    this.responseText = JSON.stringify({ jobId: 77 });
    setTimeout(() => this.onload?.(), 0);
  }
}

describe('补充材料（v1.6.7，v1.6.8 改版）', () => {
  it('对话框里写明要求；上传新文件时可以用项名作标题，显示正在识别，识别完刷新', async () => {
    let jobDone = false;
    const calls = backend('editor', {
      'GET /api/v1/jobs': () => jsonResponse(200, [{ id: 77, kind: 'ingest.document', status: jobDone ? 'succeeded' : 'running', stage: 'ocr', progress: 50, filename: 'IMG_1234.jpg', createdAt: '2024-03-05T08:00:00Z' }]),
    });
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    renderWithProviders(<Routes><Route path="/checklists/:id" element={<ChecklistPage />} /></Routes>, { route: '/checklists/3' });
    const card = await screen.findByRole('listitem', { name: '户口本' });
    expect(within(card).getByText('还没有')).toBeInTheDocument();
    await userEvent.click(within(card).getByRole('button', { name: '补充材料' }));
    const dlg = await screen.findByRole('dialog', { name: '补充“户口本”' });
    expect(within(dlg).getByText('要 1 份，还没有')).toBeInTheDocument();
    expect(within(dlg).getByText('备注：整本复印')).toBeInTheDocument();
    expect(within(dlg).getByRole('checkbox', { name: '用“户口本”作标题（否则用文件名）' })).toBeChecked();
    await userEvent.upload(within(dlg).getByLabelText('上传到“户口本”'), new File(['x'], 'IMG_1234.jpg', { type: 'image/jpeg' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(await within(card).findByText('IMG_1234.jpg · 正在识别…')).toBeInTheDocument();
    expect(FakeXHR.sent).toEqual(['/api/v1/checklists/3/items/c/upload IMG_1234.jpg title=户口本']);
    const checks = () => calls.filter((r) => r.url.endsWith('/checklists/3/check')).length;
    const before = checks();
    jobDone = true;
    await waitFor(() => expect(within(card).queryByText('IMG_1234.jpg · 正在识别…')).not.toBeInTheDocument(), { timeout: 4000 });
    await waitFor(() => expect(checks()).toBeGreaterThan(before));
    FakeXHR.sent = [];
  });

  it('从已有文档里选几份放进来；手动放入的可以移出；可以只看还缺的', async () => {
    const calls = backend('editor', {
      'GET /api/v1/checklists/3/check': () => jsonResponse(200, { checklist, items: [
        { id: 'a', label: '护照', count: 1, status: 'ok', documents: [doc(10, '护照扫描')], expiredIds: [], attachedIds: [10] },
        { id: 'c', label: '户口本', count: 2, status: 'missing', documents: [], expiredIds: [], attachedIds: [] },
      ] }),
      'GET /api/v1/documents': () => jsonResponse(200, { items: [doc(20, '常住人口登记卡'), doc(21, '户口本首页')], total: 2 }),
      'POST /api/v1/checklists/3/items/c/documents': () => new Response(null, { status: 204 }),
      'DELETE /api/v1/checklists/3/items/a/documents/10': () => new Response(null, { status: 204 }),
    });
    renderWithProviders(<Routes><Route path="/checklists/:id" element={<ChecklistPage />} /></Routes>, { route: '/checklists/3' });
    await screen.findByRole('heading', { name: '办签证' });
    await userEvent.click(screen.getByRole('button', { name: '还缺 1' }));
    expect(screen.queryByRole('listitem', { name: '护照' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '全部 2' }));
    expect(within(screen.getByRole('listitem', { name: '护照' })).getByText('手动放入')).toBeInTheDocument();
    await userEvent.click(within(screen.getByRole('listitem', { name: '护照' })).getByRole('button', { name: '把《护照扫描》移出这一项' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'DELETE')).toBe(true));

    await userEvent.click(within(screen.getByRole('listitem', { name: '户口本' })).getByRole('button', { name: '补充材料' }));
    const dlg = await screen.findByRole('dialog', { name: '补充“户口本”' });
    expect(within(dlg).getByRole('textbox', { name: '搜索已有文档' })).toHaveValue('户口本');
    expect(within(dlg).getByText(/还差 2 份，可以多选/)).toBeInTheDocument();
    await userEvent.click(await within(dlg).findByRole('option', { name: /常住人口登记卡/ }));
    await userEvent.click(within(dlg).getByRole('option', { name: /户口本首页/ }));
    await userEvent.click(within(dlg).getByRole('button', { name: '放进来（2）' }));
    await waitFor(() => expect(calls.filter((r) => r.method === 'POST' && r.url.endsWith('/items/c/documents'))).toHaveLength(2));
    const bodies = await Promise.all(calls.filter((r) => r.method === 'POST').map((r) => r.clone().json()));
    expect(bodies).toEqual([{ documentId: 20 }, { documentId: 21 }]);
  });

  it('只看的成员没有补充材料', async () => {
    backend('viewer');
    renderWithProviders(<Routes><Route path="/checklists/:id" element={<ChecklistPage />} /></Routes>, { route: '/checklists/3' });
    const card = await screen.findByRole('listitem', { name: '户口本' });
    expect(within(card).queryByRole('button', { name: '补充材料' })).not.toBeInTheDocument();
  });
});

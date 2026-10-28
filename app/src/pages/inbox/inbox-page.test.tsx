import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setWorkspaceId } from '../../api/client';
import InboxPage from './inbox-page';

vi.mock('../../components/pdf-preview', () => ({ PdfPreview: ({ url }: { url: string }) => <div data-testid="pdf">{url}</div> }));

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const ws = (role = 'editor') => ({ id: 5, name: '王家', kind: 'family', color: 'green', role, memberCount: 2 });
const rule = { algorithm: 'auto', match: '', caseInsensitive: true };
const doc = (id: number, title: string, mimeType = 'application/pdf', extra: object = {}) => ({
  id, title, createdDate: '2024-03-05', addedAt: '2024-10-01T08:00:00Z', modifiedAt: '2024-10-01T08:00:00Z', mimeType, originalFilename: `${title}.pdf`,
  pageCount: 1, hasArchive: false, hasThumbnail: false, inbox: true, tagIds: [], noteCount: 0, ...extra,
});
const items = [doc(1, '2024年3月电费账单', 'application/pdf', { documentTypeId: 3 }), doc(2, '备忘', 'text/plain')];

function backend(list = items, role = 'editor') {
  let inbox = [...list];
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws(role), csrfToken: 'tok-1' }),
    'GET /api/v1/documents': () => jsonResponse(200, { items: inbox, total: inbox.length }),
    'GET /api/v1/documents/1': () => jsonResponse(200, { ...items[0], content: '国家电网\n本期电费 128.50 元', fields: [], checksum: 'x',
      fieldSources: { type: { source: 'rule', basis: '关键词“电费”' }, date: { source: 'text', basis: '2024年3月5日' } } }),
    'GET /api/v1/documents/2': () => jsonResponse(200, { ...items[1], content: '记得交电费', fields: [], checksum: 'y' }),
    'PATCH /api/v1/documents/1': async (req) => {
      const body = await req.clone().json();
      if (body.inbox === false) inbox = inbox.filter((d) => d.id !== 1);
      return jsonResponse(200, { ...items[0], ...body, fields: [], checksum: 'x', content: '' });
    },
    'GET /api/v1/document-types': () => jsonResponse(200, [{ id: 3, name: '账单', initial: 'Z', sortKey: 'zhangdan', rule }, { id: 4, name: '合同', initial: 'H', sortKey: 'hetong', rule }]),
    'GET /api/v1/correspondents': () => jsonResponse(200, []),
    'GET /api/v1/tags': () => jsonResponse(200, []),
    'GET /api/v1/custom-fields': () => jsonResponse(200, [{ id: 50, name: '金额', initial: 'J', sortKey: 'jine', dataType: 'monetary', extra: { currency: 'CNY' }, builtIn: true }]),
  });
}

afterEach(() => { vi.unstubAllGlobals(); setWorkspaceId(null); });

describe('待整理', () => {
  it('逐份整理：显示预览和可修改的信息；账单没填金额时提示建议填写', async () => {
    backend();
    setWorkspaceId(5);
    renderWithProviders(<InboxPage />, { route: '/inbox' });
    expect(await screen.findByText('第 1 份，共 2 份')).toBeInTheDocument();
    expect(screen.getByLabelText('标题')).toHaveValue('2024年3月电费账单');
    expect(screen.getByTestId('pdf')).toHaveTextContent('/api/v1/documents/1/preview?workspace=5');
    expect(await screen.findByLabelText('类型：账单')).toBeInTheDocument();
    expect(await screen.findByText('建议填写：账单通常有金额')).toBeInTheDocument();
  });

  it('自动填入的字段打勾并给出依据；改过之后对勾消失', async () => {
    backend();
    renderWithProviders(<InboxPage />, { route: '/inbox' });
    expect(await screen.findByRole('img', { name: '自动填入：匹配规则：关键词“电费”' })).toBeInTheDocument();
    expect(screen.getByRole('img', { name: '自动填入：从正文识别：“2024年3月5日”' })).toBeInTheDocument();
    expect(screen.getByText('依据：匹配规则：类型（关键词“电费”）；日期从正文识别')).toBeInTheDocument();
    await userEvent.click(screen.getByLabelText('类型：账单'));
    await userEvent.click(await screen.findByRole('option', { name: /合同/ }));
    await waitFor(() => expect(screen.queryByRole('img', { name: /关键词“电费”/ })).not.toBeInTheDocument());
    expect(screen.getByText('依据：日期从正文识别')).toBeInTheDocument();
  });

  it('改一项存一项', async () => {
    const calls = backend();
    renderWithProviders(<InboxPage />, { route: '/inbox' });
    await userEvent.click(await screen.findByLabelText('类型：账单'));
    await userEvent.click(await screen.findByRole('option', { name: /合同/ }));
    await waitFor(() => expect(calls.some((r) => r.method === 'PATCH')).toBe(true));
    expect(await calls.find((r) => r.method === 'PATCH')!.clone().json()).toEqual({ documentTypeId: 4 });
  });

  it('按 A 完成整理并进入下一份', async () => {
    const calls = backend();
    renderWithProviders(<InboxPage />, { route: '/inbox' });
    await screen.findByText('第 1 份，共 2 份');
    await userEvent.keyboard('a');
    await waitFor(() => expect(calls.some((r) => r.method === 'PATCH')).toBe(true), { timeout: 3000 });
    expect(await calls.find((r) => r.method === 'PATCH')!.clone().json()).toEqual({ inbox: false });
    // 文本文件直接显示文字，没有原件 / 识别文字切换
    expect(await screen.findByText('记得交电费', {}, { timeout: 3000 })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '识别文字' })).not.toBeInTheDocument();
  });

  it('跳过的放到队列末尾；从任务托盘点进来时定位到那一份', async () => {
    backend();
    renderWithProviders(<InboxPage />, { route: '/inbox?doc=2' });
    expect(await screen.findByLabelText('标题')).toHaveValue('备忘');
    await userEvent.keyboard('s');
    await waitFor(() => expect(screen.getByLabelText('标题')).toHaveValue('2024年3月电费账单'));
    expect(screen.getByText('第 1 份，共 2 份')).toBeInTheDocument();
  });

  it('只看的成员只能看', async () => {
    backend(items, 'viewer');
    renderWithProviders(<InboxPage />, { route: '/inbox' });
    await screen.findByText('第 1 份，共 2 份');
    expect(screen.queryByLabelText('标题')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /完成整理/ })).not.toBeInTheDocument();
  });

  it('没有文档时显示空状态', async () => {
    backend([]);
    renderWithProviders(<InboxPage />, { route: '/inbox' });
    expect(await screen.findByText('没有待整理的文档')).toBeInTheDocument();
  });
});

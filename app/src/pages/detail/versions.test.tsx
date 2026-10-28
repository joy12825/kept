import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken, setWorkspaceId } from '../../api/client';
import { ConfirmDialog } from '../../components/overlays';
import { RemovePasswordDialog, Versions } from './versions';

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

const v = (id: number, number: number, extra: Record<string, unknown> = {}) => ({
  id, number, label: '', filename: `合同-${number}.pdf`, mimeType: 'application/pdf', size: 2048, encrypted: false, current: false,
  creatorName: '小王', createdAt: new Date().toISOString(), ...extra,
});

describe('Versions', () => {
  it('列出版本，把旧版本设为当前版本', async () => {
    const calls = stubFetch({
      'GET /api/v1/documents/9/versions': () => jsonResponse(200, [v(12, 2, { current: true, label: '第二稿' }), v(11, 1)]),
      'POST /api/v1/documents/9/versions/11/current': () => jsonResponse(202, { jobId: 5 }),
    });
    renderWithProviders(<><Versions docId={9} readOnly={false} /><ConfirmDialog /></>);
    expect(await screen.findByText('第二稿')).toBeInTheDocument();
    expect(screen.getByText('当前')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '删除第 2 版' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '设为当前版本' }));
    await userEvent.click(await screen.findByRole('button', { name: '设为当前版本' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/versions/11/current'))).toBe(true));
  });

  it('上传新版本：说明在文件之前', async () => {
    const calls = stubFetch({
      'GET /api/v1/documents/9/versions': () => jsonResponse(200, [v(0, 1, { current: true })]),
      'POST /api/v1/documents/9/versions': () => jsonResponse(202, { jobId: 6 }),
    });
    const append = vi.spyOn(FormData.prototype, 'append');
    renderWithProviders(<Versions docId={9} readOnly={false} />);
    await userEvent.click(await screen.findByRole('button', { name: '上传新版本' }));
    await userEvent.upload(screen.getByLabelText('新版本的文件'), new File(['%PDF'], '合同-二稿.pdf', { type: 'application/pdf' }));
    await userEvent.type(screen.getByRole('textbox', { name: '说明' }), '第二稿');
    await userEvent.click(screen.getByRole('button', { name: '上传' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.headers.get('content-type')?.startsWith('multipart/form-data'))).toBe(true));
    expect(append.mock.calls.map((c) => c[0])).toEqual(['label', 'file']);
    expect(append.mock.calls[0][1]).toBe('第二稿');
    append.mockRestore();
  });

  it('只看的成员只能下载', async () => {
    stubFetch({ 'GET /api/v1/documents/9/versions': () => jsonResponse(200, [v(12, 2, { current: true }), v(11, 1)]) });
    renderWithProviders(<Versions docId={9} readOnly />);
    expect(await screen.findByRole('button', { name: '下载第 1 版' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '上传新版本' })).not.toBeInTheDocument();
    expect(screen.queryByText('设为当前版本')).not.toBeInTheDocument();
  });
});

describe('RemovePasswordDialog', () => {
  it('密码不对时显示原因', async () => {
    stubFetch({ 'POST /api/v1/documents/9/versions/decrypt': () => jsonResponse(400, { code: 'wrong_password', message: 'PDF 的密码不正确' }) });
    renderWithProviders(<RemovePasswordDialog docId={9} open onClose={() => {}} />);
    await userEvent.type(screen.getByLabelText('PDF 的密码'), 'nope{Enter}');
    expect(await screen.findByText('PDF 的密码不正确')).toBeInTheDocument();
  });
});

describe('从已有文档添加版本（v1.5.8）', () => {
  it('搜索选一份文档，并为新版本', async () => {
    const calls = stubFetch({
      'GET /api/v1/documents/9/versions': () => jsonResponse(200, [v(0, 1, { current: true })]),
      'GET /api/v1/documents': () => jsonResponse(200, { items: [{ id: 9, title: '租房合同' }, { id: 15, title: '续签合同' }], total: 2 }),
      'POST /api/v1/documents/9/versions/from-document': () => jsonResponse(202, { jobId: 7 }),
    });
    renderWithProviders(<Versions docId={9} readOnly={false} />);
    await userEvent.click(await screen.findByRole('button', { name: '从已有文档添加' }));
    expect(screen.getByRole('button', { name: '并为新版本' })).toBeDisabled();
    await userEvent.click(screen.getByRole('combobox', { name: '要并入的文档' }));
    expect(await screen.findByRole('option', { name: '续签合同' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: '租房合同' })).not.toBeInTheDocument(); // 不能选自己
    await userEvent.click(screen.getByRole('option', { name: '续签合同' }));
    await userEvent.click(screen.getByRole('button', { name: '并为新版本' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/from-document'))).toBe(true));
    expect(await calls.find((r) => r.method === 'POST')!.clone().json()).toEqual({ documentId: 15 });
  });

  it('只看时没有入口', async () => {
    stubFetch({ 'GET /api/v1/documents/9/versions': () => jsonResponse(200, [v(0, 1, { current: true })]) });
    renderWithProviders(<Versions docId={9} readOnly />);
    await screen.findByText('第 1 版');
    expect(screen.queryByRole('button', { name: '从已有文档添加' })).not.toBeInTheDocument();
  });
});

import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { setCsrfToken, setWorkspaceId } from '../api/client';
import type { DocumentDetail } from '../api/documents';

// pdf.js 在 jsdom 里画不了图：换成一份三页的假文档
vi.mock('pdfjs-dist', () => ({
  GlobalWorkerOptions: {},
  getDocument: () => ({
    promise: Promise.resolve({
      numPages: 3,
      getPage: () => Promise.resolve({
        getViewport: () => ({ width: 100, height: 140 }),
        render: () => ({ promise: Promise.resolve(), cancel: () => {} }),
      }),
    }),
    destroy: () => Promise.resolve(),
  }),
}));

const { MergeDialog, PageEditorDialog, SplitDialog } = await import('./page-tools');

const doc = (extra: Partial<DocumentDetail> = {}) => ({
  id: 9, title: '租房合同', createdDate: '2024-03-05', addedAt: '2024-03-05T10:00:00Z', modifiedAt: '2024-03-05T10:00:00Z',
  tagIds: [], noteCount: 0, mimeType: 'application/pdf', originalFilename: '租房合同.pdf', pageCount: 3, hasArchive: false, hasThumbnail: true,
  inbox: false, drawerManual: false, isMilestone: false, content: '', checksum: 'ab', fields: [], fieldSources: {}, source: 'web', ...extra,
}) as DocumentDetail;

const bodyOf = async (calls: Request[], method: string) => {
  const r = calls.find((c) => c.method === method)!;
  return JSON.parse(await r.clone().text());
};

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

describe('PageEditorDialog', () => {
  it('旋转和删除页面后保存完整的目标状态', async () => {
    const calls = stubFetch({ 'PUT /api/v1/documents/9/pages': () => jsonResponse(200, doc({ pageCount: 2 })) });
    const onClose = vi.fn();
    renderWithProviders(<PageEditorDialog d={doc()} open onClose={onClose} />);
    const dlg = await screen.findByRole('dialog', { name: '编辑页面' });
    expect(within(dlg).getByRole('button', { name: '保存' })).toBeDisabled();
    await userEvent.click(await within(dlg).findByRole('button', { name: '第 1 页向右旋转' }));
    await userEvent.click(within(dlg).getByRole('button', { name: '删除第 2 页' }));
    expect(within(dlg).getByText('将删除')).toBeInTheDocument();
    await userEvent.click(within(dlg).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(await bodyOf(calls, 'PUT')).toEqual({ rotate: { 1: 90 }, delete: [2] });
  });

  it('不能删除所有页面；编辑过的可以恢复原样', async () => {
    const calls = stubFetch({ 'PUT /api/v1/documents/9/pages': () => jsonResponse(200, doc()) });
    renderWithProviders(<PageEditorDialog d={doc({ pageEdits: { delete: [3] } })} open onClose={() => {}} />);
    const dlg = await screen.findByRole('dialog', { name: '编辑页面' });
    expect(await within(dlg).findByText('将删除')).toBeInTheDocument();
    await userEvent.click(within(dlg).getByRole('button', { name: '删除第 1 页' }));
    await userEvent.click(within(dlg).getByRole('button', { name: '删除第 2 页' }));
    expect(within(dlg).getByText('至少要保留一页。')).toBeInTheDocument();
    expect(within(dlg).getByRole('button', { name: '保存' })).toBeDisabled();
    await userEvent.click(within(dlg).getByRole('button', { name: '恢复原样' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    expect(await bodyOf(calls, 'PUT')).toEqual({ rotate: {}, delete: [] });
  });
});

describe('PageEditorDialog 调整页序', () => {
  it('前移、后移调整页序，保存编辑之前的页码顺序；按钮按显示位置命名', async () => {
    const calls = stubFetch({ 'PUT /api/v1/documents/9/pages': () => jsonResponse(200, doc()) });
    renderWithProviders(<PageEditorDialog d={doc()} open onClose={() => {}} />);
    const dlg = await screen.findByRole('dialog', { name: '编辑页面' });
    expect(await within(dlg).findByRole('button', { name: '第 1 页前移' })).toBeDisabled();
    expect(within(dlg).getByRole('button', { name: '第 3 页后移' })).toBeDisabled();
    await userEvent.click(within(dlg).getByRole('button', { name: '第 3 页前移' })); // 1 3 2
    await userEvent.click(within(dlg).getByRole('button', { name: '第 2 页前移' })); // 3 1 2
    expect(within(dlg).getByTitle('原来的第 3 页')).toBeInTheDocument();
    await userEvent.click(within(dlg).getByRole('button', { name: '删除第 2 页' })); // the original page 1
    await userEvent.click(within(dlg).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    expect(await bodyOf(calls, 'PUT')).toEqual({ rotate: {}, delete: [1], order: [3, 1, 2] });
  });

  it('移回原来的顺序就不算改动', async () => {
    stubFetch({});
    renderWithProviders(<PageEditorDialog d={doc()} open onClose={() => {}} />);
    const dlg = await screen.findByRole('dialog', { name: '编辑页面' });
    await userEvent.click(await within(dlg).findByRole('button', { name: '第 2 页前移' }));
    expect(within(dlg).getByRole('button', { name: '保存' })).toBeEnabled();
    await userEvent.click(within(dlg).getByRole('button', { name: '第 1 页后移' }));
    expect(within(dlg).getByRole('button', { name: '保存' })).toBeDisabled();
  });
});

describe('SplitDialog', () => {
  it('在第 1 页之后拆开，可以把原文档移到回收站', async () => {
    const calls = stubFetch({ 'POST /api/v1/documents/9/split': () => jsonResponse(202, { jobId: 4 }) });
    renderWithProviders(<SplitDialog d={doc()} open onClose={() => {}} />);
    const dlg = await screen.findByRole('dialog', { name: '拆分' });
    expect(within(dlg).getByRole('button', { name: '拆分' })).toBeDisabled();
    await userEvent.click(await within(dlg).findByRole('button', { name: '在第 1 页之后拆开' }));
    expect(within(dlg).getByText('将拆成 2 份：第 1 页、第 2–3 页')).toBeInTheDocument();
    await userEvent.click(within(dlg).getByLabelText('拆分后把这份文档移到回收站'));
    await userEvent.click(within(dlg).getByRole('button', { name: '拆分' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    expect(await bodyOf(calls, 'POST')).toEqual({ after: [1], trashOriginal: true });
  });
});

describe('MergeDialog', () => {
  it('调整顺序后合并', async () => {
    const calls = stubFetch({ 'POST /api/v1/documents/merge': () => jsonResponse(202, { jobId: 5 }) });
    const onDone = vi.fn();
    renderWithProviders(<MergeDialog docs={[{ id: 1, title: '第一页' }, { id: 2, title: '第二页' }]} open onClose={() => {}} onDone={onDone} />);
    const dlg = await screen.findByRole('dialog', { name: '合并为一份' });
    await userEvent.click(within(dlg).getByRole('button', { name: '下移“第一页”' }));
    await userEvent.click(within(dlg).getByRole('button', { name: '合并' }));
    await waitFor(() => expect(onDone).toHaveBeenCalled());
    expect(await bodyOf(calls, 'POST')).toEqual({ ids: [2, 1], trashOriginals: false });
  });
});

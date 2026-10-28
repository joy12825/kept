import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { setCsrfToken, setWorkspaceId, type Workspace } from '../api/client';
import { ConfirmDialog } from '../components/overlays';
import DuplicatesPage from './duplicates-page';

const home: Workspace = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 2 };
const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const doc = (id: number, title: string) => ({ id, title, createdDate: '2024-03-05', addedAt: '2024-03-05T08:00:00Z', modifiedAt: '2024-03-05T08:00:00Z', mimeType: 'application/pdf',
  originalFilename: 'a.pdf', pageCount: 1, hasArchive: true, hasThumbnail: false, inbox: false, tagIds: [], noteCount: 0 });
const pair = { id: 3, similarity: 98, a: doc(10, '京东发票'), b: doc(11, '京东发票（扫描）'), aSnippet: '电子发票 发票代码 0110', bSnippet: '电子发票：发票代码：0110', foundAt: '2026-10-03T00:00:00Z' };

function backend(list: unknown, extra: Record<string, (r: Request) => Response> = {}) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: home, csrfToken: 'tok' }),
    'GET /api/v1/duplicates': () => jsonResponse(200, list),
    'GET /api/v1/document-types': () => jsonResponse(200, []),
    'GET /api/v1/correspondents': () => jsonResponse(200, []),
    'GET /api/v1/tags': () => jsonResponse(200, []),
    'GET /api/v1/drawers': () => jsonResponse(200, []),
    ...extra,
  });
}

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

describe('近似重复', () => {
  it('还没有检查过时可以立即检查', async () => {
    const calls = backend({ pairs: [], scanning: false }, { 'POST /api/v1/duplicates/scan': () => jsonResponse(202, { jobId: 4 }) });
    renderWithProviders(<DuplicatesPage />);
    expect(await screen.findByText('还没有检查过')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '立即检查' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'POST' && r.url.endsWith('/duplicates/scan'))).toBe(true));
  });

  it('左右并排，保留一份', async () => {
    let list = { pairs: [pair], scanning: false, lastScanAt: new Date().toISOString() };
    const calls = backend(list, { 'POST /api/v1/duplicates/3/keep': () => { list = { ...list, pairs: [] }; return new Response(null, { status: 204 }); } });
    renderWithProviders(<><DuplicatesPage /><ConfirmDialog /></>);
    expect(await screen.findByText('相似度 98%')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '京东发票（扫描）' })).toHaveAttribute('href', '/docs/11');
    await userEvent.click(screen.getAllByRole('button', { name: '保留这份' })[0]);
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(/“京东发票（扫描）”的标签会加到保留的这份上/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: '保留这份' }));
    await waitFor(async () => expect(await calls.find((r) => r.url.endsWith('/keep'))!.clone().json()).toEqual({ documentId: 10 }));
  });

  it('不是重复', async () => {
    const calls = backend({ pairs: [pair], scanning: false }, { 'POST /api/v1/duplicates/3/dismiss': () => new Response(null, { status: 204 }) });
    renderWithProviders(<DuplicatesPage />);
    await userEvent.click(await screen.findByRole('button', { name: '不是重复' }));
    await waitFor(() => expect(calls.some((r) => r.url.endsWith('/duplicates/3/dismiss'))).toBe(true));
  });
});

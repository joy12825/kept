import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { setWorkspaceId } from '../api/client';
import CleanupPage from './cleanup-page';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const ws = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'owner', memberCount: 2 };
const rule = { algorithm: 'auto', match: '', caseInsensitive: true };

afterEach(() => { vi.unstubAllGlobals(); setWorkspaceId(null); });

describe('可以清理（v1.6.5）', () => {
  it('没有设保留期限时说明去哪里设', async () => {
    stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws, csrfToken: 'tok' }),
      'GET /api/v1/document-types': () => jsonResponse(200, [{ id: 10, name: '账单', initial: 'Z', sortKey: 'zhangdan', rule }]),
    });
    renderWithProviders(<CleanupPage />, { route: '/cleanup' });
    expect(await screen.findByText('还没有设置保留期限')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '去设置' })).toBeInTheDocument();
  });

  it('列出超过保留期限的文档：列表按 retention 条件查询', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws, csrfToken: 'tok' }),
      'GET /api/v1/document-types': () => jsonResponse(200, [{ id: 10, name: '账单', initial: 'Z', sortKey: 'zhangdan', rule, retentionMonths: 24 }]),
      'GET /api/v1/correspondents': () => jsonResponse(200, []),
      'GET /api/v1/tags': () => jsonResponse(200, []),
      'GET /api/v1/documents': () => jsonResponse(200, { items: [{ id: 1, title: '2022年1月电费', createdDate: '2022-01-05', addedAt: '2022-01-06T08:00:00Z', modifiedAt: '2022-01-06T08:00:00Z',
        mimeType: 'application/pdf', originalFilename: 'a.pdf', pageCount: 1, hasArchive: false, hasThumbnail: false, inbox: false, tagIds: [], noteCount: 0, documentTypeId: 10 }], total: 1 }),
      'GET /api/v1/documents/facets': () => jsonResponse(200, {}),
    });
    renderWithProviders(<CleanupPage />, { route: '/cleanup' });
    expect(await screen.findByText('2022年1月电费')).toBeInTheDocument();
    expect(screen.getByText(/账单 2 年/)).toBeInTheDocument();
    await waitFor(() => expect(calls.some((r) => r.url.includes('/api/v1/documents?'))).toBe(true));
    const url = new URL(calls.find((r) => r.url.includes('/api/v1/documents?'))!.url);
    expect(JSON.parse(url.searchParams.get('filter')!)).toEqual({ field: 'retention', op: 'expired', value: true });
  });
});

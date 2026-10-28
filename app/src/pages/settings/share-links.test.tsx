import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { ConfirmDialog, Toast } from '../../components/overlays';
import { ShareLinksPanel } from './share-links';

afterEach(() => { vi.unstubAllGlobals(); });

const bundle = { kind: 'bundle', id: 4, title: '3 月报销', documents: 5, token: 'b'.repeat(32), file: 'archive', expiresAt: '2030-01-01T00:00:00Z',
  createdAt: '2026-10-01T00:00:00Z', creatorName: '小王', accessCount: 2, lastAccessAt: new Date().toISOString() };
const link = { kind: 'document', id: 9, title: '租房合同', documentId: 12, documents: 1, token: 's'.repeat(32), file: 'original', expiresAt: '2030-01-01T00:00:00Z',
  createdAt: '2026-09-01T00:00:00Z', creatorName: '小李', accessCount: 0 };

describe('分享链接', () => {
  it('列出单份文档的链接和分享包；作废前确认，作废分享包用它自己的接口', async () => {
    const calls = stubFetch({
      'GET /api/v1/share-links': () => jsonResponse(200, [bundle, link]),
      'DELETE /api/v1/share-bundles/4': () => new Response(null, { status: 204 }),
    });
    renderWithProviders(<><ShareLinksPanel /><ConfirmDialog /><Toast /></>);
    expect(await screen.findByText('分享包 · 5 份')).toBeInTheDocument();
    expect(screen.getByText(/打开 2 次/)).toBeInTheDocument();
    expect(screen.getByText(/还没有人打开/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '作废“3 月报销”' }));
    const dlg = await screen.findByRole('dialog');
    await userEvent.click(within(dlg).getByRole('button', { name: '作废' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url.endsWith('/share-bundles/4'))).toBe(true));
  });

  it('没有有效的分享时说明', async () => {
    stubFetch({ 'GET /api/v1/share-links': () => jsonResponse(200, []) });
    renderWithProviders(<ShareLinksPanel />);
    expect(await screen.findByText('现在没有有效的分享。')).toBeInTheDocument();
  });
});

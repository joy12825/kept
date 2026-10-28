import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { setCsrfToken, setWorkspaceId } from '../api/client';
import { ShareLinkDialog } from './share-link-dialog';

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

const link = (extra: Record<string, unknown> = {}) => ({
  id: 4, documentId: 9, token: 'abcdefgh12345678abcdefgh12345678', file: 'archive', expiresAt: '2026-10-10T08:00:00Z',
  createdAt: '2026-10-03T08:00:00Z', creatorName: '小王', accessCount: 0, ...extra,
});

describe('ShareLinkDialog', () => {
  it('免费版（v1.13.3）：水印是收费功能，输入框变灰并写明原因；照样能生成普通链接', async () => {
    const free = { edition: 'free', trial: false, daysLeft: -1, warn: false, expired: false, features: [], members: { used: 1, limit: 6 }, sharedWorkspaces: { used: 0, limit: 1 } };
    stubFetch({
      'GET /api/v1/edition': () => jsonResponse(200, free),
      'GET /api/v1/me': () => jsonResponse(200, { user: { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false }, csrfToken: 't', features: { watermark: true } }),
      'GET /api/v1/documents/9/share-links': () => jsonResponse(200, []),
    });
    renderWithProviders(<ShareLinkDialog doc={{ id: 9, title: '租房合同', hasArchive: true, mimeType: 'application/pdf' }} open onClose={() => {}} />);
    expect(await screen.findByText('加水印是收费版的功能')).toBeInTheDocument();
    expect(screen.getByLabelText('水印文字（可不填）')).toBeDisabled();
    expect(screen.getByRole('button', { name: '生成链接' })).toBeEnabled();
  });

  it('生成链接：选有效期和文件，显示新链接', async () => {
    let links: unknown[] = [];
    const calls = stubFetch({
      'GET /api/v1/documents/9/share-links': () => jsonResponse(200, links),
      'POST /api/v1/documents/9/share-links': () => { links = [link({ file: 'original' })]; return jsonResponse(201, links[0]); },
    });
    renderWithProviders(<ShareLinkDialog doc={{ id: 9, title: '租房合同', hasArchive: true, mimeType: 'image/jpeg' }} open onClose={() => {}} />);
    expect(await screen.findByText('还没有分享过，或者链接都已过期、作废。')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('combobox', { name: '有效期' }));
    await userEvent.click(await screen.findByRole('option', { name: '30 天' }));
    await userEvent.click(screen.getByRole('combobox', { name: '文件' }));
    await userEvent.click(await screen.findByRole('option', { name: '原件' }));
    await userEvent.click(screen.getByRole('button', { name: '生成链接' }));
    expect(await screen.findByDisplayValue(`${window.location.origin}/s/abcdefgh12345678abcdefgh12345678`)).toBeInTheDocument();
    const body = await calls.find((r) => r.method === 'POST')!.clone().json();
    expect(body).toEqual({ days: 30, file: 'original', allowDownload: true });
    expect(await screen.findByText(/原件 · 小王 生成/)).toBeInTheDocument();
  });

  it('列出有效的链接，可以作废', async () => {
    let links: unknown[] = [link({ accessCount: 3, lastAccessAt: new Date().toISOString() })];
    stubFetch({
      'GET /api/v1/documents/9/share-links': () => jsonResponse(200, links),
      'DELETE /api/v1/share-links/4': () => { links = []; return new Response(null, { status: 204 }); },
    });
    renderWithProviders(<ShareLinkDialog doc={{ id: 9, title: '租房合同', hasArchive: false, mimeType: 'text/plain' }} open onClose={() => {}} />);
    expect(await screen.findByText(/打开 3 次/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '作废链接' }));
    await waitFor(() => expect(screen.getByText('还没有分享过，或者链接都已过期、作废。')).toBeInTheDocument());
  });

  it('加水印、只能在线看（v1.9.2）', async () => {
    let links: unknown[] = [];
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user: { id: 1, username: 'wang' }, workspace: { id: 1, name: '王家' }, features: { smtp: false, watermark: true } }),
      'GET /api/v1/documents/9/share-links': () => jsonResponse(200, links),
      'POST /api/v1/documents/9/share-links': () => { links = [link({ watermark: '仅供银行办理房贷', allowDownload: false })]; return jsonResponse(201, links[0]); },
    });
    renderWithProviders(<ShareLinkDialog doc={{ id: 9, title: '租房合同', hasArchive: true, mimeType: 'image/jpeg' }} open onClose={() => {}} />);
    const wm = await screen.findByRole('textbox', { name: '水印文字（可不填）' });
    await waitFor(() => expect(wm).toBeEnabled());
    await userEvent.type(wm, '仅供银行办理房贷');
    await userEvent.click(screen.getByRole('switch', { name: '允许下载' }));
    expect(screen.getByText(/要防外传请同时加水印/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '生成链接' }));
    await screen.findByText(/水印“仅供银行办理房贷” · 只能在线看/);
    const body = await calls.find((r) => r.method === 'POST')!.clone().json();
    expect(body).toEqual({ days: 7, file: 'archive', watermark: '仅供银行办理房贷', allowDownload: false });
  });

  it('没有 PDF 的文档不能加水印', async () => {
    stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user: { id: 1, username: 'wang' }, workspace: { id: 1, name: '王家' }, features: { smtp: false, watermark: true } }),
      'GET /api/v1/documents/9/share-links': () => jsonResponse(200, []),
    });
    renderWithProviders(<ShareLinkDialog doc={{ id: 9, title: '笔记', hasArchive: false, mimeType: 'text/markdown' }} open onClose={() => {}} />);
    expect(await screen.findByText('这份文档没有 PDF 或图片，不能加水印')).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: '水印文字（可不填）' })).toBeDisabled();
  });
});

import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import PublicSharePage from './public-share-page';

afterEach(() => { vi.unstubAllGlobals(); });

const printBlocked = () => Array.from(document.querySelectorAll('style')).some((x) => /@media print[^}]*body[^}]*display:\s*none/.test(x.textContent ?? ''));

const page = () => renderWithProviders(<Routes><Route path="/s/:token" element={<PublicSharePage />} /></Routes>, { route: '/s/tok123456789012345678901234567890' });

describe('PublicSharePage', () => {
  it('显示标题、文字预览和下载，不需要登录', async () => {
    stubFetch({
      'GET /api/v1/branding': () => jsonResponse(200, { name: '收好' }),
      'GET /api/v1/public/shares/tok123456789012345678901234567890': () => jsonResponse(200, { title: '租房合同', filename: '租房合同.txt', mimeType: 'text/plain', pageCount: 0, size: 2048, expiresAt: '2026-10-10T08:00:00Z', allowDownload: true, watermarked: false }),
      'GET /api/v1/public/shares/tok123456789012345678901234567890/file': () => new Response('甲方 乙方 房租 3000 元', { headers: { 'Content-Type': 'text/plain' } }),
    });
    page();
    expect(await screen.findByRole('heading', { name: '租房合同' })).toBeInTheDocument();
    expect(await screen.findByText('甲方 乙方 房租 3000 元')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '下载' })).toHaveAttribute('href', '/api/v1/public/shares/tok123456789012345678901234567890/file?download=true');
    expect(document.querySelector('meta[name=robots]')?.getAttribute('content')).toBe('noindex, nofollow');
    // v1.9.7：允许下载时也能打印
    expect(printBlocked()).toBe(false);
    await userEvent.click(screen.getByRole('button', { name: '打印' }));
    await waitFor(() => expect(document.querySelector('iframe[aria-hidden="true"]')).not.toBeNull());
  });

  it('只能在线查看时没有下载按钮，页头写明（v1.9.2）', async () => {
    stubFetch({
      'GET /api/v1/branding': () => jsonResponse(200, { name: '收好' }),
      'GET /api/v1/public/shares/tok123456789012345678901234567890': () => jsonResponse(200, { title: '租房合同', filename: '租房合同.pdf', mimeType: 'application/zip', pageCount: 3, size: 2048, expiresAt: '2026-10-10T08:00:00Z', allowDownload: false, watermarked: true }),
    });
    page();
    expect(await screen.findByText(/只能在线查看 · 已加水印/)).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: '下载' })).toBeNull();
    expect(screen.getByText(/分享的人没有允许下载/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '下载原件' })).toBeNull();
    expect(screen.queryByRole('button', { name: '打印' })).toBeNull(); // v1.9.7：也不能打印，浏览器打印出空白
    expect(printBlocked()).toBe(true);
  });

  it('失效的链接', async () => {
    stubFetch({
      'GET /api/v1/branding': () => jsonResponse(200, { name: '收好' }),
      'GET /api/v1/public/shares/tok123456789012345678901234567890': () => jsonResponse(404, { code: 'share_expired', message: '这个链接已失效' }),
    });
    page();
    expect(await screen.findByText('这个链接已失效')).toBeInTheDocument();
    expect(screen.getByText(/链接已经过期或被作废了/)).toBeInTheDocument();
  });
});

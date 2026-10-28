import { eeBuild } from '@ee';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import PublicBundlePage from './public-bundle-page';

vi.mock('../components/pdf-preview', () => ({ PdfPreview: ({ url }: { url: string }) => <div data-testid="pdf">{url}</div> }));

afterEach(() => { vi.unstubAllGlobals(); });

const printBlocked = () => Array.from(document.querySelectorAll('style')).some((x) => /@media print[^}]*body[^}]*display:\s*none/.test(x.textContent ?? ''));

const token = 't'.repeat(32);
const render = () => renderWithProviders(<Routes><Route path="/b/:token" element={<PublicBundlePage />} /></Routes>, { route: `/b/${token}` });

describe('分享包的公开页面', () => {
  it('列出文档，点哪份预览哪份；可以逐份下载和全部下载', async () => {
    stubFetch({
      'GET /api/v1/branding': () => jsonResponse(200, { name: '收好' }),
      [`GET /api/v1/public/bundles/${token}`]: () => jsonResponse(200, {
        title: '3 月报销', expiresAt: '2030-01-01T00:00:00Z', allowDownload: true, watermarked: false, documents: [
          { id: 1, title: '停车费', date: '2024-03-05', filename: '停车费.pdf', mimeType: 'application/pdf', pageCount: 1, size: 2048 },
          { id: 2, title: '餐费', date: '2024-03-06', filename: '餐费.pdf', mimeType: 'application/pdf', pageCount: 2, size: 4096 },
        ],
      }),
    });
    render();
    expect(await screen.findByRole('heading', { name: '3 月报销' })).toBeInTheDocument();
    expect(screen.getByText(/2 份文档/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '全部下载' })).toHaveAttribute('href', `/api/v1/public/bundles/${token}/zip`);
    expect(await screen.findByTestId('pdf')).toHaveTextContent(`/documents/1/file`);
    await userEvent.click(screen.getByText('餐费'));
    expect(await screen.findByTestId('pdf')).toHaveTextContent(`/documents/2/file`);
    expect(screen.getByRole('link', { name: '下载“餐费”' })).toHaveAttribute('href', `/api/v1/public/bundles/${token}/documents/2/file?download=true`);
    expect(screen.getByRole('button', { name: '打印“餐费”' })).toBeInTheDocument(); // v1.9.7
  });

  it('只能在线查看时没有下载（v1.9.2）', async () => {
    stubFetch({
      'GET /api/v1/branding': () => jsonResponse(200, { name: '收好' }),
      [`GET /api/v1/public/bundles/${token}`]: () => jsonResponse(200, {
        title: '房贷材料', expiresAt: '2030-01-01T00:00:00Z', allowDownload: false, watermarked: true, documents: [
          { id: 1, title: '收入证明', date: '2024-03-05', filename: '收入证明.pdf', mimeType: 'application/pdf', pageCount: 1, size: 2048 },
        ],
      }),
    });
    render();
    expect(await screen.findByText(/只能在线查看 · 已加水印/)).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: '全部下载' })).toBeNull();
    expect(screen.queryByRole('link', { name: '下载“收入证明”' })).toBeNull();
    expect(screen.queryByRole('button', { name: '打印“收入证明”' })).toBeNull(); // v1.9.7：也不能打印
    expect(printBlocked()).toBe(true);
  });

  it.skipIf(!eeBuild)('给医生看的合集先显示检查结果汇总（v1.8.3）', async () => {
    const m = { id: 90, name: '甲状腺结节', aliases: [], category: '检查所见', specimen: 'other', unit: 'mm', kind: 'number' };
    stubFetch({
      'GET /api/v1/branding': () => jsonResponse(200, { name: '收好' }),
      [`GET /api/v1/public/bundles/${token}`]: () => jsonResponse(200, {
        title: '爸爸的检查结果', expiresAt: '2030-01-01T00:00:00Z', allowDownload: true, watermarked: false,
        documents: [{ id: 12, title: '甲状腺彩超复查报告', date: '2026-04-24', filename: 'a.txt', mimeType: 'text/plain', pageCount: 1, size: 300 }],
        summary: {
          personName: '爸爸', metrics: [{ metric: m, points: [{ documentId: 12, date: '2026-04-24', value: 5, text: '5×4', flag: '' }] }],
          notes: [{ documentId: 12, date: '2026-04-24', title: '甲状腺彩超复查报告', conclusion: '甲状腺右叶结节', diagnosis: '', advice: '', recheckOn: '2026-10-24' }],
          documents: [{ id: 12, title: '甲状腺彩超复查报告', date: '2026-04-24' }],
        },
      }),
    });
    render();
    expect(await screen.findByText('爸爸的检查结果汇总')).toBeInTheDocument();
    expect(screen.getByText('5×4 mm')).toBeInTheDocument();
    expect(screen.getByText('建议复查：2026-10-24')).toBeInTheDocument();
    expect(screen.queryByTestId('pdf')).toBeNull();
  });

  it('失效时说明', async () => {
    stubFetch({
      'GET /api/v1/branding': () => jsonResponse(200, { name: '收好' }),
      [`GET /api/v1/public/bundles/${token}`]: () => jsonResponse(404, { code: 'share_expired', message: '这个链接已失效' }),
    });
    render();
    expect(await screen.findByText('这个链接已失效')).toBeInTheDocument();
    expect(screen.getByText(/过期或被作废/)).toBeInTheDocument();
  });
});

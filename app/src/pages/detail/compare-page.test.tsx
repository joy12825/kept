import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import ComparePage from './compare-page';

vi.mock('../../components/pdf-preview', () => ({ PdfPreview: ({ url }: { url: string }) => <div data-testid="pdf">{url}</div> }));
afterEach(() => { vi.unstubAllGlobals(); });

const v = (id: number, number: number, extra: Record<string, unknown> = {}) => ({
  id, number, label: '', filename: `合同-${number}.pdf`, mimeType: 'application/pdf', size: 2048, encrypted: false, current: false,
  creatorName: '小王', createdAt: '2026-10-01T00:00:00Z', ...extra,
});
const doc = { id: 9, title: '租房合同', mimeType: 'application/pdf', hasArchive: false, modifiedAt: '2026-10-01T00:00:00Z' };

const render = (route = '/docs/9/compare') => renderWithProviders(
  <Routes><Route path="/docs/:id/compare" element={<ComparePage />} /><Route path="/docs/:id" element={<p>详情</p>} /></Routes>, { route });

describe('版本对比（v1.9.2）', () => {
  it('默认拿当前版本和上一版比文字，改动的字标出来', async () => {
    stubFetch({
      'GET /api/v1/documents/9': () => jsonResponse(200, doc),
      'GET /api/v1/documents/9/versions': () => jsonResponse(200, [v(13, 3, { current: true, label: '续签' }), v(12, 2), v(11, 1)]),
      'GET /api/v1/documents/9/versions/12/text': () => jsonResponse(200, { text: '租房合同\n房租 3000 元\n押一付三', source: 'file' }),
      'GET /api/v1/documents/9/versions/13/text': () => jsonResponse(200, { text: '租房合同\n房租 3200 元\n押一付三\n水电自理', source: 'content' }),
    });
    render();
    expect(await screen.findByText('租房合同', { selector: 'span' })).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: '旧版本' })).toHaveTextContent('第 2 版');
    expect(screen.getByRole('combobox', { name: '新版本' })).toHaveTextContent('第 3 版 · 续签（当前）');
    expect(await screen.findByText(/增加 2 行，删去 1 行/)).toBeInTheDocument();
    expect(screen.getByText(/当前版本的文字含识别结果/)).toBeInTheDocument();
    const table = screen.getByRole('table', { name: '文字差异' });
    expect(table.querySelectorAll('[data-kind="add"]')).toHaveLength(2);
    expect(table.querySelector('ins')).toHaveTextContent('2');
    expect(table.querySelector('del')).toHaveTextContent('0');
  });

  it('换版本、并排看', async () => {
    stubFetch({
      'GET /api/v1/documents/9': () => jsonResponse(200, doc),
      'GET /api/v1/documents/9/versions': () => jsonResponse(200, [v(13, 3, { current: true }), v(12, 2), v(11, 1, { mimeType: 'image/jpeg', filename: '合同.jpg' })]),
      'GET /api/v1/documents/9/versions/11/text': () => jsonResponse(200, { text: '', source: 'none' }),
      'GET /api/v1/documents/9/versions/12/text': () => jsonResponse(200, { text: 'a', source: 'file' }),
      'GET /api/v1/documents/9/versions/13/text': () => jsonResponse(200, { text: 'b', source: 'content' }),
    });
    render();
    await userEvent.click(await screen.findByRole('combobox', { name: '旧版本' }));
    await userEvent.click(await screen.findByRole('option', { name: '第 1 版' }));
    expect(await screen.findByText(/第 1 版是扫描件或照片，读不出文字，只能并排看/)).toBeInTheDocument();
    await userEvent.click(within(screen.getByRole('alert')).getByRole('button', { name: '并排看' }));
    expect(await screen.findByRole('img', { name: '第 1 版' })).toHaveAttribute('src', '/api/v1/documents/9/versions/11/file?inline=true');
    expect(await screen.findByTestId('pdf')).toHaveTextContent('/api/v1/documents/9/versions/13/file?inline=true');
    expect(screen.getByRole('checkbox', { name: '同步滚动' })).toBeChecked();
  });

  it('长段没改的内容折叠，可以展开', async () => {
    const lines = Array.from({ length: 30 }, (_, i) => `第 ${i + 1} 条`);
    const changed = [...lines];
    changed[29] = '第 30 条（改）';
    stubFetch({
      'GET /api/v1/documents/9': () => jsonResponse(200, doc),
      'GET /api/v1/documents/9/versions': () => jsonResponse(200, [v(13, 2, { current: true }), v(12, 1)]),
      'GET /api/v1/documents/9/versions/12/text': () => jsonResponse(200, { text: lines.join('\n'), source: 'file' }),
      'GET /api/v1/documents/9/versions/13/text': () => jsonResponse(200, { text: changed.join('\n'), source: 'file' }),
    });
    render();
    await userEvent.click(await screen.findByRole('button', { name: /展开 26 行没有改动的内容/ }));
    const table = screen.getByRole('table', { name: '文字差异' });
    await waitFor(() => expect(within(table).getByText('第 1 条')).toBeInTheDocument());
  });

  it('只有一个版本', async () => {
    stubFetch({
      'GET /api/v1/documents/9': () => jsonResponse(200, doc),
      'GET /api/v1/documents/9/versions': () => jsonResponse(200, [v(0, 1, { current: true })]),
    });
    render();
    expect(await screen.findByText('只有一个版本')).toBeInTheDocument();
  });
});

describe('并排看 Office、邮件、OFD 的版本（v1.9.7）', () => {
  const word = (id: number, number: number, extra: Record<string, unknown> = {}) =>
    v(id, number, { mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', filename: `装修合同-${number}.docx`, ...extra });

  it('看服务器转出的 PDF；转不了时说明原因', async () => {
    URL.createObjectURL = vi.fn(() => 'blob:pdf-13');
    stubFetch({
      'GET /api/v1/documents/9': () => jsonResponse(200, { ...doc, mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', hasArchive: true }),
      'GET /api/v1/documents/9/versions': () => jsonResponse(200, [word(13, 2, { current: true }), word(12, 1)]),
      'GET /api/v1/documents/9/versions/12/preview': () => jsonResponse(409, { code: 'no_converter', message: '要配好文档转换服务（Gotenberg）才能在网页上显示' }),
      'GET /api/v1/documents/9/versions/13/preview': () => new Response('%PDF-1.4', { status: 200, headers: { 'Content-Type': 'application/pdf' } }),
    });
    render('/docs/9/compare?view=side');
    expect(await screen.findByTestId('pdf')).toHaveTextContent('blob:pdf-13');
    expect(await screen.findByText(/要配好文档转换服务/)).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: /下载/ }).length).toBeGreaterThan(0);
  });
});

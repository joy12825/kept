import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { DocPreview } from '../doc-preview';
import { defaultMode, modeLabel, previewKind, previewModes } from './kinds';
import { colName } from './sheet-view';
import { buildTree } from './zip-view';

vi.mock('../pdf-preview', () => ({ PdfPreview: ({ url }: { url: string }) => <div data-testid="pdf">{url}</div> }));
afterEach(() => { vi.unstubAllGlobals(); });

const XLSX = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';

describe('表格、压缩包、OFD 的显示（v1.9.3）', () => {
  it('按文件列出显示', () => {
    const xlsx = { mimeType: XLSX, hasArchive: true, originalFilename: '账本.xlsx' };
    expect(previewKind(xlsx)).toBe('sheet');
    expect(previewModes(xlsx)).toEqual(['original', 'archive', 'text']);
    expect(defaultMode(xlsx)).toBe('original');
    expect(modeLabel(xlsx, 'original')).toBe('表格');
    const csv = { mimeType: 'text/plain', hasArchive: false, originalFilename: '电表.csv' };
    expect(previewModes(csv)).toEqual(['original', 'text']);
    expect(modeLabel(csv, 'text')).toBe('文本');
    expect(previewModes({ mimeType: 'application/zip', hasArchive: false })).toEqual(['original']);
    const ofd = { mimeType: 'application/ofd', hasArchive: true };
    expect(previewModes(ofd)).toEqual(['archive', 'text']);
    expect(defaultMode(ofd)).toBe('archive');
    // v1.9.7：演示文稿默认一张一张地看幻灯片；还没转成 PDF 时只有文字
    const pptx = { mimeType: 'application/vnd.openxmlformats-officedocument.presentationml.presentation', hasArchive: true };
    expect(previewKind(pptx)).toBe('slides');
    expect(previewModes(pptx)).toEqual(['original', 'archive', 'text']);
    expect(defaultMode(pptx)).toBe('original');
    expect(modeLabel(pptx, 'original')).toBe('幻灯片');
    expect(previewModes({ ...pptx, hasArchive: false })).toEqual(['text']);
    expect(colName(0)).toBe('A');
    expect(colName(26)).toBe('AA');
  });

  it('表格：列号、行号、合并的单元格、切换表、截断提示', async () => {
    stubFetch({
      'GET /api/v1/documents/5/sheets': () => jsonResponse(200, { sheets: [
        { name: '支出', rows: [['2026 年家庭支出'], ['日期', '项目', '金额'], ['2026-10-04', '电费', '212.4']], merges: [[0, 0, 0, 2]], truncated: true },
        { name: '收入', rows: [['工资', '18000']], merges: [], truncated: false },
      ] }),
    });
    renderWithProviders(<DocPreview doc={{ id: 5, mimeType: XLSX, hasArchive: false, originalFilename: '账本.xlsx' }} mode="original" />);
    const grid = await screen.findByRole('grid');
    expect(within(grid).getByRole('columnheader', { name: 'C' })).toBeInTheDocument();
    const title = within(grid).getByText('2026 年家庭支出');
    expect(title).toHaveAttribute('colspan', '3');
    expect(within(grid).getByText('212.4')).toBeInTheDocument();
    expect(screen.getByText(/只显示前 10000 行/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('tab', { name: '收入' }));
    expect(await screen.findByText('18000')).toBeInTheDocument();
  });

  it('共享来的表格看可搜索版', async () => {
    renderWithProviders(<DocPreview doc={{ id: 5, mimeType: XLSX, hasArchive: true }} mode="original" fileUrlOf={(k) => `/shared/${k}`} />);
    expect(await screen.findByTestId('pdf')).toHaveTextContent('/shared/preview');
  });

  it('压缩包：文件树、勾选导入、没有导入的原因', async () => {
    const calls = stubFetch({
      'GET /api/v1/documents/8/entries': () => jsonResponse(200, { truncated: false, entries: [
        { path: '报销', size: 0, dir: true, encrypted: false },
        { path: '报销/停车费.txt', size: 120, dir: false, encrypted: false },
        { path: '报销/酒店/住宿发票.txt', size: 2048, dir: false, encrypted: false },
        { path: '密码.pdf', size: 10, dir: false, encrypted: true },
      ] }),
      'POST /api/v1/documents/8/entries/import': () => jsonResponse(200, { results: [
        { path: '报销/停车费.txt', jobId: 31 }, { path: '报销/酒店/住宿发票.txt', reason: '已经有这份文档了' },
      ] }),
    });
    renderWithProviders(<DocPreview doc={{ id: 8, mimeType: 'application/zip', hasArchive: false }} mode="original" canImport />);
    expect(await screen.findByText('3 个文件，勾选后可以导入为单独的文档')).toBeInTheDocument();
    expect(screen.getByText('2.0 KB')).toBeInTheDocument();
    expect(screen.getByText('有密码')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('checkbox', { name: '选择“报销”' }));
    await userEvent.click(screen.getByRole('button', { name: '导入选中的 2 个文件' }));
    expect(await screen.findByText(/1 个文件没有导入/)).toBeInTheDocument();
    expect(screen.getByText('报销/酒店/住宿发票.txt：已经有这份文档了')).toBeInTheDocument();
    const body = await calls.find((r) => r.method === 'POST')!.clone().json();
    expect([...body.paths].sort()).toEqual(['报销/停车费.txt', '报销/酒店/住宿发票.txt']);
    await userEvent.click(screen.getByRole('button', { name: '收起“报销”' }));
    await waitFor(() => expect(screen.queryByText('停车费.txt')).toBeNull());
  });

  it('只能看的人没有勾选和导入', async () => {
    stubFetch({ 'GET /api/v1/documents/8/entries': () => jsonResponse(200, { truncated: false, entries: [{ path: 'a.txt', size: 1, dir: false, encrypted: false }] }) });
    renderWithProviders(<DocPreview doc={{ id: 8, mimeType: 'application/zip', hasArchive: false }} mode="original" />);
    expect(await screen.findByText('a.txt')).toBeInTheDocument();
    expect(screen.queryByRole('checkbox')).toBeNull();
    expect(screen.queryByRole('button', { name: /导入/ })).toBeNull();
  });

  it('文件树：目录在前，按名称', () => {
    const t = buildTree([{ path: 'b.txt', size: 1, dir: false, encrypted: false }, { path: 'z/a.txt', size: 2, dir: false, encrypted: false }, { path: 'a.txt', size: 3, dir: false, encrypted: false }]);
    expect(t.map((n) => n.name)).toEqual(['z', 'a.txt', 'b.txt']);
    expect(t[0].size).toBe(2);
  });

  it('OFD 的可搜索版写明印章没有画', async () => {
    renderWithProviders(<DocPreview doc={{ id: 9, mimeType: 'application/ofd', hasArchive: true }} mode="archive" />);
    expect(screen.getByText(/印章画在原来的位置/)).toBeInTheDocument();
  });
});

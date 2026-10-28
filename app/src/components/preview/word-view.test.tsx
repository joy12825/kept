import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { WordView } from './word-view';
import { defaultMode, modeLabel, previewKind, previewModes, WORD } from './kinds';

afterEach(() => { vi.unstubAllGlobals(); });

const doc = {
  baseSize: 10.5, truncated: false,
  images: [{ width: 240, height: 160 }],
  notes: [{ n: 1, blocks: [{ kind: 'paragraph', runs: [{ text: '夏天要短一些。' }] }] }],
  blocks: [
    { kind: 'paragraph', heading: 1, runs: [{ text: '家庭应急手册', size: 20, bold: true }] },
    { kind: 'paragraph', list: { marker: '1.', level: 0 }, runs: [{ text: '先看是不是只有我们家停电' }] },
    { kind: 'paragraph', list: { marker: 'a)', level: 1 }, runs: [{ text: '空气开关跳闸' }] },
    { kind: 'paragraph', runs: [{ text: '冰箱：' }, { text: '不要频繁开门', bold: true, color: 'C00000' }, { note: 1, vert: 'super' }, { text: '停电', highlight: 'FFFF00' }] },
    { kind: 'paragraph', runs: [{ text: '官网', link: 'https://www.mem.gov.cn/' }, { text: '坏链接', link: 'javascript:alert(1)' }, { image: 1 }] },
    { kind: 'box', blocks: [{ kind: 'paragraph', runs: [{ text: '出门记得关总水阀' }] }] },
    { kind: 'table', headerRows: 1, rows: [
      [{ colSpan: 1, rowSpan: 1, blocks: [{ kind: 'paragraph', runs: [{ text: '类别' }] }] }, { colSpan: 1, rowSpan: 1, blocks: [{ kind: 'paragraph', runs: [{ text: '电话' }] }] }],
      [{ colSpan: 1, rowSpan: 2, blocks: [{ kind: 'paragraph', runs: [{ text: '小区' }] }] }, { colSpan: 1, rowSpan: 1, blocks: [{ kind: 'paragraph', runs: [{ text: '停电找物业' }] }] }],
      [{ colSpan: 1, rowSpan: 1, blocks: [{ kind: 'paragraph', runs: [{ text: '8888' }] }] }],
      [{ colSpan: 2, rowSpan: 1, blocks: [{ kind: 'paragraph', runs: [{ text: '120 / 119 / 110' }] }] }],
    ] },
  ],
};

describe('Word 阅读视图（v1.9.8）', () => {
  it('切换和默认显示：窄屏默认阅读，宽屏有可搜索版时看可搜索版', () => {
    const d = { mimeType: WORD, hasArchive: true };
    expect(previewKind(d)).toBe('word');
    expect(previewModes(d)).toEqual(['original', 'archive', 'text']);
    expect(modeLabel(d, 'original')).toBe('阅读');
    expect(defaultMode(d)).toBe('archive');
    expect(defaultMode(d, true)).toBe('original');
    expect(previewModes({ ...d, hasArchive: false })).toEqual(['original', 'text']);
    expect(defaultMode({ ...d, hasArchive: false })).toBe('original');
  });

  it('旧的 .doc 和 .odt 也有阅读视图（v1.11.1）', () => {
    for (const mimeType of ['application/msword', 'application/vnd.oasis.opendocument.text']) {
      const d = { mimeType, hasArchive: true };
      expect(previewKind(d)).toBe('word');
      expect(previewModes(d)).toEqual(['original', 'archive', 'text']);
      expect(defaultMode(d, true)).toBe('original');
    }
    expect(previewKind({ mimeType: 'application/vnd.oasis.opendocument.spreadsheet', hasArchive: true })).toBe('none');
  });

  it('按块排版：标题、编号、格式、链接、图片、文本框、表格、脚注', async () => {
    stubFetch({ 'GET /api/v1/documents/7/word': () => jsonResponse(200, doc) });
    renderWithProviders(<WordView docId={7} onDownload={() => {}} />);
    expect(await screen.findByRole('heading', { level: 1, name: '家庭应急手册' })).toBeInTheDocument();
    expect(screen.getByText(/按屏幕宽度排版，版式以可搜索版为准/)).toBeInTheDocument();
    expect(screen.getByText('1.')).toBeInTheDocument();
    expect(screen.getByText('a)')).toBeInTheDocument();
    expect(screen.getByText('不要频繁开门')).toHaveStyle({ fontWeight: '600', color: '#C00000' });
    expect(screen.getByRole('link', { name: '官网' })).toHaveAttribute('href', 'https://www.mem.gov.cn/');
    expect(screen.getByRole('link', { name: '官网' })).toHaveAttribute('rel', 'noopener noreferrer');
    expect(screen.queryByRole('link', { name: '坏链接' })).toBeNull();
    expect(screen.getByText('坏链接')).toBeInTheDocument();
    expect(screen.getByRole('img', { name: '图片 1' })).toHaveAttribute('src', '/api/v1/documents/7/word/images/1');
    expect(screen.getByText('出门记得关总水阀')).toBeInTheDocument();
    const table = screen.getByRole('table');
    expect(within(table).getAllByRole('columnheader')).toHaveLength(2);
    expect(within(table).getByText('小区').closest('td')).toHaveAttribute('rowspan', '2');
    expect(within(table).getByText('120 / 119 / 110').closest('td')).toHaveAttribute('colspan', '2');

    // the note reference and the note go to each other
    const scroll = vi.fn();
    Element.prototype.scrollIntoView = scroll;
    await userEvent.click(screen.getByRole('button', { name: '脚注 1' }));
    expect(scroll.mock.contexts[0]).toBe(document.getElementById('word-note-1'));
    await userEvent.click(screen.getByRole('button', { name: '回到正文的第 1 处引用' }));
    expect(scroll.mock.contexts[1]).toBe(document.getElementById('word-ref-1'));
    expect(screen.getByText('夏天要短一些。')).toBeInTheDocument();
  });

  it('查找：在正文、表格和脚注里标出，报回总数', async () => {
    stubFetch({ 'GET /api/v1/documents/7/word': () => jsonResponse(200, doc) });
    const onCount = vi.fn();
    renderWithProviders(<WordView docId={7} onDownload={() => {}} find={{ terms: ['停电'], current: 1, seq: 1, onCount }} />);
    await screen.findByRole('heading', { level: 1 });
    await waitFor(() => expect(onCount).toHaveBeenCalledWith(3));
    const marks = screen.getByTestId('word').querySelectorAll('mark');
    expect(marks).toHaveLength(3);
    expect(marks[1]).toHaveAttribute('data-find-current');
  });

  it('读不出来时说明原因，可以下载原件', async () => {
    stubFetch({ 'GET /api/v1/documents/7/word': () => jsonResponse(400, { code: 'word_unreadable', message: '这份 Word 文档读不出来，可能已损坏' }) });
    const onDownload = vi.fn();
    renderWithProviders(<WordView docId={7} onDownload={onDownload} />);
    expect(await screen.findByText(/读不出来，可能已损坏/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /下载/ }));
    expect(onDownload).toHaveBeenCalled();
  });
});

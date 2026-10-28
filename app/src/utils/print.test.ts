import { afterEach, describe, expect, it, vi } from 'vitest';
import { printDocument } from './print';

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); document.querySelectorAll('iframe').forEach((f) => f.remove()); });

const base = { id: 7, title: '<租约>', content: '甲方 & 乙方', modifiedAt: '2024-10-01T08:00:00Z' };

describe('打印', () => {
  it('PDF：先取成 blob，在隐藏的框架里打开', async () => {
    const fetchMock = vi.fn(async (_url: string) => new Response(new Blob(["%PDF-1.4"]), { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    const blobUrl = vi.fn(() => 'blob:x');
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: blobUrl, revokeObjectURL: vi.fn() }));
    await printDocument({ ...base, mimeType: 'application/pdf', hasArchive: true });
    expect(String(fetchMock.mock.calls[0][0])).toContain('/api/v1/documents/7/preview');
    const f = document.querySelector('iframe')!;
    expect(f.getAttribute('src')).toBe('blob:x');
    expect(f.getAttribute('aria-hidden')).toBe('true');
  });

  it('文本文档打印文字，转义特殊字符', async () => {
    await printDocument({ ...base, mimeType: 'text/plain', hasArchive: false });
    const html = document.querySelector('iframe')!.getAttribute('srcdoc')!;
    expect(html).toContain('<title>&lt;租约&gt;</title>');
    expect(html).toContain('甲方 &amp; 乙方');
  });

  it('取不到文件时报错', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('', { status: 404 })));
    await expect(printDocument({ ...base, mimeType: 'image/png', hasArchive: false })).rejects.toThrow('文件没能取到');
  });
});

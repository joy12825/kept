import { describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { DocPreview, PreviewToggle, usePreviewMode } from './doc-preview';
import { defaultMode, previewKind, previewModes } from './preview/kinds';

vi.mock('./pdf-preview', () => ({ PdfPreview: ({ url }: { url: string }) => <div data-testid="pdf">{url}</div> }));

describe('EPUB 预览', () => {
  const epub = { id: 9, mimeType: 'application/epub+zip', title: '唐诗选读', hasThumbnail: true, hasArchive: false };

  it('显示封面和下载，不用 pdf.js', () => {
    render(<DocPreview doc={epub} mode="original" />);
    expect(screen.getByRole('img', { name: '封面' })).toHaveAttribute('src', '/api/v1/documents/9/thumbnail');
    expect(screen.getByText(/EPUB 电子书/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '下载' })).toBeInTheDocument();
    expect(screen.queryByTestId('pdf')).not.toBeInTheDocument();
  });

  it('没有封面时显示书名；共享来的文档用共享的地址', () => {
    render(<DocPreview doc={{ ...epub, hasThumbnail: false }} mode="original" />);
    expect(screen.getByText('唐诗选读')).toBeInTheDocument();
    render(<DocPreview doc={epub} mode="original" fileUrlOf={(k) => `/shared/${k}`} />);
    const imgs = screen.getAllByRole('img', { name: '封面' });
    expect(imgs[imgs.length - 1]).toHaveAttribute('src', '/shared/thumbnail');
  });

  it('切换项叫“封面 / 正文”，正文显示提取的文字', () => {
    render(<PreviewToggle doc={epub} mode="text" onMode={() => {}} />);
    expect(screen.getByRole('button', { name: '封面' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '正文' })).toBeInTheDocument();
    render(<DocPreview doc={epub} mode="text" content="床前明月光" />);
    expect(screen.getByText('床前明月光')).toBeInTheDocument();
  });
});

describe('预览器：能看的几种显示（v1.9.1）', () => {
  const photo = { id: 1, mimeType: 'image/jpeg', hasArchive: true, originalFilename: '阳台的花.jpg' };
  const scan = { id: 2, mimeType: 'application/pdf', hasArchive: true, originalFilename: '账单.pdf' };
  // 旧的 .doc：docx 从 v1.9.8 起另有阅读视图
  const office = { id: 3, mimeType: 'application/vnd.ms-excel', hasArchive: false, originalFilename: '账本.xls' }; // v1.11.1: a .doc has the reading view

  it('按文件列出、选默认', () => {
    expect(previewModes(photo)).toEqual(['original', 'archive', 'text']);
    expect(defaultMode(photo)).toBe('original');
    expect(defaultMode(scan)).toBe('archive');
    expect(previewModes({ ...scan, originalEncrypted: true })).toEqual(['archive', 'text']);
    expect(previewModes(office)).toEqual(['text']);
    expect(previewModes({ ...office, hasArchive: true })).toEqual(['archive', 'text']);
    expect(previewModes({ mimeType: 'text/markdown', hasArchive: false })).toEqual(['original']);
    expect(previewKind({ mimeType: 'text/plain', hasArchive: false, originalFilename: 'main.go' })).toBe('code');
    expect(previewKind({ mimeType: 'text/plain', hasArchive: false, originalFilename: '笔记.txt' })).toBe('text');
    expect(previewKind({ mimeType: 'audio/mpeg', hasArchive: false })).toBe('audio');
    expect(previewKind({ mimeType: 'image/heic', hasArchive: true })).toBe('none');
  });

  it('切换按能看的几种显示；只有一种时不显示', () => {
    render(<PreviewToggle doc={photo} mode="original" onMode={() => {}} />);
    expect(screen.getAllByRole('button').map((b) => b.textContent)).toEqual(['原件', '可搜索版', '识别文字']);
    const { container } = render(<PreviewToggle doc={{ mimeType: 'text/markdown', hasArchive: false }} mode="original" onMode={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('V 依次切换，换了文档回到默认', () => {
    const { result, rerender } = renderHook(({ d }) => usePreviewMode(d), { initialProps: { d: photo as typeof photo } });
    expect(result.current.mode).toBe('original');
    act(() => result.current.cycle());
    expect(result.current.mode).toBe('archive');
    act(() => result.current.cycle());
    act(() => result.current.cycle());
    expect(result.current.mode).toBe('original');
    act(() => result.current.setMode('text'));
    rerender({ d: scan });
    expect(result.current.mode).toBe('archive');
  });

  it('可搜索版和 PDF 原件用 pdf.js；图片原件用原件查看器', async () => {
    render(<DocPreview doc={scan} mode="archive" />);
    expect(await screen.findByTestId('pdf')).toHaveTextContent('/api/v1/documents/2/preview');
    render(<DocPreview doc={scan} mode="original" />);
    expect((await screen.findAllByTestId('pdf'))[1]).toHaveTextContent('/api/v1/documents/2/preview?original=true');
    render(<DocPreview doc={photo} mode="original" />);
    expect(screen.getByRole('img', { name: '阳台的花.jpg' })).toHaveAttribute('src', '/api/v1/documents/1/preview?original=true');
    expect(screen.getByRole('toolbar', { name: '预览工具' })).toBeInTheDocument();
  });

  it('等 Office 转换时说明，显示读出的文字', () => {
    render(<DocPreview doc={{ ...office, conversionPending: true }} mode="original" content="租赁合同" />);
    expect(screen.getByText(/配好 Office 转换后会自动生成可搜索版/)).toBeInTheDocument();
    expect(screen.getByText('租赁合同')).toBeInTheDocument();
  });
});

describe('原件查看器', () => {
  const photo = { id: 1, mimeType: 'image/png', hasArchive: false, originalFilename: 'a.png', title: '照片' };

  it('缩放、旋转、镜像和快捷键', () => {
    render(<DocPreview doc={photo} mode="original" shortcuts />);
    const img = screen.getByRole('img', { name: '照片' });
    Object.defineProperty(img, 'naturalWidth', { value: 400 });
    Object.defineProperty(img, 'naturalHeight', { value: 300 });
    fireEvent.load(img);
    expect(screen.getByText('100%')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '放大' }));
    expect(screen.getByText('150%')).toBeInTheDocument();
    fireEvent.keyDown(window, { key: '0' });
    expect(screen.getByText('100%')).toBeInTheDocument();
    fireEvent.keyDown(window, { key: '-' });
    expect(screen.getByText('75%')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '旋转' }));
    expect(img.style.transform).toContain('rotate(90deg)');
    fireEvent.keyDown(window, { key: 'r' });
    expect(img.style.transform).toContain('rotate(180deg)');
    fireEvent.click(screen.getByRole('button', { name: '镜像' }));
    expect(img.style.transform).toContain('scaleX(-1)');
    expect(screen.getByRole('button', { name: '镜像' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('读不出来时显示空状态和下载原件', () => {
    render(<DocPreview doc={photo} mode="original" />);
    fireEvent.error(screen.getByRole('img', { name: '照片' }));
    expect(screen.getByText('暂不能预览')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '下载原件' })).toBeInTheDocument();
  });
});

describe('Markdown、代码和音视频', () => {
  it('Markdown 排版显示，不执行 HTML', () => {
    const md = '# 家里的网络\n\n- 路由器\n- 光猫\n\n<script>alert(1)</script>\n\n[小米](https://www.mi.com) [坏的](javascript:alert(1))\n\n![图](https://example.com/a.png)';
    const { container } = render(<DocPreview doc={{ id: 5, mimeType: 'text/markdown', hasArchive: false }} mode="original" content={md} />);
    expect(screen.getByRole('heading', { level: 1, name: '家里的网络' })).toBeInTheDocument();
    expect(screen.getAllByRole('listitem')).toHaveLength(2);
    expect(container.querySelector('script')).toBeNull();
    expect(screen.getByText('<script>alert(1)</script>')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '小米' })).toHaveAttribute('href', 'https://www.mi.com');
    expect(screen.queryByRole('link', { name: '坏的' })).toBeNull();
    expect(container.querySelector('img')).toBeNull(); // 外部图片不加载
    expect(screen.getByRole('link', { name: /图片：图/ })).toBeInTheDocument();
  });

  it('Markdown 里查找', () => {
    const onCount = vi.fn();
    render(<DocPreview doc={{ id: 5, mimeType: 'text/markdown', hasArchive: false }} mode="original" content={'# 光猫\n\n光猫在**弱电箱**，别拔错光猫。'}
      find={{ terms: ['光猫'], current: 1, seq: 1, onCount }} />);
    expect(onCount).toHaveBeenLastCalledWith(3);
    expect(document.querySelectorAll('mark')).toHaveLength(3);
    expect(document.querySelector('[data-find-current]')).toHaveTextContent('光猫');
  });

  it('代码带行号', () => {
    render(<DocPreview doc={{ id: 6, mimeType: 'text/plain', hasArchive: false, originalFilename: 'main.go' }} mode="original" content={'package main\n\nfunc main() {}\n'} />);
    const code = screen.getByTestId('code');
    expect(code.querySelectorAll('tr')).toHaveLength(3);
    expect(code).toHaveTextContent('3func main() {}');
  });

  it('音频用浏览器的播放器', () => {
    const { container } = render(<DocPreview doc={{ id: 7, mimeType: 'audio/wav', hasArchive: false, title: '乐乐唱歌' }} mode="original" />);
    const audio = container.querySelector('audio')!;
    expect(audio).toHaveAttribute('src', '/api/v1/documents/7/preview');
    expect(audio).toHaveAttribute('controls');
    fireEvent.error(audio);
    expect(screen.getByText(/浏览器播放不了这种格式/)).toBeInTheDocument();
  });
});

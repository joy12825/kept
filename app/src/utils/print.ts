// 打印（v1.5.2）：在隐藏的框架里打开文件再调用打印。预览接口带 sandbox 的安全头，浏览器不肯在沙盒里打印 PDF，
// 所以先把文件取成 blob 再打开；图片包一个只有图片的页面；文本文档打印识别出的文字。
import { fileUrl, isPdfDocument, withPurpose, type DocumentDetail } from '../api/documents';
import { t } from '../i18n';

function frame(): HTMLIFrameElement {
  const f = document.createElement('iframe');
  f.setAttribute('aria-hidden', 'true');
  f.style.cssText = 'position:fixed;right:0;bottom:0;width:0;height:0;border:0;visibility:hidden';
  document.body.appendChild(f);
  return f;
}

function printIn(f: HTMLIFrameElement, cleanup: () => void) {
  f.onload = () => {
    try {
      f.contentWindow?.focus();
      f.contentWindow?.print();
    } finally {
      // 打印对话框关闭前不能移走框架；一分钟后再清理
      setTimeout(() => { cleanup(); f.remove(); }, 60_000);
    }
  };
}

const escape = (s: string) => s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]!);

export async function printDocument(d: Pick<DocumentDetail, 'id' | 'mimeType' | 'hasArchive' | 'title' | 'content' | 'modifiedAt'>): Promise<void> {
  const pdf = isPdfDocument(d);
  if (pdf || d.mimeType.startsWith('image/')) {
    await printFile(withPurpose(fileUrl(d.id, pdf ? 'preview' : 'original', String(new Date(d.modifiedAt).getTime())), 'print'), pdf ? 'application/pdf' : d.mimeType, d.title);
    return;
  }
  printText(d.title, d.content || '');
}

/** 能直接打印的文件：PDF、网页能显示的图片、文本（v1.9.7 分享页） */
export const canPrintFile = (mimeType: string) =>
  mimeType === 'application/pdf' || mimeType.startsWith('text/') || ['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/bmp'].includes(mimeType);

/** 打印一个文件：PDF 原样打印，图片单独一页，文本按文字排版。 */
export async function printFile(url: string, mimeType: string, title: string): Promise<void> {
  const res = await fetch(url, { credentials: 'include' });
  if (!res.ok) throw new Error(t('文件没能取到，请稍后再试'));
  if (mimeType.startsWith('text/')) { printText(title, await res.text()); return; }
  printBlob(await res.blob(), mimeType, title);
}

/** 打印取到的 PDF 或图片（v1.11.2 加水印的副本）。 */
export function printBlob(data: Blob, mimeType: string, title: string) {
  const blob = URL.createObjectURL(data);
  const f = frame();
  printIn(f, () => URL.revokeObjectURL(blob));
  if (mimeType === 'application/pdf') f.src = blob;
  else f.srcdoc = `<!doctype html><title>${escape(title)}</title><style>@page{margin:10mm}body{margin:0}img{max-width:100%;max-height:100vh;display:block;margin:auto}</style><img src="${blob}">`;
}

function printText(title: string, text: string) {
  const f = frame();
  printIn(f, () => {});
  f.srcdoc = `<!doctype html><meta charset="utf-8"><title>${escape(title)}</title><style>body{font:14px/1.7 "PingFang SC","Noto Sans CJK SC",sans-serif;white-space:pre-wrap;margin:16mm}</style>${escape(text)}`;
}

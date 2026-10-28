// 预览器（v1.9.1）：按文件的样子选显示方式，并列出这份文档能看的几种显示。
// 原件：浏览器能直接显示的原件；可搜索版：识别后生成的 PDF；识别文字：识别出的文字。EPUB 是“封面 / 正文”。
import { t, tc } from '../../i18n';

export type PreviewMode = 'original' | 'archive' | 'text';

export type PreviewKind = 'pdf' | 'image' | 'text' | 'markdown' | 'code' | 'sheet' | 'slides' | 'word' | 'zip' | 'audio' | 'video' | 'epub' | 'none';

export interface PreviewDoc { mimeType: string; hasArchive: boolean; originalFilename?: string; originalEncrypted?: boolean }

/** 按扩展名当作代码或数据显示（带行号、等宽）的文本文件。 */
const CODE_EXT = new Set([
  'c', 'h', 'cc', 'cpp', 'hpp', 'cs', 'go', 'java', 'kt', 'swift', 'rs', 'py', 'rb', 'php', 'pl', 'lua', 'r', 'scala', 'dart',
  'js', 'mjs', 'cjs', 'jsx', 'ts', 'tsx', 'vue', 'css', 'scss', 'less', 'sql', 'sh', 'bash', 'zsh', 'ps1', 'bat',
  'json', 'jsonl', 'csv', 'tsv', 'yaml', 'yml', 'toml', 'ini', 'conf', 'cfg', 'env', 'properties', 'log', 'diff', 'patch',
  'dockerfile', 'makefile', 'gradle', 'proto', 'graphql',
]);

/** 浏览器能直接显示的图片格式；HEIC 之类只能看可搜索版 */
const WEB_IMAGES = new Set(['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/bmp']);

export const extOf = (name = '') => {
  const base = name.split('/').pop()!.toLowerCase();
  const i = base.lastIndexOf('.');
  return i > 0 ? base.slice(i + 1) : base; // Makefile、Dockerfile 没有扩展名
};

const XLSX = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';

/** Word 文档（v1.9.8 阅读视图） */
export const WORD = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';
/** 阅读视图也认旧的 .doc 和 OpenDocument 的 .odt（v1.11.1） */
const WORD_TYPES = new Set([WORD, 'application/msword', 'application/vnd.oasis.opendocument.text']);

/** 演示文稿（v1.9.7）：可搜索版一页一张幻灯片 */
const SLIDES = new Set([
  'application/vnd.openxmlformats-officedocument.presentationml.presentation', 'application/vnd.ms-powerpoint',
  'application/vnd.oasis.opendocument.presentation',
]);

export function previewKind(d: PreviewDoc): PreviewKind {
  const m = d.mimeType;
  if (m === 'application/epub+zip') return 'epub';
  if (m === 'application/pdf') return 'pdf';
  if (m === 'text/markdown') return 'markdown';
  // 表格（v1.9.3）：Excel 和 CSV、TSV
  if (m === XLSX) return 'sheet';
  if (m === 'application/zip') return 'zip';
  if (SLIDES.has(m)) return 'slides';
  if (WORD_TYPES.has(m)) return 'word';
  if (m.startsWith('text/')) {
    const ext = extOf(d.originalFilename);
    if (ext === 'csv' || ext === 'tsv') return 'sheet';
    return CODE_EXT.has(ext) ? 'code' : 'text';
  }
  if (m.startsWith('audio/')) return 'audio';
  if (m.startsWith('video/')) return 'video';
  if (WEB_IMAGES.has(m)) return 'image';
  return 'none'; // Office、邮件、HEIC：原件浏览器显示不了
}

/** 文字本身就是原件：没有可搜索版和识别文字可切换 */
export const isTextKind = (k: PreviewKind) => k === 'text' || k === 'markdown' || k === 'code';

/** 这份文档能看的几种显示，按“原件、可搜索版、识别文字”的顺序。 */
export function previewModes(d: PreviewDoc): PreviewMode[] {
  const k = previewKind(d);
  if (k === 'epub') return ['original', 'text'];
  if (k === 'zip') return ['original'];
  // 演示文稿：幻灯片 / 可搜索版 / 识别文字；还没转成 PDF 时只有文字
  if (k === 'slides') return d.hasArchive ? ['original', 'archive', 'text'] : ['text'];
  // Word：阅读 / 可搜索版 / 识别文字（v1.9.8）
  if (k === 'word') return d.hasArchive ? ['original', 'archive', 'text'] : ['original', 'text'];
  // CSV：表格 / 文本；Excel：表格 / 可搜索版 / 识别文字
  if (k === 'sheet') return d.mimeType.startsWith('text/') ? ['original', 'text'] : ['original', ...(d.hasArchive ? ['archive' as const] : []), 'text'];
  if (isTextKind(k) || k === 'audio' || k === 'video') return ['original'];
  const out: PreviewMode[] = [];
  // 加密的 PDF 原件打不开，看解密后的可搜索版
  if (k !== 'none' && !(k === 'pdf' && d.originalEncrypted)) out.push('original');
  if (d.hasArchive) out.push('archive');
  out.push('text');
  return out;
}

/** 打开时的显示：PDF、Office、邮件有可搜索版时看它（查找高亮靠它），其余看原件。
 * Word 在窄屏（手机）上默认看阅读视图，宽屏有可搜索版时看可搜索版（v1.9.8）。 */
export function defaultMode(d: PreviewDoc, narrow = false): PreviewMode {
  const modes = previewModes(d);
  const k = previewKind(d);
  if (k === 'word') return narrow || !d.hasArchive ? 'original' : 'archive';
  if (modes.includes('archive') && (k === 'pdf' || k === 'none')) return 'archive';
  return modes[0];
}

export const MODE_LABEL: Record<PreviewMode, string> = { original: t('原件'), archive: t('可搜索版'), text: t('识别文字') };

/** 切换上的名称：EPUB 是“封面 / 正文”，表格的原件是“表格”，CSV 的文字是“文本” */
export function modeLabel(d: PreviewDoc, m: PreviewMode): string {
  const k = previewKind(d);
  if (k === 'epub') return m === 'original' ? t('封面') : tc('书的正文', '正文');
  if (k === 'sheet' && m === 'original') return t('表格');
  if (k === 'slides' && m === 'original') return t('幻灯片');
  if (k === 'word' && m === 'original') return tc('视图', '阅读');
  if (k === 'sheet' && m === 'text' && d.mimeType.startsWith('text/')) return t('文本');
  return MODE_LABEL[m];
}

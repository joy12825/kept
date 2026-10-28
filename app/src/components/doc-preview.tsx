// 文档预览（v1.9.1 预览器）：按文件的样子显示——PDF 用 pdf.js，图片用原件查看器，Markdown 排版，代码带行号，
// 音视频直接播放，EPUB 显示封面；看不了时显示空状态。待整理、详情页和共享来的文档共用。
import { useEffect, useState } from 'react';
import { Alert, Box, Link, Stack, ToggleButton, ToggleButtonGroup, Typography, useMediaQuery } from '@mui/material';
import { fileUrl, isPdfDocument, startDownload, withPurpose, type DocumentSummary } from '../api/documents';
import type { FindState } from './find-bar';
import { FindText } from './find-text';
import { PdfPreview, SlideView } from './pdf-lazy';
import { CodeView } from './preview/code-view';
import { ImageViewer } from './preview/image-viewer';
import { defaultMode, modeLabel, previewKind, previewModes, type PreviewDoc, type PreviewMode } from './preview/kinds';
import { MarkdownView } from './preview/markdown-view';
import { MediaPlayer } from './preview/media-player';
import { PreviewUnavailable } from './preview/unavailable';
import { SheetView } from './preview/sheet-view';
import { ZipView } from './preview/zip-view';
import { WordView } from './preview/word-view';
import { t } from '../i18n';

export type { PreviewMode } from './preview/kinds';

export const isTextDocument = (mime: string) => mime.startsWith('text/');
export const isEpub = (mime: string) => mime === 'application/epub+zip';
/** 能在阅读器里打开：EPUB、PDF，或有可搜索版的文档。 */
export const canRead = (d: { mimeType: string; hasArchive: boolean }) => isEpub(d.mimeType) || isPdfDocument(d);

type Doc = PreviewDoc & { id: number };

/** 这份文档当前的显示：换了文档回到它的默认显示；V 依次切换。 */
export function usePreviewMode(doc: Doc | undefined) {
  const narrow = useMediaQuery('(max-width:767px)');
  const modes = doc ? previewModes(doc) : (['original'] as PreviewMode[]);
  const fallback = doc ? defaultMode(doc, narrow) : 'original';
  const [chosen, setChosen] = useState<{ id?: number; mode: PreviewMode } | null>(null);
  useEffect(() => { setChosen(null); }, [doc?.id]);
  const mode = chosen && chosen.id === doc?.id && modes.includes(chosen.mode) ? chosen.mode : fallback;
  const setMode = (m: PreviewMode) => setChosen({ id: doc?.id, mode: m });
  const cycle = () => setMode(modes[(modes.indexOf(mode) + 1) % modes.length]);
  return { mode, setMode, modes, cycle };
}

/** 显示切换：按这份文档能看的几种列出；只有一种时不显示。 */
export function PreviewToggle({ doc, mode, onMode }: { doc: PreviewDoc; mode: PreviewMode; onMode: (m: PreviewMode) => void }) {
  const modes = previewModes(doc);
  if (modes.length < 2) return null;
  return (
    <ToggleButtonGroup size="small" exclusive value={mode} onChange={(_, v) => v && onMode(v)} aria-label={t('预览方式')}
      sx={{ bgcolor: 'background.paper' }}>
      {modes.map((m) => <ToggleButton key={m} value={m}>{modeLabel(doc, m)}</ToggleButton>)}
    </ToggleButtonGroup>
  );
}

type FileKind = 'preview' | 'original' | 'thumbnail';

export function DocPreview({ doc, mode, content, loading, fileUrlOf, find, shortcuts, canImport = false }: {
  doc: Pick<DocumentSummary, 'id' | 'mimeType'> & Partial<Pick<DocumentSummary, 'hasArchive' | 'originalFilename' | 'hasThumbnail' | 'title' | 'modifiedAt'>>
    & { originalEncrypted?: boolean; conversionPending?: boolean };
  mode: PreviewMode; content?: string; loading?: boolean;
  /** 文件地址；默认是本空间的文档接口（共享来的文档另有地址）。preview：有可搜索版时是它，否则是原件 */
  fileUrlOf?: (kind: FileKind) => string;
  /** 查找框（详情页） */
  find?: FindState;
  /** 响应预览器的快捷键（详情页） */
  shortcuts?: boolean;
  /** 能把压缩包里的文件导入为文档（v1.9.3，能修改的人） */
  canImport?: boolean;
}) {
  const d: PreviewDoc = { mimeType: doc.mimeType, hasArchive: !!doc.hasArchive, originalFilename: doc.originalFilename, originalEncrypted: doc.originalEncrypted };
  const kind = previewKind(d);
  const modes = previewModes(d);
  const m = modes.includes(mode) ? mode : defaultMode(d);
  // 预览接口带 ETag、不缓存内容（no-cache），每次打开都会核对版本；带上修改时间：编辑页面、重新处理之后换成新文件
  const version = doc.modifiedAt ? String(new Date(doc.modifiedAt).getTime()) : undefined;
  const url = (k: FileKind | 'inline') => {
    if (fileUrlOf) return fileUrlOf(k === 'inline' ? (doc.hasArchive ? 'original' : 'preview') : k);
    if (k === 'inline') { const u = fileUrl(doc.id, 'preview', version); return u + (u.includes('?') ? '&' : '?') + 'original=true'; }
    return fileUrl(doc.id, k, k === 'preview' ? version : undefined);
  };
  const download = () => startDownload(fileUrlOf ? url('original') : withPurpose(url('original'), 'download'));
  const title = doc.title ?? doc.originalFilename ?? '';
  const pending = doc.conversionPending
    ? <Alert severity="info" sx={{ mx: 3, mt: 2 }}>{t('这份文档还没有转成 PDF，现在显示的是从文件里读出的文字。配好 Office 转换后会自动生成可搜索版。')}</Alert>
    : null;
  const plain = (
    <Box component="pre" sx={{ m: 0, p: 3, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontFamily: 'inherit', fontSize: 14, lineHeight: '24px' }}>
      {loading ? t('正在读取…') : content ? <FindText text={content} find={find} /> : t('没有识别出文字。')}
    </Box>
  );

  if (kind === 'epub') {
    if (m === 'text') return plain;
    return (
      <Stack sx={{ alignItems: 'center', p: 4, gap: 2 }}>
        {doc.hasThumbnail
          ? <Box component="img" src={url('thumbnail')} alt={t('封面')} sx={{ width: 280, maxWidth: '100%', borderRadius: 1.5, boxShadow: 3 }} />
          : <Box sx={{ width: 280, maxWidth: '100%', aspectRatio: '2 / 3', borderRadius: 1.5, boxShadow: 3, bgcolor: 'background.paper', display: 'flex', alignItems: 'center', justifyContent: 'center', p: 3 }}>
            <Typography sx={{ fontSize: 22, fontWeight: 500, textAlign: 'center', overflowWrap: 'anywhere' }}>{doc.title}</Typography>
          </Box>}
        <Typography variant="body2" sx={{ color: 'text.secondary' }}>
          {t('EPUB 电子书')} · <Link component="button" variant="body2" onClick={download}>{t('下载')}</Link>
        </Typography>
      </Stack>
    );
  }
  if (kind === 'markdown') return loading ? plain : <MarkdownView source={content ?? ''} find={find} />;
  if (kind === 'code') return loading ? plain : <CodeView source={content ?? ''} find={find} />;
  if (kind === 'text') return plain;
  if (kind === 'audio' || kind === 'video') return <MediaPlayer url={url('preview')} kind={kind} title={title} onDownload={download} />;
  // 表格和压缩包（v1.9.3）要读本空间的文档；共享来的文档看文字或可搜索版
  if (kind === 'zip') return fileUrlOf || loading ? plain : <ZipView docId={doc.id} canImport={canImport} onDownload={download} />;
  if (kind === 'sheet' && m === 'original') {
    if (!fileUrlOf) return <SheetView docId={doc.id} onDownload={download} />;
    if (doc.hasArchive) { const u = url('preview'); return <PdfPreview key={u} url={u} find={find} />; }
    return plain;
  }
  // Word 阅读视图（v1.9.8）要读本空间的文档；共享来的文档看可搜索版或文字
  if (kind === 'word' && m === 'original') {
    if (!fileUrlOf) return <WordView docId={doc.id} find={find} onDownload={download} />;
    if (doc.hasArchive) { const u = url('preview'); return <PdfPreview key={u} url={u} find={find} />; }
    return plain;
  }
  // 演示文稿（v1.9.7）：可搜索版一张一张地看
  if (kind === 'slides' && m === 'original') { const u = url('preview'); return <SlideView key={u} url={u} shortcuts={shortcuts} />; }
  if (m === 'text') return <>{pending}{plain}</>;
  if (m === 'archive') {
    const u = url('preview');
    return (
      <>
        {doc.mimeType === 'application/ofd' && (
          <Typography variant="caption" sx={{ display: 'block', textAlign: 'center', color: 'text.secondary', mt: -1 }}>{t('按 OFD 原件画出的 PDF，印章画在原来的位置；签章是否有效见右边的“电子签章”')}</Typography>
        )}
        <PdfPreview key={u} url={u} find={find} shortcuts={shortcuts} />
      </>
    );
  }
  // 原件
  if (kind === 'image') return <ImageViewer url={url('inline')} alt={title} shortcuts={shortcuts} onDownload={download} />;
  if (kind === 'pdf') { const u = doc.hasArchive ? url('inline') : url('preview'); return <PdfPreview key={u} url={u} find={find} shortcuts={shortcuts} />; }
  return <PreviewUnavailable reason={t('浏览器显示不了这种文件，可以下载原件查看。')} onDownload={download} />;
}

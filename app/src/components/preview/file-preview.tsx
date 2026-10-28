// 分享出去的页面（单份分享、分享包）用的预览（v1.9.1）：只有一个文件地址，按它的类型选显示方式。
import { useEffect, useState } from 'react';
import { Box } from '@mui/material';
import { PdfPreview } from '../pdf-lazy';
import { CodeView } from './code-view';
import { ImageViewer } from './image-viewer';
import { isTextKind, previewKind } from './kinds';
import { MarkdownView } from './markdown-view';
import { MediaPlayer } from './media-player';
import { PreviewUnavailable } from './unavailable';
import { t } from '../../i18n';

export function FilePreview({ url, mimeType, filename, title, onDownload }: { url: string; mimeType: string; filename: string; title: string; onDownload?: () => void }) {
  const kind = previewKind({ mimeType, hasArchive: false, originalFilename: filename });
  if (kind === 'pdf') return <Box sx={{ height: '75vh' }}><PdfPreview key={url} url={url} /></Box>;
  if (kind === 'image') return <ImageViewer key={url} url={url} alt={title} onDownload={onDownload} height="75vh" />;
  if (kind === 'audio' || kind === 'video') return <MediaPlayer key={url} url={url} kind={kind} title={title} onDownload={onDownload} />;
  if (isTextKind(kind)) return <RemoteText key={url} url={url} kind={kind} />;
  return <PreviewUnavailable reason={onDownload ? t('这种文件不能在网页上预览，请下载后查看。') : t('这种文件不能在网页上预览，分享的人没有允许下载。')} onDownload={onDownload} />;
}

function RemoteText({ url, kind }: { url: string; kind: 'text' | 'markdown' | 'code' | string }) {
  const [text, setText] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    fetch(url).then((r) => r.text()).then((t) => { if (live) setText(t); }).catch(() => { if (live) setText(''); });
    return () => { live = false; };
  }, [url]);
  if (text === null) return <Box sx={{ p: 3, color: 'text.secondary' }}>{t('正在读取…')}</Box>;
  if (kind === 'markdown') return <MarkdownView source={text} />;
  if (kind === 'code') return <CodeView source={text} />;
  return <Box component="pre" sx={{ m: 0, p: 3, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontFamily: 'inherit', fontSize: 14, lineHeight: '24px' }}>{text}</Box>;
}

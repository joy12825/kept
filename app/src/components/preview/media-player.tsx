// 音频和视频（v1.9.1）：浏览器自带的播放器；服务器支持分段读取，可以拖动进度。
import { useState } from 'react';
import { Box, Stack, Typography } from '@mui/material';
import { FileAudio } from 'lucide-react';
import { SW } from '../icons';
import { PreviewUnavailable } from './unavailable';
import { t } from '../../i18n';

export function MediaPlayer({ url, kind, title, onDownload }: { url: string; kind: 'audio' | 'video'; title: string; onDownload?: () => void }) {
  const [error, setError] = useState(false);
  if (error) return <PreviewUnavailable reason={t('浏览器播放不了这种格式，可以下载原件用别的播放器打开。')} onDownload={onDownload} />;
  if (kind === 'video') {
    return (
      <Box sx={{ p: { xs: 2, md: 3 }, display: 'flex', justifyContent: 'center' }}>
        <Box component="video" src={url} controls preload="metadata" aria-label={title} onError={() => setError(true)}
          sx={{ display: 'block', maxWidth: '100%', maxHeight: { md: 'calc(100vh - 56px - 96px)' }, borderRadius: 2, bgcolor: '#000', boxShadow: 1 }} />
      </Box>
    );
  }
  return (
    <Stack spacing={2} sx={{ alignItems: 'center', py: 10, px: 3, maxWidth: 520, mx: 'auto' }}>
      <Box sx={{ color: 'text.secondary' }}><FileAudio size={40} strokeWidth={SW} /></Box>
      <Typography variant="h5" sx={{ textAlign: 'center', overflowWrap: 'anywhere' }}>{title}</Typography>
      <Box component="audio" src={url} controls preload="metadata" aria-label={title} onError={() => setError(true)} sx={{ width: '100%' }} />
    </Stack>
  );
}

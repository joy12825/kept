// 看不了时（v1.9.1）：按规范的空状态，一个 40px 图标、一句标题、一句原因和“下载原件”。
import { Button, Stack, Typography } from '@mui/material';
import { FileX2 } from 'lucide-react';
import { SW } from '../icons';
import { t } from '../../i18n';

export function PreviewUnavailable({ title = t('暂不能预览'), reason, onDownload, icon }: {
  title?: string; reason: string; onDownload?: () => void; icon?: React.ReactNode;
}) {
  return (
    <Stack spacing={1} sx={{ alignItems: 'center', textAlign: 'center', py: 12, px: 3, maxWidth: 440, mx: 'auto' }}>
      <Stack sx={{ color: 'text.secondary', mb: 1 }}>{icon ?? <FileX2 size={40} strokeWidth={SW} />}</Stack>
      <Typography variant="h5">{title}</Typography>
      <Typography sx={{ color: 'text.secondary' }}>{reason}</Typography>
      {onDownload && <Stack sx={{ pt: 1.5 }}><Button variant="contained" onClick={onDownload}>{t('下载原件')}</Button></Stack>}
    </Stack>
  );
}

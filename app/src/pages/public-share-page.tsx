// 分享链接的公开页面（/s/<令牌>，阶段 18）：不用登录。只显示文档的标题、预览和下载，不透露空间、分享人等信息。
import { useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { Box, Button, GlobalStyles, Paper, Stack, Typography } from '@mui/material';
import { Download, Link2Off, Printer } from 'lucide-react';
import { useBranding } from '../api/branding';
import { publicFileUrl, usePublicShare } from '../api/share';
import { BrandMark, SW } from '../components/icons';
import { EmptyState } from '../components/common';
import { FilePreview } from '../components/preview/file-preview';
import { bytes } from './settings/system';
import { dateTime } from './detail/time';
import { canPrintFile, printFile } from '../utils/print';
import { t } from '../i18n';

/** 分享页不让搜索引擎收录。 */
export function useNoIndex() {
  useEffect(() => {
    const m = document.createElement('meta');
    m.name = 'robots';
    m.content = 'noindex, nofollow';
    document.head.appendChild(m);
    return () => { m.remove(); };
  }, []);
}

/** 只能在线查看时，浏览器的打印什么也不输出（v1.9.7）；截图仍然挡不住。 */
export const NoPrint = () => <GlobalStyles styles={{ '@media print': { body: { display: 'none' } } }} />;

/** 页头的说明：只能在线查看、加了水印（v1.9.2） */
export const viewNote = (s: { allowDownload: boolean; watermarked: boolean }) =>
  [s.allowDownload ? '' : t('只能在线查看'), s.watermarked ? t('已加水印') : ''].filter(Boolean).map((x) => ` · ${x}`).join('');

export default function PublicSharePage() {
  useNoIndex();
  const { token = '' } = useParams();
  const brand = useBranding();
  const share = usePublicShare(token);
  const s = share.data;
  const file = publicFileUrl(token);

  return (
    <Box sx={{ minHeight: '100vh', bgcolor: 'background.default', display: 'flex', flexDirection: 'column' }}>
      <Stack direction="row" spacing={1.25} sx={{ alignItems: 'center', height: 56, px: { xs: 2, md: 3 }, borderBottom: 1, borderColor: 'divider', bgcolor: 'background.paper' }}>
        <BrandMark size={24} />
        <Typography variant="h6" sx={{ color: 'text.secondary' }}>{brand.name}</Typography>
      </Stack>
      {share.isPending ? null : share.error || !s ? (
        <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', px: 2 }}>
          <EmptyState icon={<Link2Off size={40} strokeWidth={SW} />} title={t('这个链接已失效')}
            text={share.error?.code === 'share_expired' ? t('链接已经过期或被作废了。如果还需要这份文件，请联系分享给你的人。') : share.error?.message} />
        </Box>
      ) : (
        <Box sx={{ flex: 1, width: '100%', maxWidth: 1000, mx: 'auto', px: { xs: 2, md: 3 }, py: 3 }}>
          {!s.allowDownload && <NoPrint />}
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} sx={{ alignItems: { sm: 'center' }, mb: 2 }}>
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Typography variant="h4" component="h1" sx={{ overflowWrap: 'anywhere' }}>{s.title}</Typography>
              <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                {s.filename} · {bytes(s.size)}{s.pageCount > 1 ? ` · ${t('{n} 页', { n: s.pageCount })}` : ''} · {t('链接 {date} 到期', { date: dateTime(s.expiresAt) })}{viewNote(s)}
              </Typography>
            </Box>
            {s.allowDownload && canPrintFile(s.mimeType) && (
              <Button variant="outlined" startIcon={<Printer size={16} strokeWidth={SW} />} onClick={() => { printFile(file, s.mimeType, s.title).catch((e: Error) => window.alert(e.message)); }}>{t('打印')}</Button>
            )}
            {s.allowDownload && <Button variant="contained" startIcon={<Download size={16} strokeWidth={SW} />} href={publicFileUrl(token, true)}>{t('下载')}</Button>}
          </Stack>
          <Paper variant="outlined" sx={{ overflow: 'hidden', borderRadius: 3, minHeight: 360, bgcolor: 'surface.containerLow' }}>
            <FilePreview url={file} mimeType={s.mimeType} filename={s.filename} title={s.title} onDownload={s.allowDownload ? () => { window.location.href = publicFileUrl(token, true); } : undefined} />
          </Paper>
        </Box>
      )}
    </Box>
  );
}

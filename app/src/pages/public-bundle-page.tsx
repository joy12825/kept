// 分享包的公开页面（/b/<令牌>，v1.4.2）：不用登录。列出包里的文档，可以逐份预览、下载，或全部打包下载；
// 和单份文档的分享页一样，不透露空间、分享人等信息，也不让搜索引擎收录。
// v1.8.3：给医生看的合集带一页检查结果汇总，打开时先看到它。
import { useState } from 'react';
import { useParams } from 'react-router-dom';
import { Box, Button, ButtonBase, Paper, Stack, Typography } from '@mui/material';
import { Download, FileHeart, FileText, Link2Off, Printer } from 'lucide-react';
import { useBranding } from '../api/branding';
import { publicBundleFileUrl, publicBundleZipUrl, usePublicBundle, type PublicBundle } from '../api/share';
import { BrandMark, SW } from '../components/icons';
import { EmptyState } from '../components/common';
import { FilePreview } from '../components/preview/file-preview';
import { bytes } from './settings/system';
import { dateTime } from './detail/time';
import { NoPrint, useNoIndex, viewNote } from './public-share-page';
import { canPrintFile, printFile } from '../utils/print';
import { HealthReportView } from '@ee';
import { t } from '../i18n';

const SUMMARY = -1;

type Doc = PublicBundle['documents'][number];

function Preview({ token, d, download }: { token: string; d: Doc; download: boolean }) {
  const url = publicBundleFileUrl(token, d.id);
  return <FilePreview url={url} mimeType={d.mimeType} filename={d.filename} title={d.title} onDownload={download ? () => { window.location.href = publicBundleFileUrl(token, d.id, true); } : undefined} />;
}

export default function PublicBundlePage() {
  useNoIndex();
  const { token = '' } = useParams();
  const brand = useBranding();
  const bundle = usePublicBundle(token);
  const b = bundle.data;
  const [open, setOpen] = useState<number | null>(null);
  const showSummary = !!b?.summary && (open === null || open === SUMMARY);
  const current = showSummary ? undefined : b?.documents.find((d) => d.id === open) ?? b?.documents[0];
  const total = b?.documents.reduce((n, d) => n + d.size, 0) ?? 0;

  return (
    <Box sx={{ minHeight: '100vh', bgcolor: 'background.default', display: 'flex', flexDirection: 'column' }}>
      <Stack direction="row" spacing={1.25} sx={{ alignItems: 'center', height: 56, px: { xs: 2, md: 3 }, borderBottom: 1, borderColor: 'divider', bgcolor: 'background.paper' }}>
        <BrandMark size={24} />
        <Typography variant="h6" sx={{ color: 'text.secondary' }}>{brand.name}</Typography>
      </Stack>
      {bundle.isPending ? null : bundle.error || !b ? (
        <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', px: 2 }}>
          <EmptyState icon={<Link2Off size={40} strokeWidth={SW} />} title={t('这个链接已失效')}
            text={bundle.error?.code === 'share_expired' ? t('链接已经过期或被作废了。如果还需要这些文件，请联系分享给你的人。') : bundle.error?.message} />
        </Box>
      ) : (
        <Box sx={{ flex: 1, width: '100%', maxWidth: 1200, mx: 'auto', px: { xs: 2, md: 3 }, py: 3 }}>
          {!b.allowDownload && <NoPrint />}
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} sx={{ alignItems: { sm: 'center' }, mb: 2 }}>
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Typography variant="h4" component="h1" sx={{ overflowWrap: 'anywhere' }}>{b.title}</Typography>
              <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('{n} 份文档', { n: b.documents.length })} · {bytes(total)} · {t('链接 {date} 到期', { date: dateTime(b.expiresAt) })}{viewNote(b)}</Typography>
            </Box>
            {b.allowDownload && <Button variant="contained" startIcon={<Download size={16} strokeWidth={SW} />} href={publicBundleZipUrl(token)}>{t('全部下载')}</Button>}
          </Stack>
          <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0,1fr)', md: '300px minmax(0,1fr)' }, gap: 2, alignItems: 'start' }}>
            <Paper variant="outlined" component="ul" aria-label={t('文档')} sx={{ listStyle: 'none', m: 0, p: 0.5, borderRadius: 3 }}>
              {b.summary && (
                <Box component="li">
                  <ButtonBase onClick={() => setOpen(SUMMARY)} aria-current={showSummary ? 'true' : undefined}
                    sx={{ width: '100%', justifyContent: 'flex-start', textAlign: 'left', gap: 1.25, px: 1.25, py: 1, borderRadius: 2, bgcolor: showSummary ? 'surface.secondaryContainer' : 'transparent' }}>
                    <FileHeart size={18} strokeWidth={SW} style={{ flex: 'none' }} />
                    <Box sx={{ minWidth: 0 }}>
                      <Typography noWrap>{t('检查结果汇总')}</Typography>
                      <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('{n} 个指标的变化和各次结论', { n: b.summary.metrics.length })}</Typography>
                    </Box>
                  </ButtonBase>
                </Box>
              )}
              {b.documents.map((d) => {
                const on = current?.id === d.id;
                return (
                  <Box component="li" key={d.id}>
                    <Stack direction="row" sx={{ alignItems: 'center', borderRadius: 2, bgcolor: on ? 'surface.secondaryContainer' : 'transparent' }}>
                      <ButtonBase onClick={() => setOpen(d.id)} aria-current={on ? 'true' : undefined}
                        sx={{ flex: 1, minWidth: 0, justifyContent: 'flex-start', textAlign: 'left', gap: 1.25, px: 1.25, py: 1, borderRadius: 2 }}>
                        <FileText size={18} strokeWidth={SW} style={{ flex: 'none' }} />
                        <Box sx={{ minWidth: 0 }}>
                          <Typography noWrap>{d.title}</Typography>
                          <Typography variant="caption" sx={{ color: 'text.secondary' }}>{d.date} · {bytes(d.size)}{d.pageCount > 1 ? ` · ${t('{n} 页', { n: d.pageCount })}` : ''}</Typography>
                        </Box>
                      </ButtonBase>
                      {b.allowDownload && canPrintFile(d.mimeType) && (
                        <Button size="small" aria-label={t('打印“{title}”', { title: d.title })} sx={{ minWidth: 0 }}
                          onClick={() => { printFile(publicBundleFileUrl(token, d.id), d.mimeType, d.title).catch((e: Error) => window.alert(e.message)); }}>
                          <Printer size={16} strokeWidth={SW} />
                        </Button>
                      )}
                      {b.allowDownload && (
                        <Button size="small" href={publicBundleFileUrl(token, d.id, true)} aria-label={t('下载“{title}”', { title: d.title })} sx={{ minWidth: 0, mr: 0.5 }}>
                          <Download size={16} strokeWidth={SW} />
                        </Button>
                      )}
                    </Stack>
                  </Box>
                );
              })}
            </Paper>
            {showSummary && b.summary && (
              <Paper variant="outlined" sx={{ borderRadius: 3, p: { xs: 2, md: 3 } }} aria-label={t('检查结果汇总')}>
                <HealthReportView report={b.summary} />
              </Paper>
            )}
            {current && (
              <Paper variant="outlined" sx={{ overflow: 'hidden', borderRadius: 3, minHeight: 360, bgcolor: 'surface.containerLow' }} aria-label={t('预览“{title}”', { title: current.title })}>
                <Preview token={token} d={current} download={b.allowDownload} />
              </Paper>
            )}
          </Box>
        </Box>
      )}
    </Box>
  );
}

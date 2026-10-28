// 共享来的文档（只读）：预览、识别文字、基本信息和下载。
import { useNavigate, useParams } from 'react-router-dom';
import { Box, Button, Chip, IconButton, Stack, Typography, useMediaQuery } from '@mui/material';
import { ArrowLeft, Download } from 'lucide-react';
import { formatAmount, startDownload } from '../../api/documents';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import { DocPreview, PreviewToggle, usePreviewMode } from '../../components/doc-preview';
import { SW } from '../../components/icons';
import { drawerSource, type SharedSource } from './sources';
import { LockedPanel } from '../../components/verify-dialog';
import { LABEL_W } from '../../components/doc-fields';
import { t } from '../../i18n';

export default function SharedDocumentPage({ source = drawerSource }: { source?: SharedSource }) {
  const params = useParams();
  const id = Number(params.id);
  const docId = Number(params.docId);
  const nav = useNavigate();
  const drawer = source.useInfo(id);
  const d = source.useDocument(id, docId);
  const preview = usePreviewMode(d.data ? { ...d.data, id: docId } : undefined);
  const narrow = useMediaQuery('(max-width:767px)');
  if (d.isPending) return <LoadingState />;
  if (d.error?.status === 404) return <EmptyState title={t('找不到这份文档')} text={t('它可能已移出共享的{noun}，或共享已被取消。', { noun: source.noun })} action={t('返回')} onAction={() => nav(`${source.route}/${id}`)} />;
  if (d.error || !d.data) return <ErrorState onRetry={() => void d.refetch()} />;
  const doc = d.data;
  const rows: [string, string][] = [
    [t('类型'), doc.typeName ?? '—'], [t('往来方'), doc.correspondentName ?? '—'], [t('日期'), doc.createdDate],
    [t('标签'), doc.tagNames.join(t('、')) || '—'], [t('金额'), doc.amount ? formatAmount(doc.amount) : '—'], [t('文件名'), doc.originalFilename],
  ];
  return (
    <Box>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', height: 56, px: 2, borderBottom: 1, borderColor: 'divider' }}>
        <IconButton aria-label={t('返回')} onClick={() => nav(`${source.route}/${id}`)}><ArrowLeft size={18} strokeWidth={SW} /></IconButton>
        <Typography noWrap sx={{ color: 'text.secondary', fontSize: 14 }}>{drawer.data?.name}</Typography>
        <Typography noWrap sx={{ fontWeight: 500, flex: 1 }}>{doc.title}</Typography>
        <Button variant="outlined" size="small" disabled={!!doc.locked} startIcon={<Download size={16} strokeWidth={SW} />} onClick={() => startDownload(source.fileUrl(id, docId, 'original'))}>{t('下载原件')}</Button>
      </Stack>
      <Box sx={{ display: 'grid', gridTemplateColumns: narrow ? '1fr' : 'minmax(0,65fr) minmax(320px,35fr)', height: narrow ? 'auto' : 'calc(100vh - 57px)' }}>
        <Box sx={{ bgcolor: 'surface.containerLow', overflow: 'auto', minHeight: narrow ? 480 : 0, position: 'relative' }}>
          <Box sx={{ position: 'sticky', top: 0, zIndex: 3, display: 'flex', justifyContent: 'center', pt: 1.5, height: 0, overflow: 'visible' }}>
            {!doc.locked && <PreviewToggle doc={doc} mode={preview.mode} onMode={preview.setMode} />}
          </Box>
          {doc.locked
            ? <LockedPanel text={t('对方把这份标成了敏感文档。确认你自己的密码后可以查看。')} />
            : <Box sx={{ pt: preview.modes.length > 1 ? 6 : 0 }}><DocPreview doc={doc} mode={preview.mode} content={doc.content} fileUrlOf={(k) => source.fileUrl(id, docId, k)} /></Box>}
        </Box>
        <Box component="aside" aria-label={t('文档信息')} sx={{ p: 2.5, overflow: 'auto' }}>
          <Typography sx={{ fontSize: 18, lineHeight: '26px', fontWeight: 500, overflowWrap: 'anywhere' }}>{doc.title}</Typography>
          <Chip size="small" label={t('只读 · 来自“{space}”', { space: drawer.data?.sourceWorkspaceName ?? '' })} sx={{ mt: 1 }} />
          <Box component="dl" sx={{ m: 0, mt: 2, display: 'grid', gridTemplateColumns: `${LABEL_W}px minmax(0,1fr)`, rowGap: 1.5, columnGap: 1 }}>
            {rows.map(([k, v]) => (
              <Box key={k} sx={{ display: 'contents' }}>
                <Box component="dt" sx={{ color: 'text.secondary', fontSize: 14 }}>{k}</Box>
                <Box component="dd" sx={{ m: 0, fontSize: 14, overflowWrap: 'anywhere' }}>{v}</Box>
              </Box>
            ))}
          </Box>
        </Box>
      </Box>
    </Box>
  );
}

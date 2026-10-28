// 近似重复（阶段 20）：疑似重复的文档对左右并排，保留其中一份（另一份的标签合并过来，另一份移到回收站），或者“不是重复”。
import { Link as RouterLink } from 'react-router-dom';
import { Box, Button, Card, Chip, Link, Skeleton, Stack, Typography } from '@mui/material';
import { CopyCheck, RefreshCw } from 'lucide-react';
import { useDismissDuplicate, useDuplicates, useKeepDuplicate, useScanDuplicates, type DuplicatePair } from '../api/duplicates';
import type { DocumentSummary } from '../api/documents';
import { useRole } from '../api/workspaces';
import { useApp } from '../store/app-store';
import { EmptyState, ErrorState } from '../components/common';
import { DocThumb } from '../components/doc-list';
import { SW } from '../components/icons';
import { useTaxonomyIndex } from '../components/pickers';
import { relativeTime } from './detail/time';
import { t } from '../i18n';

function Side({ d, snippet }: { d: DocumentSummary; snippet: string }) {
  const ix = useTaxonomyIndex();
  const meta = [d.createdDate, d.correspondentId ? ix.correspondents.get(d.correspondentId)?.name : '', d.documentTypeId ? ix.types.get(d.documentTypeId)?.name : '',
    d.pageCount > 1 ? t('{n} 页', { n: d.pageCount }) : ''].filter(Boolean).join(' · ');
  return (
    <Stack direction="row" spacing={1.5} sx={{ flex: 1, minWidth: 0 }}>
      <DocThumb d={d} w={64} h={84} />
      <Box sx={{ minWidth: 0 }}>
        <Link component={RouterLink} to={`/docs/${d.id}`} underline="hover" sx={{ fontWeight: 500, overflowWrap: 'anywhere' }}>{d.title}</Link>
        <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>{meta}</Typography>
        <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5, display: '-webkit-box', WebkitLineClamp: 3, WebkitBoxOrient: 'vertical', overflow: 'hidden', overflowWrap: 'anywhere' }}>{snippet || t('（没有识别出文字）')}</Typography>
      </Box>
    </Stack>
  );
}

function PairCard({ p, canWrite }: { p: DuplicatePair; canWrite: boolean }) {
  const app = useApp();
  const keep = useKeepDuplicate();
  const dismiss = useDismissDuplicate();
  const busy = keep.isPending || dismiss.isPending;
  const keepOne = (k: DocumentSummary, other: DocumentSummary) => app.confirm({
    title: t('保留“{title}”？', { title: k.title }),
    body: t('“{title}”的标签会加到保留的这份上，然后移到回收站，30 天内可以恢复。', { title: other.title }),
    verb: t('保留这份'),
    run: () => keep.mutate({ id: p.id, documentId: k.id }, { onSuccess: () => { app.confirm(null); app.toast(t('已保留“{title}”，另一份移到了回收站', { title: k.title })); }, onError: (e) => { app.confirm(null); app.toast(e.message); } }),
  });
  return (
    <Card sx={{ p: 2 }}>
      <Stack direction="row" sx={{ alignItems: 'center', mb: 1.5 }}>
        <Chip size="small" label={t('相似度 {pct}%', { pct: p.similarity })} color={p.similarity >= 98 ? 'warning' : 'default'} />
        <Box sx={{ flex: 1 }} />
        {canWrite && <Button size="small" disabled={busy} onClick={() => dismiss.mutate(p.id, { onSuccess: () => app.toast(t('好的，以后不再提示这一对')), onError: (e) => app.toast(e.message) })}>{t('不是重复')}</Button>}
      </Stack>
      <Stack direction={{ xs: 'column', md: 'row' }} spacing={2}>
        {([[p.a, p.aSnippet, p.b], [p.b, p.bSnippet, p.a]] as const).map(([d, snip, other]) => (
          <Stack key={d.id} spacing={1} sx={{ flex: 1, minWidth: 0, p: 1.5, borderRadius: 2, bgcolor: 'surface.containerLow' }}>
            <Side d={d} snippet={snip} />
            {canWrite && <Box><Button size="small" variant="outlined" disabled={busy} onClick={() => keepOne(d, other)}>{t('保留这份')}</Button></Box>}
          </Stack>
        ))}
      </Stack>
    </Card>
  );
}

export default function DuplicatesPage() {
  const role = useRole();
  const list = useDuplicates();
  const scan = useScanDuplicates();
  const app = useApp();
  const data = list.data;
  const scanning = scan.isPending || !!data?.scanning;
  return (
    <Box sx={{ px: 3, py: 2, maxWidth: 1100 }}>
      <Stack direction="row" sx={{ alignItems: 'center' }}>
        <Typography variant="h4" sx={{ flex: 1 }}>{t('近似重复')}</Typography>
        {role.canWrite && (
          <Button variant="outlined" size="small" startIcon={<RefreshCw size={16} strokeWidth={SW} />} disabled={scanning}
            onClick={() => scan.mutate(undefined, { onError: (e) => app.toast(e.message) })}>{scanning ? t('正在检查…') : t('立即检查')}</Button>
        )}
      </Stack>
      <Typography sx={{ color: 'text.secondary', mt: 0.5, mb: 2.5 }}>
        {data?.lastScanAt
          ? t('内容几乎一样、文件却不同的文档，比如同一张发票的扫描件和电子版。每周自动检查一次，最近一次在 {time}。', { time: relativeTime(data.lastScanAt) })
          : t('内容几乎一样、文件却不同的文档，比如同一张发票的扫描件和电子版。每周自动检查一次。')}
      </Typography>
      {list.isPending ? <Stack spacing={2}>{[0, 1].map((i) => <Skeleton key={i} variant="rounded" height={160} />)}</Stack>
        : list.error ? <ErrorState text={list.error.message} onRetry={() => void list.refetch()} />
          : !data?.pairs.length ? (
            <EmptyState icon={<CopyCheck size={40} strokeWidth={SW} />} title={data?.lastScanAt ? t('没有发现近似重复') : t('还没有检查过')}
              text={data?.lastScanAt ? t('有新文档后，下次检查时会再找一遍。') : t('点“立即检查”找一找内容几乎一样的文档。')} />
          ) : (
            <Stack spacing={2}>{data.pairs.map((p) => <PairCard key={p.id} p={p} canWrite={role.canWrite} />)}</Stack>
          )}
    </Box>
  );
}

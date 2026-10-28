// 共享来的抽屉或共享集（只读）：来自哪个空间、文档列表（可以搜索），点开看详情、预览和下载。
import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, Box, InputBase, Paper, Skeleton, Stack, Typography } from '@mui/material';
import { Search } from 'lucide-react';
import { formatAmount } from '../../api/documents';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import { SW, Thumb } from '../../components/icons';
import { drawerSource, type SharedSource } from './sources';
import { DocThumb } from '../../components/doc-list';
import { t } from '../../i18n';

export default function SharedDrawerPage({ source = drawerSource }: { source?: SharedSource }) {
  const id = Number(useParams().id);
  const nav = useNavigate();
  const d = source.useInfo(id);
  const [input, setInput] = useState('');
  const [q, setQ] = useState('');
  useEffect(() => { const timer = window.setTimeout(() => setQ(input.trim()), 250); return () => window.clearTimeout(timer); }, [input]);
  const docs = source.useDocuments(id, q);
  const items = useMemo(() => docs.data?.pages.flatMap((p) => p.items) ?? [], [docs.data]);
  const sentinel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = sentinel.current;
    if (!el || typeof IntersectionObserver === 'undefined') return;
    const io = new IntersectionObserver((es) => { if (es.some((e) => e.isIntersecting) && docs.hasNextPage && !docs.isFetchingNextPage) void docs.fetchNextPage(); }, { rootMargin: '400px' });
    io.observe(el);
    return () => io.disconnect();
  }, [docs.hasNextPage, docs.isFetchingNextPage, items.length]); // eslint-disable-line react-hooks/exhaustive-deps

  if (d.isPending) return <LoadingState />;
  if (d.error?.status === 404) return <EmptyState title={t('找不到这个{noun}', { noun: source.noun })} text={t('共享可能已被取消。')} action={t('回到首页')} onAction={() => nav('/')} />;
  if (d.error || !d.data) return <ErrorState onRetry={() => void d.refetch()} />;
  const D = d.data;
  return (
    <Box sx={{ px: 3, pt: 0.5, pb: 10 }}>
      <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center' }}>
        {source.icon(D, 40)}
        <Typography variant="h4" component="h1">{D.name}</Typography>
      </Stack>
      <Typography variant="body2" sx={{ color: 'text.secondary', mt: 1 }}>{t('{n} 份', { n: D.documentCount })} · {t('来自空间“{space}”', { space: D.sourceWorkspaceName })}</Typography>
      <Alert severity="info" sx={{ mt: 2 }}>{t('这是“{space}”只读共享过来的{noun}：可以查看、预览和下载，不能修改。', { space: D.sourceWorkspaceName, noun: source.noun })}</Alert>
      <Paper elevation={0} sx={{ display: 'flex', alignItems: 'center', gap: 1.5, height: 48, px: 2, mt: 2, borderRadius: 7, bgcolor: 'surface.containerHigh', color: 'text.secondary' }}>
        <Search size={18} strokeWidth={SW} />
        <InputBase value={input} onChange={(e) => setInput(e.target.value)} placeholder={t('在{name}中搜索…', { name: D.name })} inputProps={{ 'aria-label': t('搜索文档') }} sx={{ flex: 1, fontSize: 15 }} />
      </Paper>
      {docs.isPending ? <Stack sx={{ mt: 2 }}>{[0, 1, 2].map((i) => <Skeleton key={i} height={56} />)}</Stack>
        : docs.error ? <ErrorState text={docs.error.message} onRetry={() => void docs.refetch()} />
          : items.length === 0 ? <EmptyState icon={<Search size={40} strokeWidth={SW} />} title={q ? t('没有符合条件的文档') : t('这个{noun}还没有文档', { noun: source.noun })} />
            : (
              <Box sx={{ mt: 1.5 }}>
                <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>{t('共 {n} 份', { n: docs.data?.pages[0]?.total ?? items.length })}</Typography>
                {items.map((x) => (
                  <Box key={x.id} role="link" tabIndex={0} onClick={() => nav(`${source.route}/${id}/docs/${x.id}`)} onKeyDown={(e) => e.key === 'Enter' && nav(`${source.route}/${id}/docs/${x.id}`)}
                    sx={{ display: 'flex', alignItems: 'center', gap: 1.5, minHeight: 56, px: 1, borderRadius: 2, cursor: 'pointer', '&:hover': { bgcolor: 'action.hover' } }}>
                    {x.sensitive ? <DocThumb d={{ ...x, sensitive: true }} /> : x.hasThumbnail
                      ? <Box component="img" src={source.fileUrl(id, x.id, 'thumbnail')} alt="" loading="lazy" sx={{ width: 32, height: 42, objectFit: 'cover', objectPosition: 'top', border: 1, borderColor: 'divider', borderRadius: '4px' }} />
                      : <Thumb w={32} h={42} />}
                    <Typography noWrap sx={{ flex: 1 }}>{x.title}</Typography>
                    <Typography noWrap sx={{ flex: '0 1 150px', fontSize: 13, color: 'text.secondary', display: { xs: 'none', md: 'block' } }}>{x.correspondentName ?? ''}</Typography>
                    <Typography noWrap sx={{ flex: '0 1 90px', fontSize: 13, display: { xs: 'none', md: 'block' } }}>{x.typeName ?? ''}</Typography>
                    <Typography noWrap sx={{ flex: '0 1 100px', fontSize: 13, display: { xs: 'none', md: 'block' }, fontVariantNumeric: 'tabular-nums' }}>{x.amount ? formatAmount(x.amount) : ''}</Typography>
                    <Typography sx={{ flex: '0 0 96px', fontSize: 13, color: 'text.secondary', fontVariantNumeric: 'tabular-nums' }}>{x.createdDate}</Typography>
                  </Box>
                ))}
                <Box ref={sentinel} sx={{ height: 1 }} />
              </Box>
            )}
    </Box>
  );
}

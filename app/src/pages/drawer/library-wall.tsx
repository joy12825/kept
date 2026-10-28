// 书房的封面墙（阶段 23）：按添加时间从新到旧，滚到底时再取下一页；没有缩略图的书显示书名。
import { useEffect, useRef, useState } from 'react';
import { Link as RouterLink, useNavigate } from 'react-router-dom';
import { docHref, docLinkClick } from '../../components/doc-link';
import { Box, ButtonBase, IconButton, LinearProgress, ToggleButton, ToggleButtonGroup, Tooltip, Typography } from '@mui/material';
import { BookOpen } from 'lucide-react';
import { coverUrl } from '../../api/books';
import { fileUrl, useDocumentList } from '../../api/documents';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import { canRead } from '../../components/doc-preview';
import { SW } from '../../components/icons';
import { useApp } from '../../store/app-store';
import { t } from '../../i18n';

type Shelf = 'all' | 'want' | 'reading' | 'finished';
const SHELVES: [Shelf, string][] = [['all', t('全部')], ['reading', t('在读')], ['want', t('想读')], ['finished', t('读完')]];
const SHELF_EMPTY: Record<Shelf, string> = { all: t('没有全部的书'), reading: t('没有在读的书'), want: t('没有想读的书'), finished: t('没有读完的书') };

export function LibraryWall({ drawerId, name }: { drawerId: number; name: string }) {
  const nav = useNavigate();
  const app = useApp();
  const [shelf, setShelf] = useState<Shelf>('all');
  const inDrawer = { field: 'drawer', op: 'in', value: [drawerId] };
  // 阅读状态是自己的，按自己的状态筛选
  const filter = JSON.stringify(shelf === 'all' ? inDrawer : { all: [inDrawer, { field: 'reading', op: 'in', value: [shelf] }] });
  const q = useDocumentList({ filter, sort: 'added', dir: 'desc' });
  const more = useRef<HTMLDivElement>(null);
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = q;
  useEffect(() => {
    const el = more.current;
    if (!el || typeof IntersectionObserver === 'undefined') return;
    const io = new IntersectionObserver((es) => { if (es[0].isIntersecting && hasNextPage && !isFetchingNextPage) void fetchNextPage(); });
    io.observe(el);
    return () => io.disconnect();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);
  const bar = (
    <ToggleButtonGroup size="small" exclusive value={shelf} onChange={(_, v) => v && setShelf(v)} aria-label={t('按阅读状态筛选')} sx={{ mb: 2.5 }}>
      {SHELVES.map(([k, l]) => <ToggleButton key={k} value={k} sx={{ px: 2 }}>{l}</ToggleButton>)}
    </ToggleButtonGroup>
  );
  if (q.isPending) return <>{bar}<LoadingState /></>;
  if (q.isError) return <ErrorState onRetry={() => void q.refetch()} />;
  const items = q.data.pages.flatMap((p) => p.items);
  if (items.length === 0) {
    return shelf === 'all'
      ? <EmptyState title={t('书房里还没有书')} text={t('在文档详情页的“更多”中选择“设为书籍”，书会自动放进书房。')} />
      : <>{bar}<EmptyState title={SHELF_EMPTY[shelf]} text={t('在书的详情页“我的阅读”中标记想读、在读或读完。')} /></>;
  }
  const open = (id: number) => {
    app.set({ docNav: { from: `/drawers/${drawerId}`, label: name, query: { filter, sort: 'added', dir: 'desc' }, ids: items.map((d) => d.id) } });
    nav(`/docs/${id}`);
  };
  return (
    <Box>
      {bar}
      <Box role="list" aria-label={t('封面')} sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(128px, 1fr))', gap: 2.5, pb: 4 }}>
        {items.map((d) => (
          <Box key={d.id} role="listitem" aria-label={d.title} sx={{ position: 'relative', '&:hover .read, &:focus-within .read': { opacity: 1 } }}>
            <ButtonBase component={RouterLink} to={docHref(d.id)} onClick={docLinkClick(() => open(d.id))}
              sx={{ width: '100%', display: 'flex', flexDirection: 'column', alignItems: 'stretch', textAlign: 'left', borderRadius: 2, '&:hover .cover': { boxShadow: 3 }, '&:focus-visible .cover': { outline: '2px solid', outlineColor: 'primary.main' } }}>
              <Box className="cover" sx={{ position: 'relative', aspectRatio: '2 / 3', borderRadius: 1.5, overflow: 'hidden', bgcolor: 'surface.containerHigh', boxShadow: 1, transition: 'box-shadow 150ms', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                {d.hasCover || d.hasThumbnail
                  ? <Box component="img" src={d.hasCover ? coverUrl(d.id) : fileUrl(d.id, 'thumbnail')} alt="" loading="lazy" sx={{ width: '100%', height: '100%', objectFit: 'cover' }} />
                  : <Typography sx={{ p: 1.5, fontSize: 15, fontWeight: 500, textAlign: 'center', overflowWrap: 'anywhere' }}>{d.title}</Typography>}
                {d.reading?.status === 'finished' && (
                  <Box aria-label={t('读完')} sx={{ position: 'absolute', left: 6, top: 6, px: 0.75, borderRadius: 1, bgcolor: 'success.main', color: 'success.contrastText', fontSize: 11, lineHeight: '18px' }}>{t('读完')}</Box>
                )}
                {d.reading?.status === 'reading' && (
                  <LinearProgress variant="determinate" value={Math.round(d.reading.percent * 100)} aria-label={t('读到 {pct}%', { pct: Math.round(d.reading.percent * 100) })}
                    sx={{ position: 'absolute', left: 0, right: 0, bottom: 0, height: 4 }} />
                )}
              </Box>
              <Typography variant="body2" sx={{ mt: 1, display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>{d.title}</Typography>
            </ButtonBase>
            {canRead(d) && (
              <Tooltip title={t('阅读')}>
                <IconButton className="read" size="small" aria-label={t('阅读“{title}”', { title: d.title })} onClick={() => nav(`/read/${d.id}`, { state: { from: `/drawers/${drawerId}` } })}
                  sx={{ position: 'absolute', top: 6, right: 6, opacity: 0, transition: 'opacity 150ms', bgcolor: 'background.paper', boxShadow: 1, '&:hover': { bgcolor: 'background.paper' }, '@media (hover: none)': { opacity: 1 } }}>
                  <BookOpen size={16} strokeWidth={SW} />
                </IconButton>
              </Tooltip>
            )}
          </Box>
        ))}
      </Box>
      <Box ref={more} sx={{ height: 1 }} />
    </Box>
  );
}

// 书单列表（阶段 28）：每个书单一张卡片，前几本的封面叠在一起。
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Box, Button, Card, CardActionArea, Stack, Typography } from '@mui/material';
import { Plus } from 'lucide-react';
import { useBookLists } from '../../api/booklists';
import { useRole } from '../../api/workspaces';
import { BookCover } from '../../components/book-cover';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import { SW } from '../../components/icons';
import { BookListDialog } from './book-list-dialog';
import { t } from '../../i18n';

export default function BookListsPage() {
  const nav = useNavigate();
  const role = useRole();
  const lists = useBookLists();
  const [open, setOpen] = useState(false);
  return (
    <Box sx={{ px: 3, pt: 0.5, pb: 4 }}>
      <Stack direction="row" sx={{ alignItems: 'center' }}>
        <Typography variant="h4" component="h1" sx={{ flex: 1 }}>{t('书单')}</Typography>
        {role.canWrite && <Button variant="outlined" startIcon={<Plus size={16} strokeWidth={SW} />} onClick={() => setOpen(true)}>{t('新建书单')}</Button>}
      </Stack>
      <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5, mb: 3 }}>{t('把书房里的书按主题排在一起，空间里的成员都能看到。')}</Typography>
      {lists.isPending ? <LoadingState />
        : lists.isError ? <ErrorState onRetry={() => void lists.refetch()} />
          : lists.data.length === 0
            ? <EmptyState title={t('还没有书单')} text={t('新建一个书单，从书房里挑书放进去。')} action={role.canWrite ? t('新建书单') : undefined} onAction={() => setOpen(true)} />
            : (
              <Box role="list" aria-label={t('书单')} sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))', gap: 2 }}>
                {lists.data.map((l) => (
                  <Card key={l.id} role="listitem" aria-label={l.name} variant="outlined">
                    <CardActionArea onClick={() => nav(`/book-lists/${l.id}`)} sx={{ p: 2, height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'stretch', justifyContent: 'flex-start' }}>
                      <Stack direction="row" spacing={1} sx={{ height: 75, mb: 1.5, overflow: "hidden" }}>
                        {l.preview.map((b) => <BookCover key={b.documentId} id={b.documentId} title={b.title} hasCover={b.hasCover} hasThumbnail={b.hasThumbnail} width={50} />)}
                      </Stack>
                      <Typography variant="h6">{l.name}</Typography>
                      <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('{n} 本', { n: l.count })}{l.description ? ` · ${l.description}` : ''}</Typography>
                    </CardActionArea>
                  </Card>
                ))}
              </Box>
            )}
      <BookListDialog open={open} onClose={() => setOpen(false)} onSaved={(l) => nav(`/book-lists/${l.id}`)} />
    </Box>
  );
}

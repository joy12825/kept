// 回收站（UI 设计规范 5.11）：删除时间、还剩几天自动清除；成员可以恢复，空间管理员可以彻底删除和清空。
import { useNavigate } from 'react-router-dom';
import { Box, Button, Stack, Table, TableBody, TableCell, TableHead, TableRow, Typography } from '@mui/material';
import { Trash2, Undo2 } from 'lucide-react';
import { usePurgeDocument, useEmptyTrash, useRestoreDocument, useTrash } from '../../api/documents';
import { useRole } from '../../api/workspaces';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import { SW } from '../../components/icons';
import { useTaxonomyIndex } from '../../components/pickers';
import { useApp } from '../../store/app-store';
import { dateTime } from '../detail/time';
import { t } from '../../i18n';

/** 手机上隐藏往来方和删除时间两列 */
const WIDE = { display: { xs: 'none', sm: 'table-cell' } };

export default function TrashPage() {
  const app = useApp();
  const nav = useNavigate();
  const role = useRole();
  const ix = useTaxonomyIndex();
  const trash = useTrash();
  const restore = useRestoreDocument();
  const purge = usePurgeDocument();
  const empty = useEmptyTrash();
  const rows = trash.data ?? [];

  if (trash.isPending) return <LoadingState />;
  if (trash.isError) return <ErrorState onRetry={() => void trash.refetch()} />;

  const emptyAll = () => app.confirm({
    title: t('清空回收站？'), body: t('回收站中的 {n} 份文档会被彻底删除，无法恢复。请输入“{word}”确认。', { n: rows.length, word: t('清空') }), verb: t('清空'), needText: t('清空'),
    run: () => { app.confirm(null); empty.mutate(undefined, { onSuccess: () => app.toast(t('已清空回收站')), onError: (e) => app.toast(e.message) }); },
  });
  return (
    <Box sx={{ px: 3, pt: 0.5, pb: 10 }}>
      <Stack direction="row" sx={{ alignItems: 'center', mb: 1.5, gap: 2 }}>
        <Typography sx={{ color: 'text.secondary', flex: 1 }}>
          {!role.isOwner && rows.length > 0 ? t('删除的文档会保留 30 天，之后自动清除。只有空间管理员可以彻底删除。') : t('删除的文档会保留 30 天，之后自动清除。')}
        </Typography>
        <Button onClick={() => nav('/cleanup')}>{t('可以清理的文档')}</Button>
        {role.isOwner && rows.length > 0 && <Button variant="outlined" onClick={emptyAll} disabled={empty.isPending}>{t('清空回收站')}</Button>}
      </Stack>
      {rows.length === 0 ? <EmptyState icon={<Trash2 size={40} strokeWidth={SW} />} title={t('回收站是空的')} text={t('删除的文档会在这里保留 30 天。')} /> : (
        <Table>
          <TableHead><TableRow><TableCell>{t('标题')}</TableCell><TableCell sx={WIDE}>{t('往来方')}</TableCell><TableCell sx={WIDE}>{t('删除时间')}</TableCell><TableCell>{t('自动清除')}</TableCell><TableCell align="right" /></TableRow></TableHead>
          <TableBody>
            {rows.map((d) => (
              <TableRow key={d.id} hover>
                <TableCell>{d.title}</TableCell>
                <TableCell sx={{ ...WIDE, color: 'text.secondary' }}>{d.correspondentId ? ix.correspondents.get(d.correspondentId)?.name : ''}</TableCell>
                <TableCell sx={{ ...WIDE, color: 'text.secondary', fontVariantNumeric: 'tabular-nums' }}>{dateTime(d.deletedAt)}</TableCell>
                <TableCell sx={{ color: d.daysLeft <= 3 ? 'warning.main' : 'text.secondary' }}>{d.daysLeft > 0 ? t('还剩 {n} 天自动清除', { n: d.daysLeft }) : t('即将清除')}</TableCell>
                <TableCell align="right" sx={{ whiteSpace: 'nowrap', '& > *': { my: { xs: 0.5, sm: 0 } }, display: { xs: 'flex', sm: 'table-cell' }, flexDirection: 'column', alignItems: 'flex-end' }}>
                  {role.canWrite && (
                    <Button size="small" variant="tonal" startIcon={<Undo2 size={16} strokeWidth={SW} />} disabled={restore.isPending}
                      onClick={() => restore.mutate(d.id, { onSuccess: () => app.toast(t('已恢复：{title}', { title: d.title })), onError: (e) => app.toast(e.message) })}>{t('恢复')}</Button>
                  )}
                  {role.isOwner && (
                    <Button size="small" sx={{ ml: 1, color: 'text.secondary' }} onClick={() => app.confirm({
                      title: t('彻底删除这份文档？'), body: t('“{title}”会被彻底删除，无法恢复。', { title: d.title }), verb: t('彻底删除'),
                      run: () => { app.confirm(null); purge.mutate(d.id, { onSuccess: () => app.toast(t('已彻底删除')), onError: (e) => app.toast(e.message) }); },
                    })}>{t('彻底删除')}</Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Box>
  );
}

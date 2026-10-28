// 设置 → 分享链接（v1.4.2）：这个空间所有还有效的分享（单份文档的链接和分享包），集中查看打开次数、复制和作废。
import { useNavigate } from 'react-router-dom';
import { Alert, Box, Card, Chip, IconButton, Skeleton, Stack, Tooltip, Typography } from '@mui/material';
import { Copy, Link2Off } from 'lucide-react';
import { activeShareUrl, useActiveShares, useRevokeShare, type ActiveShare } from '../../api/share';
import { SW } from '../../components/icons';
import { useApp } from '../../store/app-store';
import { dateTime, relativeTime } from '../detail/time';
import { protectNote } from '../../components/share-protect';
import { t, tc } from '../../i18n';

async function copy(text: string): Promise<boolean> {
  try { await navigator.clipboard.writeText(text); return true; } catch { return false; }
}

export function ShareLinksPanel() {
  const app = useApp();
  const nav = useNavigate();
  const list = useActiveShares();
  const revoke = useRevokeShare();
  const end = (s: ActiveShare) => app.confirm({
    title: t('作废这个分享？'), body: t('“{title}”的链接会立即失效，拿到链接的人再也打不开。', { title: s.title }), verb: t('作废'),
    run: () => { app.confirm(null); revoke.mutate(s, { onSuccess: () => app.toast(t('已作废，链接立即失效')), onError: (e) => app.toast(e.message) }); },
  });
  return (
    <>
      <Typography variant="h4" sx={{ mb: 1 }}>{tc('复数', '分享链接')}</Typography>
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 3 }}>
        {t('这个空间里还有效的分享：拿到链接的人不用登录就能查看和下载。单份文档在详情页“更多 → 分享链接”里生成，多份文档在文件列表选中后“生成分享链接”。不再需要的请及时作废。')}
      </Typography>
      {list.isPending ? <Skeleton variant="rounded" height={120} />
        : list.error ? <Alert severity="error">{list.error.message}</Alert>
          : !list.data.length ? <Card sx={{ p: 3 }}><Typography sx={{ color: 'text.secondary' }}>{t('现在没有有效的分享。')}</Typography></Card> : (
            <Card>
              {list.data.map((s, i) => (
                <Stack key={`${s.kind}-${s.id}`} direction="row" spacing={1} sx={{ alignItems: 'center', px: 2, py: 1.5, borderTop: i ? 1 : 0, borderColor: 'divider' }}>
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                      <Chip size="small" label={s.kind === 'bundle' ? t('分享包 · {n} 份', { n: s.documents }) : t('单份文档')} />
                      {s.kind === 'document' && s.documentId
                        ? <Typography noWrap component="button" onClick={() => nav(`/docs/${s.documentId}`)}
                          sx={{ border: 0, p: 0, bgcolor: 'transparent', cursor: 'pointer', color: 'text.primary', font: 'inherit', textAlign: 'left', '&:hover': { textDecoration: 'underline' } }}>{s.title}</Typography>
                        : <Typography noWrap>{s.title}</Typography>}
                    </Stack>
                    <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', mt: 0.25 }}>
                      {s.file === 'archive' ? t('可搜索版') : t('原件')} · {t('{name} 生成', { name: s.creatorName || '—' })} · <Tooltip title={dateTime(s.expiresAt)}><span>{t('{date} 到期', { date: dateTime(s.expiresAt).slice(0, 10) })}</span></Tooltip>
                      {protectNote(s) ? ` · ${protectNote(s)}` : ''}
                      {' · '}{s.accessCount ? t('打开 {n} 次，最近 {time}', { n: s.accessCount, time: relativeTime(s.lastAccessAt!) }) : t('还没有人打开')}
                    </Typography>
                  </Box>
                  <Tooltip title={t('复制链接')}><IconButton aria-label={t('复制“{title}”的链接', { title: s.title })} onClick={async () => app.toast(await copy(activeShareUrl(s)) ? t('已复制链接') : t('复制失败'))}><Copy size={16} strokeWidth={SW} /></IconButton></Tooltip>
                  <Tooltip title={t('作废')}><IconButton aria-label={t('作废“{title}”', { title: s.title })} disabled={revoke.isPending} onClick={() => end(s)}><Link2Off size={16} strokeWidth={SW} /></IconButton></Tooltip>
                </Stack>
              ))}
            </Card>
          )}
    </>
  );
}

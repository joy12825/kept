// 详情页“分享链接”：选有效期和文件，生成后复制；列出这份文档有效的链接，可以作废。
import { canWatermark } from '../utils/watermark';
import { useState } from 'react';
import { Alert, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, MenuItem, Skeleton, Stack, TextField, Tooltip, Typography } from '@mui/material';
import { Copy, Link2Off } from 'lucide-react';
import { shareUrl, useCreateShareLink, useRevokeShareLink, useShareLinks, type ShareLink } from '../api/share';
import { useApp } from '../store/app-store';
import { t } from '../i18n';
import { dateTime, relativeTime } from '../pages/detail/time';
import { SW } from './icons';
import { NO_PROTECT, protectBody, protectNote, ShareProtectFields, type ProtectValue } from './share-protect';

const DAYS: [number, string][] = [[1, t('{n} 天', { n: 1 })], [7, t('{n} 天', { n: 7 })], [30, t('{n} 天', { n: 30 })], [365, t('一年')]];

async function copy(text: string): Promise<boolean> {
  try { await navigator.clipboard.writeText(text); return true; } catch { return false; }
}

export function ShareLinkDialog({ doc, open, onClose }: { doc: { id: number; title: string; hasArchive: boolean; mimeType: string }; open: boolean; onClose: () => void }) {
  const app = useApp();
  const links = useShareLinks(doc.id, open);
  const create = useCreateShareLink(doc.id);
  const revoke = useRevokeShareLink(doc.id);
  const [days, setDays] = useState(7);
  const [file, setFile] = useState<'archive' | 'original'>(doc.hasArchive ? 'archive' : 'original');
  const [fresh, setFresh] = useState<ShareLink | null>(null);
  const [protect, setProtect] = useState<ProtectValue>(NO_PROTECT);
  const noPDF = !canWatermark(doc);

  const make = () => create.mutate({ days, file, ...protectBody(protect) }, {
    onSuccess: async (l) => {
      setFresh(l);
      app.toast(await copy(shareUrl(l.token)) ? t('已生成链接并复制') : t('已生成链接'));
    },
  });
  const copyLink = async (l: ShareLink) => app.toast(await copy(shareUrl(l.token)) ? t('已复制链接') : t('复制失败，请手动选择链接复制'));

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm" aria-labelledby="share-title">
      <DialogTitle id="share-title">{t('分享链接')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 2 }}>
          {t('拿到链接的人不用登录就能查看“{title}”（允许时还能下载），直到链接过期或被作废。请只发给信任的人。', { title: doc.title })}
        </Typography>
        {create.error && <Alert severity="error" sx={{ mb: 2 }}>{create.error.message}</Alert>}
        <Stack direction="row" spacing={1.5} sx={{ alignItems: 'flex-start', flexWrap: 'wrap', rowGap: 1.5 }}>
          <TextField select label={t('有效期')} value={days} onChange={(e) => setDays(Number(e.target.value))} sx={{ width: 120 }}>
            {DAYS.map(([d, l]) => <MenuItem key={d} value={d}>{l}</MenuItem>)}
          </TextField>
          <TextField select label={t('文件')} value={file} onChange={(e) => setFile(e.target.value as 'archive' | 'original')} sx={{ width: 190 }}>
            <MenuItem value="archive" disabled={!doc.hasArchive}>{t('可搜索版（PDF）')}</MenuItem>
            <MenuItem value="original">{t('原件')}</MenuItem>
          </TextField>
        </Stack>
        <Stack sx={{ mt: 2 }}><ShareProtectFields value={protect} onChange={setProtect} noPDF={noPDF} /></Stack>
        <Stack direction="row" sx={{ mt: 1.5 }}>
          <Button variant="contained" disabled={create.isPending} onClick={make}>{t('生成链接')}</Button>
        </Stack>
        {fresh && (
          <TextField fullWidth sx={{ mt: 2 }} label={t('新链接')} value={shareUrl(fresh.token)} slotProps={{
            input: { readOnly: true, endAdornment: <IconButton aria-label={t('复制新链接')} onClick={() => void copyLink(fresh)}><Copy size={16} strokeWidth={SW} /></IconButton> },
            htmlInput: { onFocus: (e: React.FocusEvent<HTMLInputElement>) => e.target.select() },
          }} />
        )}
        <Typography variant="h6" sx={{ mt: 3, mb: 1 }}>{t('有效的链接')}</Typography>
        {links.isPending ? <Skeleton variant="rounded" height={56} />
          : links.error ? <Alert severity="error">{links.error.message}</Alert>
            : !links.data?.length ? <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('还没有分享过，或者链接都已过期、作废。')}</Typography> : (
              <Box>
                {links.data.map((l, i) => (
                  <Stack key={l.id} direction="row" spacing={1} sx={{ alignItems: 'center', py: 1, borderTop: i ? 1 : 0, borderColor: 'divider' }}>
                    <Box sx={{ flex: 1, minWidth: 0 }}>
                      <Typography variant="body2" noWrap sx={{ fontFamily: 'monospace' }}>…/s/{l.token.slice(0, 8)}…</Typography>
                      <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                        {l.file === 'archive' ? t('可搜索版') : t('原件')} · {t('{name} 生成', { name: l.creatorName })} · <Tooltip title={dateTime(l.expiresAt)}><span>{t('{date} 到期', { date: dateTime(l.expiresAt).slice(0, 10) })}</span></Tooltip>
                        {protectNote(l) ? ` · ${protectNote(l)}` : ''}
                        {' · '}{l.accessCount ? t('打开 {n} 次，最近 {time}', { n: l.accessCount, time: relativeTime(l.lastAccessAt!) }) : t('还没有人打开')}
                      </Typography>
                    </Box>
                    <Tooltip title={t('复制链接')}><IconButton size="small" aria-label={t('复制链接')} onClick={() => void copyLink(l)}><Copy size={16} strokeWidth={SW} /></IconButton></Tooltip>
                    <Tooltip title={t('作废')}><IconButton size="small" aria-label={t('作废链接')} disabled={revoke.isPending}
                      onClick={() => revoke.mutate(l.id, { onSuccess: () => { if (fresh?.id === l.id) setFresh(null); app.toast(t('已作废，链接立即失效')); }, onError: (e) => app.toast(e.message) })}>
                      <Link2Off size={16} strokeWidth={SW} />
                    </IconButton></Tooltip>
                  </Stack>
                ))}
              </Box>
            )}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}><Button onClick={onClose}>{t('关闭')}</Button></DialogActions>
    </Dialog>
  );
}

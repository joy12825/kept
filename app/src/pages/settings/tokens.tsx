import { useState, type FormEvent } from 'react';
import { Alert, Box, Button, Card, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, Stack, TextField, Typography } from '@mui/material';
import { Copy, Trash2 } from 'lucide-react';
import { SW } from '../../components/icons';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import { useCreateToken, useRevokeToken, useTokens } from '../../api/tokens';
import type { ApiToken } from '../../api/client';
import { useApp } from '../../store/app-store';
import { locale, t } from '../../i18n';

const fmt = (iso?: string) => (iso ? new Date(iso).toLocaleString(locale(), { hour12: false }) : t('从未使用'));

export function TokensPanel() {
  const app = useApp();
  const tokens = useTokens();
  const create = useCreateToken();
  const revoke = useRevokeToken();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [plain, setPlain] = useState<string | null>(null);
  const [toRevoke, setToRevoke] = useState<ApiToken | null>(null);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate(name, { onSuccess: (r) => { setCreating(false); setName(''); setPlain(r.token); } });
  };

  return (
    <>
      <Stack
        direction="row"
        sx={{
          alignItems: "center",
          mb: 1
        }}>
        <Typography variant="h4" sx={{ flex: 1 }}>{t('API Token')}</Typography>
        <Button variant="contained" onClick={() => setCreating(true)}>{t('新建 Token')}</Button>
      </Stack>
      <Typography
        variant="body2"
        sx={{
          color: "text.secondary",
          mb: 3
        }}>
        {t('供脚本、扫描仪或第三方程序调用接口：在请求头中加入')} <code>Authorization: Bearer {t('你的 Token')}</code>{t('。')}
      </Typography>

      {tokens.isPending && <LoadingState />}
      {tokens.isError && <ErrorState onRetry={() => tokens.refetch()} />}
      {tokens.data?.length === 0 && <EmptyState title={t('还没有 API Token')} text={t('新建一个 Token，让脚本也能把文件交给系统。')} />}
      <Stack spacing={1}>
        {tokens.data?.map((tk) => (
          <Card key={tk.id} sx={{ p: 2, display: 'flex', alignItems: 'center', gap: 2 }}>
            <Box sx={{ flex: 1 }}>
              <Typography sx={{ fontWeight: 500 }}>{tk.name}</Typography>
              <Typography variant="caption" sx={{
                color: "text.secondary"
              }}>
                <Box component="span" sx={{ fontFamily: 'monospace' }}>{tk.prefix}…</Box> · {t('创建于 {time}', { time: fmt(tk.createdAt) })} · {t('最近使用 {time}', { time: fmt(tk.lastUsedAt) })}
              </Typography>
            </Box>
            <IconButton aria-label={t('吊销 {name}', { name: tk.name })} onClick={() => setToRevoke(tk)}><Trash2 size={18} strokeWidth={SW} /></IconButton>
          </Card>
        ))}
      </Stack>

      <Dialog open={creating} onClose={() => setCreating(false)} fullWidth maxWidth="xs">
        <form onSubmit={submit}>
          <DialogTitle>{t('新建 API Token')}</DialogTitle>
          <DialogContent>
            {create.error && <Alert severity="error" sx={{ mb: 2 }}>{create.error.message}</Alert>}
            <TextField label={t('名称')} value={name} onChange={(e) => setName(e.target.value)} helperText={t('用来辨认用途，例如“扫描仪脚本”')} fullWidth required autoFocus sx={{ mt: 1 }} />
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setCreating(false)}>{t('取消')}</Button>
            <Button type="submit" variant="contained" disabled={create.isPending}>{t('创建')}</Button>
          </DialogActions>
        </form>
      </Dialog>

      <Dialog open={plain !== null} fullWidth maxWidth="sm">
        <DialogTitle>{t('Token 已创建')}</DialogTitle>
        <DialogContent>
          <Alert severity="warning" sx={{ mb: 2 }}>{t('完整的 Token 只显示这一次，请立即复制并妥善保存。')}</Alert>
          <Stack direction="row" spacing={1} sx={{
            alignItems: "center"
          }}>
            <Box sx={{ flex: 1, p: 1.5, bgcolor: 'surface.containerLow', borderRadius: 2, fontFamily: 'monospace', wordBreak: 'break-all' }}>{plain}</Box>
            <IconButton
              aria-label={t('复制')}
              onClick={async () => {
                try {
                  if (!navigator.clipboard) throw new Error('clipboard unavailable');
                  await navigator.clipboard.writeText(plain ?? '');
                  app.toast(t('已复制'));
                } catch {
                  app.toast(t('无法自动复制，请手动选中复制'));
                }
              }}
            ><Copy size={18} strokeWidth={SW} /></IconButton>
          </Stack>
        </DialogContent>
        <DialogActions><Button variant="contained" onClick={() => setPlain(null)}>{t('关闭')}</Button></DialogActions>
      </Dialog>

      <Dialog open={toRevoke !== null} onClose={() => setToRevoke(null)} fullWidth maxWidth="xs">
        <DialogTitle>{t('吊销“{name}”？', { name: toRevoke?.name ?? '' })}</DialogTitle>
        <DialogContent><Typography>{t('吊销后，使用这个 Token 的脚本将无法再访问。')}</Typography></DialogContent>
        <DialogActions>
          <Button onClick={() => setToRevoke(null)}>{t('取消')}</Button>
          <Button color="error" variant="contained" disabled={revoke.isPending}
            onClick={() => toRevoke && revoke.mutate(toRevoke.id, { onSuccess: () => { setToRevoke(null); app.toast(t('已吊销')); }, onError: (e) => app.toast(e.message) })}>{t('吊销')}</Button>
        </DialogActions>
      </Dialog>
    </>
  );
}

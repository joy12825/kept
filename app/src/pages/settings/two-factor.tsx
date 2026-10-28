import { useEffect, useState, type FormEvent } from 'react';
import { Alert, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Stack, TextField, Typography } from '@mui/material';
import { Copy } from 'lucide-react';
import { toDataURL } from 'qrcode';
import { SW } from '../../components/icons';
import {
  useBeginTwoFactor, useDisableTwoFactor, useEnableTwoFactor, useRegenerateRecoveryCodes, useTwoFactorStatus,
} from '../../api/two-factor';
import type { TwoFactorSetup } from '../../api/client';
import { useApp } from '../../store/app-store';
import { t, tc } from '../../i18n';

const formatSecret = (secret: string) => secret.replace(/(.{4})/g, '$1 ').trim();

function RecoveryCodesView({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const app = useApp();
  const copyAll = async () => {
    try {
      if (!navigator.clipboard) throw new Error('clipboard unavailable');
      await navigator.clipboard.writeText(codes.join('\n'));
      app.toast(t('已复制'));
    } catch {
      app.toast(t('无法自动复制，请手动选中复制'));
    }
  };
  return (
    <>
      <DialogContent>
        <Alert severity="warning" sx={{ mb: 2 }}>{t('恢复码只显示这一次。请保存在安全的地方，手机丢失时可以用它登录。')}</Alert>
        <Box sx={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 1, fontFamily: 'monospace' }}>
          {codes.map((c) => <Box key={c}>{c}</Box>)}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button startIcon={<Copy size={16} strokeWidth={SW} />} onClick={copyAll}>{t('复制全部')}</Button>
        <Button variant="contained" onClick={onDone}>{t('完成')}</Button>
      </DialogActions>
    </>
  );
}

export function TwoFactorPanel() {
  const app = useApp();
  const status = useTwoFactorStatus();
  const begin = useBeginTwoFactor();
  const enable = useEnableTwoFactor();
  const disable = useDisableTwoFactor();
  const regenerate = useRegenerateRecoveryCodes();

  const [setup, setSetup] = useState<TwoFactorSetup | null>(null);
  const [qrDataUrl, setQrDataUrl] = useState<string | null>(null);
  const [code, setCode] = useState('');
  const [codes, setCodes] = useState<string[] | null>(null);
  const [confirmMode, setConfirmMode] = useState<'regenerate' | 'disable' | null>(null);
  const [password, setPassword] = useState('');

  useEffect(() => {
    if (!setup) { setQrDataUrl(null); return; }
    let cancelled = false;
    toDataURL(setup.otpauthUri).then((url) => { if (!cancelled) setQrDataUrl(url); });
    return () => { cancelled = true; };
  }, [setup]);

  const startSetup = () => {
    enable.reset();
    begin.mutate(undefined, { onSuccess: (s) => { setSetup(s); setCode(''); } });
  };

  const closeSetup = () => { setSetup(null); setCode(''); enable.reset(); };

  const submitCode = (e: FormEvent) => {
    e.preventDefault();
    enable.mutate({ code }, { onSuccess: (r) => { setCodes(r.codes); setSetup(null); } });
  };

  const finishCodes = () => { setCodes(null); enable.reset(); regenerate.reset(); };

  const openConfirm = (mode: 'regenerate' | 'disable') => {
    disable.reset(); regenerate.reset();
    setConfirmMode(mode); setPassword('');
  };
  const closeConfirm = () => { setConfirmMode(null); setPassword(''); };

  const submitConfirm = (e: FormEvent) => {
    e.preventDefault();
    if (confirmMode === 'disable') {
      disable.mutate({ password }, { onSuccess: () => { setConfirmMode(null); setPassword(''); app.toast(t('已关闭两步验证')); } });
    } else if (confirmMode === 'regenerate') {
      regenerate.mutate({ password }, { onSuccess: (r) => { setCodes(r.codes); setConfirmMode(null); setPassword(''); } });
    }
  };

  const confirmError = confirmMode === 'disable' ? disable.error : regenerate.error;
  const confirmTitle = confirmMode === 'disable' ? t('关闭两步验证') : t('重新生成恢复码');

  return (
    <Box sx={{ mt: 4 }}>
      <Typography variant="h5" sx={{ mb: 1.5 }}>{t('两步验证')}</Typography>

      {status.data && !status.data.enabled && (
        <>
          <Typography
            variant="body2"
            sx={{
              color: "text.secondary",
              mb: 1.5
            }}>{t('登录时除了密码，还需要输入身份验证器 App 生成的 6 位验证码。')}</Typography>
          <Button variant="contained" onClick={startSetup} disabled={begin.isPending}>{t('开启两步验证')}</Button>
        </>
      )}

      {status.data?.enabled && (
        <>
          <Typography variant="body2" sx={{ mb: 1.5 }}>{t('已开启 · 剩余 {n} 个恢复码', { n: status.data.recoveryCodesLeft })}</Typography>
          <Stack direction="row" spacing={1.5}>
            <Button variant="outlined" onClick={() => openConfirm('regenerate')}>{t('重新生成恢复码')}</Button>
            <Button color="error" variant="outlined" onClick={() => openConfirm('disable')}>{t('关闭两步验证')}</Button>
          </Stack>
        </>
      )}

      <Dialog open={setup !== null} onClose={closeSetup} fullWidth maxWidth="xs">
        <DialogTitle>{t('开启两步验证')}</DialogTitle>
        <form onSubmit={submitCode}>
          <DialogContent>
            {qrDataUrl && <Box sx={{ display: 'flex', justifyContent: 'center', mb: 2 }}><img src={qrDataUrl} alt={t('两步验证二维码')} width={180} height={180} /></Box>}
            {setup && (
              <Typography
                variant="body2"
                sx={{
                  color: "text.secondary",
                  mb: 2
                }}>
                {t('无法扫码？手动输入密钥：')}<Box component="span" sx={{ fontFamily: 'monospace' }}>{formatSecret(setup.secret)}</Box>
              </Typography>
            )}
            <TextField
              label={t('6 位验证码')} value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))} fullWidth autoFocus required
              error={!!enable.error}
              helperText={enable.error?.message} slotProps={{
              htmlInput: { inputMode: 'numeric' }
            }}
            />
          </DialogContent>
          <DialogActions>
            <Button onClick={closeSetup}>{t('取消')}</Button>
            <Button type="submit" variant="contained" disabled={enable.isPending}>{t('验证并开启')}</Button>
          </DialogActions>
        </form>
      </Dialog>

      <Dialog open={codes !== null} fullWidth maxWidth="xs">
        <DialogTitle>{tc('多个', '恢复码')}</DialogTitle>
        {codes && <RecoveryCodesView codes={codes} onDone={finishCodes} />}
      </Dialog>

      <Dialog open={confirmMode !== null} onClose={closeConfirm} fullWidth maxWidth="xs">
        <DialogTitle>{confirmTitle}</DialogTitle>
        <form onSubmit={submitConfirm}>
          <DialogContent>
            <Typography
              variant="body2"
              sx={{
                color: "text.secondary",
                mb: 2
              }}>{t('请输入当前密码以确认。')}</Typography>
            <TextField
              label={t('当前密码')} type="password" value={password} onChange={(e) => setPassword(e.target.value)} fullWidth autoFocus required
              autoComplete="current-password" error={!!confirmError} helperText={confirmError?.message}
            />
          </DialogContent>
          <DialogActions>
            <Button onClick={closeConfirm}>{t('取消')}</Button>
            <Button type="submit" variant="contained" disabled={disable.isPending || regenerate.isPending}>{confirmTitle}</Button>
          </DialogActions>
        </form>
      </Dialog>
    </Box>
  );
}

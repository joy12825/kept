// 确认密码（v1.6.4）：打开敏感文档前输入密码；开了两步验证的也可以输验证码。
import { useState, type FormEvent, type ReactNode } from 'react';
import { Alert, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Link, Stack, TextField, Typography } from '@mui/material';
import { Lock } from 'lucide-react';
import { useVerifySession } from '../api/sensitive';
import { useMe } from '../api/session';
import { SW } from './icons';
import { t } from '../i18n';

export function VerifyDialog({ open, onClose, onDone }: { open: boolean; onClose: () => void; onDone?: () => void }) {
  const verify = useVerifySession();
  const twoFactor = !!useMe().data?.user.twoFactorEnabled;
  const [useCode, setUseCode] = useState(false);
  const [value, setValue] = useState('');
  const close = () => { setValue(''); setUseCode(false); verify.reset(); onClose(); };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    verify.mutate(useCode ? { code: value.trim() } : { password: value }, { onSuccess: () => { close(); onDone?.(); } });
  };
  return (
    <Dialog open={open} onClose={close} fullWidth maxWidth="xs" aria-labelledby="verify-title">
      <form onSubmit={submit}>
        <DialogTitle id="verify-title">{t('确认是你本人')}</DialogTitle>
        <DialogContent>
          <Stack spacing={2}>
            <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('这是敏感文档。确认后 15 分钟内可以查看敏感文档，不用再输入。')}</Typography>
            <TextField autoFocus label={useCode ? t('两步验证码') : t('密码')} type={useCode ? 'text' : 'password'} value={value} onChange={(e) => setValue(e.target.value)}
              autoComplete={useCode ? 'one-time-code' : 'current-password'}
              slotProps={{ htmlInput: useCode ? { inputMode: 'numeric', maxLength: 8 } : { maxLength: 256 } }} />
            {twoFactor && (
              <Link component="button" type="button" variant="body2" sx={{ alignSelf: 'flex-start' }} onClick={() => { setUseCode(!useCode); setValue(''); verify.reset(); }}>
                {useCode ? t('改用密码') : t('改用两步验证码')}
              </Link>
            )}
            {verify.error && <Alert severity="error">{verify.error.message}</Alert>}
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 2 }}>
          <Button onClick={close}>{t('取消')}</Button>
          <Button type="submit" variant="contained" disabled={!value.trim() || verify.isPending}>{t('确认')}</Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}

/** 锁住的预览区：说明为什么看不到，按钮打开确认密码。 */
export function LockedPanel({ text = t('确认密码后可以查看预览、原件和识别文字。'), action }: { text?: ReactNode; action?: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', textAlign: 'center', gap: 1.5, py: 10, px: 3 }}>
      <Box sx={{ color: 'text.secondary' }}><Lock size={40} strokeWidth={SW} /></Box>
      <Typography variant="h6">{t('这是敏感文档')}</Typography>
      <Typography variant="body2" sx={{ color: 'text.secondary', maxWidth: 360 }}>{text}</Typography>
      {action ?? <Button variant="contained" onClick={() => setOpen(true)}>{t('确认密码')}</Button>}
      <VerifyDialog open={open} onClose={() => setOpen(false)} />
    </Box>
  );
}

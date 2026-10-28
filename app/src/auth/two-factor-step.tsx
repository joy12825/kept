import { useState } from 'react';
import { Box, Button, CircularProgress, Link, Stack, TextField, Typography } from '@mui/material';
import { ArrowLeft, ShieldCheck } from 'lucide-react';
import { SW } from '../components/icons';
import { ApiError } from '../api/client';
import { useLoginTwoFactor } from '../api/session';
import { fontMono } from '../theme';
import { t } from '../i18n';

export interface TwoFactorStepProps {
  challenge: string;
  recoveryMode: boolean;
  onToggleRecovery: () => void;
  onBack: () => void;
  /** 挑战已过期：回到登录步骤，并带上服务端返回的说明文字。 */
  onExpired: (message: string) => void;
  /** 触发了登录失败锁定：回到登录步骤，并进入锁定倒计时。 */
  onLocked: (seconds: number) => void;
}

/** 两步验证：验证码（6 位，输满自动提交）或恢复码（手动提交）二选一。 */
export function TwoFactorStep({ challenge, recoveryMode, onToggleRecovery, onBack, onExpired, onLocked }: TwoFactorStepProps) {
  const [code, setCode] = useState('');
  const [codeErr, setCodeErr] = useState(false);
  const [recoveryCode, setRecoveryCode] = useState('');
  const mutation = useLoginTwoFactor();

  const handleError = (err: Error) => {
    if (err instanceof ApiError) {
      if (err.code === 'challenge_expired') { onExpired(err.message); return; }
      if (err.status === 429) {
        const seconds = err.details?.retryAfterSeconds;
        onLocked(typeof seconds === 'number' ? seconds : 0);
        return;
      }
    }
    setCodeErr(true);
    setCode('');
  };

  const verifyCode = (c: string) => {
    if (c.length !== 6 || mutation.isPending) return;
    mutation.mutate({ challenge, code: c }, { onError: handleError });
  };

  const verifyRecovery = () => {
    if (!recoveryCode || mutation.isPending) return;
    mutation.mutate({ challenge, recoveryCode }, { onError: handleError });
  };

  return (
    <>
      <Button startIcon={<ArrowLeft size={16} strokeWidth={SW} />} onClick={onBack} sx={{ ml: -1.5, mb: 2, color: 'text.secondary' }}>
        {t('返回登录')}
      </Button>
      <Box sx={{ width: 48, height: 48, borderRadius: 3, bgcolor: 'surface.secondaryContainer', color: 'surface.onSecondaryContainer', display: 'flex', alignItems: 'center', justifyContent: 'center', mb: 2 }}>
        <ShieldCheck size={24} strokeWidth={SW} />
      </Box>
      <Typography variant="h2">{t('两步验证')}</Typography>

      {!recoveryMode && (
        <>
          <Typography
            sx={{
              color: "text.secondary",
              mt: 0.5,
              mb: 3
            }}>{t('打开身份验证器 App，输入 6 位验证码。')}</Typography>
          <TextField size="medium"
            fullWidth autoFocus value={code} error={codeErr}
            helperText={codeErr ? t('验证码不正确或已过期，请重新输入。') : ' '}
            placeholder="000000"
            onChange={(e) => {
              const v = e.target.value.replace(/\D/g, '').slice(0, 6);
              setCode(v); setCodeErr(false);
              if (v.length === 6) verifyCode(v);
            }}
            slotProps={{
              htmlInput: { inputMode: 'numeric', maxLength: 6, style: { fontSize: 28, letterSpacing: '0.5em', textAlign: 'center', fontFamily: fontMono } }
            }}
          />
          <Button fullWidth variant="contained" size="large" sx={{ height: 48, mt: 1 }} disabled={code.length !== 6 || mutation.isPending} onClick={() => verifyCode(code)}>
            {mutation.isPending ? <CircularProgress size={20} color="inherit" /> : t('验证')}
          </Button>
          <Stack
            direction="row"
            sx={{
              justifyContent: "center",
              mt: 2
            }}>
            <Link component="button" type="button" variant="body2" underline="hover" onClick={onToggleRecovery}>{t('改用恢复码')}</Link>
          </Stack>
        </>
      )}

      {recoveryMode && (
        <>
          <Typography
            sx={{
              color: "text.secondary",
              mt: 0.5,
              mb: 3
            }}>{t('输入保存的恢复码，每个只能用一次。')}</Typography>
          <TextField size="medium" fullWidth autoFocus label={t('恢复码')} value={recoveryCode} onChange={(e) => setRecoveryCode(e.target.value)} />
          <Button fullWidth variant="contained" size="large" sx={{ height: 48, mt: 2 }} disabled={!recoveryCode || mutation.isPending} onClick={verifyRecovery}>
            {mutation.isPending ? <CircularProgress size={20} color="inherit" /> : t('验证')}
          </Button>
          <Stack
            direction="row"
            sx={{
              justifyContent: "center",
              mt: 2
            }}>
            <Link component="button" type="button" variant="body2" underline="hover" onClick={onToggleRecovery}>{t('改用验证码')}</Link>
          </Stack>
        </>
      )}
    </>
  );
}

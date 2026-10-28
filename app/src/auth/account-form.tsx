import { useState, type FormEvent } from 'react';
import { Alert, Box, Button, CircularProgress, IconButton, InputAdornment, Stack, TextField, Typography } from '@mui/material';
import { Eye, EyeOff } from 'lucide-react';
import { SW } from '../components/icons';
import { ApiError } from '../api/client';
import { useSetup } from '../api/session';
import { t } from '../i18n';

const WEAK_LABELS = [t('太短'), t('较弱'), t('中等'), t('强')];

/** 密码强度：不足 8 位为 0；否则统计字母、数字、符号三类，结果至少为 1。与设计稿的 strength() 一致。 */
function strength(p: string): number {
  if (p.length < 8) return 0;
  let n = 0;
  if (/[a-zA-Z]/.test(p)) n++;
  if (/\d/.test(p)) n++;
  if (/[^a-zA-Z0-9]/.test(p)) n++;
  return Math.max(1, n);
}

const barColor = (st: number, i: number) => {
  if (st < i) return 'surface.containerHigh';
  if (st === 1) return 'error.main';
  if (st === 2) return 'warning.main';
  return 'success.main';
};

/** 首次初始化时创建管理员账号的表单：名字、邮箱、密码（带强度条）、确认密码。此后的账号都由管理员在“账号管理”中创建。 */
export function AccountForm() {
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [show, setShow] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const mutation = useSetup();

  const st = strength(password);
  const mismatch = !!confirmPassword && confirmPassword !== password;
  const canSubmit = !!name && /@/.test(email) && st >= 2 && confirmPassword === password && !mutation.isPending;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    setErrorMessage(null);
    mutation.mutate(
      { displayName: name, email, password },
      { onError: (err) => setErrorMessage(err instanceof ApiError ? err.message : t('网络错误，请稍后重试')) },
    );
  };

  return (
    <>
      <Typography variant="h2">{t('创建管理员账号')}</Typography>
      <Typography
        sx={{
          color: "text.secondary",
          mt: 0.5,
          mb: 3
        }}>
        {t('这是第一次启动。创建的账号拥有全部权限，之后可以在“账号管理”中为其他成员创建账号。')}
      </Typography>

      {errorMessage && (
        <Alert severity="error" sx={{ mb: 2.5 }}>{errorMessage}</Alert>
      )}

      <Box component="form" onSubmit={submit}>
        <Stack spacing={2}>
          <TextField size="medium"
            label={t('名字')} value={name} autoFocus
            onChange={(e) => { setName(e.target.value); setErrorMessage(null); }}
          />
          <TextField size="medium"
            label={t('邮箱')} type="email" value={email}
            onChange={(e) => { setEmail(e.target.value); setErrorMessage(null); }}
          />
          <Box>
            <TextField size="medium"
              fullWidth label={t('密码')} type={show ? 'text' : 'password'} value={password}
              onChange={(e) => { setPassword(e.target.value); setErrorMessage(null); }}
              slotProps={{
                input: {
                  endAdornment: (
                    <InputAdornment position="end">
                      <IconButton size="small" edge="end" onClick={() => setShow((v) => !v)} aria-label={show ? t('隐藏密码') : t('显示密码')}>
                        {show ? <EyeOff size={18} strokeWidth={SW} /> : <Eye size={18} strokeWidth={SW} />}
                      </IconButton>
                    </InputAdornment>
                  ),
                }
              }}
            />
            <Stack direction="row" spacing={0.5} sx={{ mt: 1 }}>
              {[1, 2, 3].map((i) => (
                <Box key={i} sx={{ flex: 1, height: 4, borderRadius: 2, bgcolor: password ? barColor(st, i) : 'surface.containerHigh', transition: 'background 150ms' }} />
              ))}
            </Stack>
            <Typography
              variant="caption"
              sx={{
                color: "text.secondary",
                display: 'block',
                mt: 0.5
              }}>
              {password ? `${WEAK_LABELS[st]} · ` : ''}{t('至少 8 位，建议包含字母、数字和符号')}
            </Typography>
          </Box>
          <TextField size="medium"
            label={t('确认密码')} type={show ? 'text' : 'password'} value={confirmPassword} error={mismatch}
            helperText={mismatch ? t('两次输入的密码不一致') : ' '}
            onChange={(e) => { setConfirmPassword(e.target.value); setErrorMessage(null); }}
          />
        </Stack>
        <Button type="submit" fullWidth variant="contained" size="large" disabled={!canSubmit} sx={{ height: 48, mt: 0.5 }}>
          {mutation.isPending ? <CircularProgress size={20} color="inherit" /> : t('创建并进入')}
        </Button>
      </Box>
    </>
  );
}

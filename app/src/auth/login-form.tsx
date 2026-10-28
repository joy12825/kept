import { useState, type FormEvent } from 'react';
import { Alert, Box, Button, Checkbox, CircularProgress, Divider, FormControlLabel, IconButton, InputAdornment, Link, Stack, TextField, Typography } from '@mui/material';
import { Eye, EyeOff } from 'lucide-react';
import { SW } from '../components/icons';
import { ApiError } from '../api/client';
import { useLogin } from '../api/session';
import { useServerHealth } from './server-status';
import { ssoStartUrl } from './sso';
import { t, tc } from '../i18n';

export interface LoginFormProps {
  errorMessage: string | null;
  lockSeconds: number;
  serverDown: boolean;
  /** 会话在使用中过期时由 AuthGate/LoginPage 传入：回填账号、焦点放在密码框，并显示过期提示。 */
  expired?: { login: string } | null;
  onError: (message: string | null) => void;
  onLocked: (seconds: number) => void;
  onServerDown: (down: boolean) => void;
  onChallenge: (challenge: string) => void;
  onForgot: () => void;
  /** 配置了单点登录时（v1.0），表单下方显示“使用 name 登录”。 */
  sso?: { name: string; default: boolean };
  /** 单点登录没有成功的原因（不标红输入框）。 */
  ssoError?: string | null;
}

const mmss = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;

/** 登录步骤：用户名/邮箱 + 密码，处理锁定倒计时和服务器无法连接两种错误态。 */
export function LoginForm({ errorMessage, lockSeconds, serverDown, expired = null, onError, onLocked, onServerDown, onChallenge, onForgot, sso, ssoError = null }: LoginFormProps) {
  const [login, setLogin] = useState(expired?.login ?? '');
  const [password, setPassword] = useState('');
  const [remember, setRemember] = useState(true);
  const [show, setShow] = useState(false);
  const mutation = useLogin();
  const health = useServerHealth();
  const locked = lockSeconds > 0;
  // 服务器无法连接：要么底部健康检查明确失败，要么这次登录请求本身抛出了网络异常。
  const down = serverDown || health.data === false;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!login || !password || locked || down || mutation.isPending) return;
    onError(null);
    mutation.mutate({ login, password, remember }, {
      onSuccess: (outcome) => {
        onServerDown(false);
        if (outcome.kind === 'challenge') onChallenge(outcome.challenge);
      },
      onError: (err) => {
        if (err instanceof ApiError) {
          onServerDown(false);
          if (err.status === 401) {
            const n = err.details?.remainingAttempts;
            onError(typeof n === 'number' ? t('用户名或密码不正确。再错 {n} 次，账号会被锁定 15 分钟。', { n }) : err.message);
          } else if (err.status === 429) {
            const s = err.details?.retryAfterSeconds;
            onLocked(typeof s === 'number' ? s : 0);
          } else {
            onError(err.message);
          }
        } else {
          // fetch 本身抛出异常（网络错误）：标记服务器无法连接。
          onServerDown(true);
        }
      },
    });
  };

  const retry = () => {
    onServerDown(false);
    health.refetch();
  };

  // 锁定是按账号的：锁定期间只禁用密码框和登录按钮，用户名或邮箱仍可修改；一旦修改就清除
  // 倒计时，以便换一个账号登录（服务端仍会按新账号的计数判断）。
  const disabledPassword = locked || down;

  return (
    <>
      <Typography variant="h2">{t('登录')}</Typography>
      <Typography
        sx={{
          color: "text.secondary",
          mt: 0.5,
          mb: 3
        }}>{t('使用你的账号登录')}</Typography>
      <Stack spacing={1.5} sx={{ mb: (down || locked || errorMessage || expired || ssoError) ? 2.5 : 0 }}>
        {ssoError && !errorMessage && <Alert severity="error">{ssoError}</Alert>}
        {expired && <Alert severity="info">{t('登录已过期，请重新登录。你刚才的页面会在登录后恢复。')}</Alert>}
        {down && (
          <Alert severity="error" action={<Button color="inherit" size="small" onClick={retry}>{t('重试')}</Button>}>
            {t('无法连接到服务器。请检查服务器地址，或确认服务正在运行。')}
          </Alert>
        )}
        {locked && <Alert severity="warning">{t('登录失败次数过多，账号已锁定。请在 {time} 后重试，或联系管理员解锁。', { time: mmss(lockSeconds) })}</Alert>}
        {errorMessage && !locked && <Alert severity="error">{errorMessage}</Alert>}
      </Stack>
      <Box component="form" onSubmit={submit}>
        <Stack spacing={2}>
          <TextField size="medium"
            label={t('用户名或邮箱')} value={login} autoComplete="username" autoFocus={!expired} disabled={down} error={!!errorMessage}
            onChange={(e) => { setLogin(e.target.value); onError(null); if (locked) onLocked(0); }}
          />
          <TextField size="medium"
            label={t('密码')} type={show ? 'text' : 'password'} value={password} autoComplete="current-password" autoFocus={!!expired} disabled={disabledPassword} error={!!errorMessage}
            onChange={(e) => { setPassword(e.target.value); onError(null); }}
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
        </Stack>
        <Stack
          direction="row"
          sx={{
            alignItems: "center",
            justifyContent: "space-between",
            mt: 1,
            mb: 2
          }}>
          <FormControlLabel
            control={<Checkbox size="small" checked={remember} onChange={(e) => setRemember(e.target.checked)} />}
            label={<Typography variant="body2">{t('30 天内记住我')}</Typography>}
          />
          <Link component="button" type="button" variant="body2" underline="hover" onClick={onForgot}>{t('忘记密码？')}</Link>
        </Stack>
        <Button type="submit" fullWidth variant="contained" size="large" disabled={!login || !password || locked || down || mutation.isPending} sx={{ height: 48 }}>
          {mutation.isPending ? <CircularProgress size={20} color="inherit" /> : t('登录')}
        </Button>
      </Box>
      {sso && (
        <>
          <Divider sx={{ my: 2.5, color: 'text.secondary', fontSize: 13 }}>{tc('分隔', '或')}</Divider>
          <Button fullWidth variant="outlined" size="large" disabled={down} sx={{ height: 48 }} onClick={() => window.location.assign(ssoStartUrl(remember))}>
            {sso.default ? t('单点登录') : t('使用{name}登录', { name: sso.name })}
          </Button>
        </>
      )}
    </>
  );
}

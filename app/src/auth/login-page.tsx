import { useEffect, useState } from 'react';
import { Box, Button, IconButton, Stack, Tooltip, Typography } from '@mui/material';
import { alpha } from '@mui/material/styles';
import { useQuery } from '@tanstack/react-query';
import { Moon, Sun } from 'lucide-react';
import { BrandMark, SW } from '../components/icons';
import { useApp } from '../store/app-store';
import { useBranding } from '../api/branding';
import { api } from '../api/client';
import { LoginForm } from './login-form';
import { TwoFactorStep } from './two-factor-step';
import { ForgotPassword } from './forgot-password';
import { ServerStatus } from './server-status';
import { AccountForm } from './account-form';
import { takeSSOError } from './sso';
import { useSetupStatus } from '../api/session';
import { applyPreference, currentLang, t } from '../i18n';

type Step = 'login' | 'twofactor' | 'forgot' | 'setup';

export interface LoginPageProps {
  initialStep?: 'login' | 'setup';
  /** 会话在使用中过期时，任务 6 用它在登录框里回填账号并提示。 */
  expired?: { login: string } | null;
}

function formatVersion(v: string) {
  return /^\d/.test(v) ? `v${v}` : v;
}

function useServerVersion() {
  return useQuery<string | null>({
    queryKey: ['system-version'],
    queryFn: async () => {
      const r = await api.GET('/api/v1/system/version');
      if (r.error || !r.response.ok || !r.data) return null;
      return r.data.version;
    },
    retry: false,
    refetchOnWindowFocus: false,
  });
}

/** 登录页框架：单栏，点阵底色，页面正中是标语和卡片里的步骤切换（登录、两步验证、忘记密码、初始化）；顶部是产品名和深浅色切换，底部是服务器状态和版本号。 */
export function LoginPage({ initialStep = 'login', expired = null }: LoginPageProps) {
  const { s, set, setTheme } = useApp();
  const brand = useBranding();
  const version = useServerVersion();
  const [step, setStep] = useState<Step>(initialStep);
  const [twofactor, setTwofactor] = useState<{ challenge: string; recovery: boolean } | null>(null);
  const [lockSeconds, setLockSeconds] = useState(0);
  // 单点登录失败时服务端转回 /?sso=原因
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [ssoError] = useState<string | null>(() => takeSSOError());
  const sso = useSetupStatus().data?.sso;
  const [serverDown, setServerDown] = useState(false);

  // 锁定倒计时：每秒减一，归零后停止；用 (lockSeconds > 0) 做依赖，避免每次都重建定时器。
  useEffect(() => {
    if (lockSeconds <= 0) return;
    const id = window.setInterval(() => setLockSeconds((x) => Math.max(0, x - 1)), 1000);
    return () => window.clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lockSeconds > 0]);

  const backToLogin = () => { setStep('login'); setTwofactor(null); };

  const dark = s.mode === 'dark';
  return (
    <Box sx={(theme) => ({
      display: 'flex', flexDirection: 'column', minHeight: '100vh',
      // 浅底加强调色点阵；深色模式下换成最深的底色
      bgcolor: dark ? 'surface.containerLowest' : 'surface.containerLow',
      backgroundImage: `radial-gradient(${alpha(theme.palette.primary.main, dark ? 0.1 : 0.14)} 1px, transparent 1.2px)`,
      backgroundSize: '24px 24px',
    })}>
      <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center', height: 64, px: 3 }}>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          <BrandMark size={28} />
          <Typography sx={{ fontWeight: 500 }}>{brand.name}</Typography>
        </Stack>
        <Box sx={{ flex: 1 }} />
        {/* 没登录时按浏览器语言；这里选的记在这台浏览器上，登录后存到账号（v1.10.0） */}
        {currentLang() === 'zh'
          ? <Button size="small" lang="en" onClick={() => applyPreference('en')}>English</Button>
          : <Button size="small" lang="zh-CN" onClick={() => applyPreference('zh')}>中文</Button>}{/* i18n-ignore：用那种语言自己写 */}
        <Tooltip title={s.mode === 'light' ? t('深色模式') : t('浅色模式')}>
          <IconButton onClick={() => setTheme(s.mode === 'light' ? 'dark' : 'light')}>
            {s.mode === 'light' ? <Moon size={18} strokeWidth={SW} /> : <Sun size={18} strokeWidth={SW} />}
          </IconButton>
        </Tooltip>
      </Stack>

      <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 4, px: 3, py: 4 }}>
        <Box sx={{ maxWidth: 560, textAlign: 'center' }}>
          <Typography sx={{ fontSize: { xs: 28, sm: 36 }, lineHeight: { xs: '36px', sm: '44px' }, color: 'text.primary', textWrap: 'pretty' }}>{t('文件放进抽屉，需要时一搜就有')}</Typography>
          <Typography sx={{ color: 'text.secondary', fontSize: 15, mt: 1 }}>{t('自动识别 · 按抽屉整理 · 部署在你自己的服务器')}</Typography>
        </Box>
        <Box sx={(theme) => ({
          width: '100%', maxWidth: 452, boxSizing: 'border-box', p: { xs: 3, sm: 4.5 }, borderRadius: '16px', bgcolor: 'surface.container',
          boxShadow: dark ? '0 1px 3px rgb(0 0 0 / 0.5)' : `0 1px 2px rgb(0 0 0 / 0.12), 0 2px 8px ${alpha(theme.palette.primary.main, 0.08)}`,
        })}>
          {step === 'login' && (
            <LoginForm
              errorMessage={errorMessage}
              lockSeconds={lockSeconds}
              serverDown={serverDown}
              expired={expired}
              onError={setErrorMessage}
              onLocked={setLockSeconds}
              onServerDown={setServerDown}
              onChallenge={(challenge) => { setTwofactor({ challenge, recovery: false }); setStep('twofactor'); setErrorMessage(null); }}
              onForgot={() => setStep('forgot')}
              sso={sso}
              ssoError={ssoError}
            />
          )}
          {step === 'twofactor' && twofactor && (
            <TwoFactorStep
              challenge={twofactor.challenge}
              recoveryMode={twofactor.recovery}
              onToggleRecovery={() => setTwofactor((tf) => tf && { ...tf, recovery: !tf.recovery })}
              onBack={backToLogin}
              onExpired={(message) => { setErrorMessage(message); backToLogin(); }}
              onLocked={(seconds) => { setLockSeconds(seconds); backToLogin(); }}
            />
          )}
          {step === 'forgot' && <ForgotPassword onBack={backToLogin} />}
          {step === 'setup' && <AccountForm />}
        </Box>
      </Box>

      <Stack direction="row" sx={{ alignItems: 'center', pr: 3 }}>
        <ServerStatus />
        <Box sx={{ flex: 1 }} />
        {version.data && <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('{version} · 自托管版', { version: formatVersion(version.data) })}</Typography>}
      </Stack>
    </Box>
  );
}

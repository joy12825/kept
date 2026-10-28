import { useEffect, useState, type FormEvent } from 'react';
import { Alert, Box, Button, Stack, TextField, Typography } from '@mui/material';
import { useMe } from '../../api/session';
import { useChangePassword, useUpdateMe } from '../../api/me';
import { useApp } from '../../store/app-store';
import { t } from '../../i18n';

export function ProfileForm() {
  const app = useApp();
  const me = useMe().data;
  const update = useUpdateMe();
  const [displayName, setDisplayName] = useState('');
  const [email, setEmail] = useState('');

  useEffect(() => {
    if (me) { setDisplayName(me.user.displayName); setEmail(me.user.email ?? ''); }
  }, [me]);

  const emailValid = !!email && email.includes('@');

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!emailValid) return;
    update.mutate({ displayName, email }, { onSuccess: () => app.toast(t('已保存')) });
  };

  return (
    <>
      <Typography variant="h4" sx={{ mb: 3 }}>{t('个人资料')}</Typography>
      <Stack component="form" onSubmit={submit} spacing={2.5} sx={{ maxWidth: 380 }}>
        {update.error && <Alert severity="error">{update.error.message}</Alert>}
        <TextField label={t('用户名')} value={me?.user.username ?? ''} disabled helperText={t('用户名用于登录，不能修改')} />
        <TextField label={t('显示名称')} value={displayName} onChange={(e) => setDisplayName(e.target.value)} required />
        <TextField label={t('邮箱')} type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        <Box><Button type="submit" variant="contained" disabled={update.isPending || !emailValid}>{t('保存修改')}</Button></Box>
      </Stack>
    </>
  );
}

export function PasswordForm() {
  const app = useApp();
  const change = useChangePassword();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [again, setAgain] = useState('');
  const [mismatch, setMismatch] = useState(false);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (next !== again) { setMismatch(true); return; }
    setMismatch(false);
    change.mutate({ currentPassword: current, newPassword: next }, {
      onSuccess: () => { setCurrent(''); setNext(''); setAgain(''); app.toast(t('已修改密码，其他登录、已连接的手机和 API Token 都已失效')); },
    });
  };

  return (
    <>
      <Typography variant="h4" sx={{ mb: 1 }}>{t('密码')}</Typography>
      <Typography sx={{ color: 'text.secondary', mb: 3 }}>{t('修改后，除了现在这个页面，其他浏览器里的登录、已连接的手机和 API Token 都会失效：手机需要重新配对，脚本需要换新的 Token。')}</Typography>
      <Stack component="form" onSubmit={submit} spacing={2.5} sx={{ maxWidth: 380 }}>
        {mismatch && <Alert severity="error">{t('两次输入的新密码不一致')}</Alert>}
        {change.error && <Alert severity="error">{change.error.message}</Alert>}
        <TextField label={t('当前密码')} type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" required />
        <TextField label={t('新密码')} type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" helperText={t('至少 8 个字符')} required />
        <TextField label={t('再输入一次新密码')} type="password" value={again} onChange={(e) => setAgain(e.target.value)} autoComplete="new-password" required />
        <Box><Button type="submit" variant="contained" disabled={change.isPending}>{t('修改密码')}</Button></Box>
      </Stack>
    </>
  );
}

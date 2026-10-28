import { useState, type FormEvent } from 'react';
import { Alert, Box, Button, Card, Checkbox, Chip, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Menu, MenuItem, IconButton, Stack, TextField, Typography } from '@mui/material';
import { MoreHorizontal } from 'lucide-react';
import { SW } from '../../components/icons';
import { ErrorState, LoadingState } from '../../components/common';
import { useCreateUser, useDisableUserTwoFactor, useSetUserPassword, useUnlinkUserSSO, useUpdateUser, useUsers } from '../../api/admin';
import { useMe } from '../../api/session';
import type { User } from '../../api/client';
import { useApp } from '../../store/app-store';
import { t, tc } from '../../i18n';

export function AccountsPanel() {
  const app = useApp();
  const me = useMe().data;
  const users = useUsers();
  const update = useUpdateUser();
  const unlink = useUnlinkUserSSO();
  const [menu, setMenu] = useState<{ el: HTMLElement; user: User } | null>(null);
  const [creating, setCreating] = useState(false);
  const [resetting, setResetting] = useState<User | null>(null);
  const [disabling2fa, setDisabling2fa] = useState<User | null>(null);

  const act = (vars: Parameters<typeof update.mutate>[0], done: string) => {
    setMenu(null);
    update.mutate(vars, { onSuccess: () => app.toast(done), onError: (e) => app.toast(e.message) });
  };

  return (
    <>
      <Stack
        direction="row"
        sx={{
          alignItems: "center",
          mb: 3
        }}>
        <Typography variant="h4" sx={{ flex: 1 }}>{t('账号管理')}</Typography>
        <Button variant="contained" onClick={() => setCreating(true)}>{t('新建账号')}</Button>
      </Stack>

      <Typography sx={{ color: 'text.secondary', mb: 3 }}>
        {t('账号由管理员创建，不提供注册。把用户名或邮箱和初始密码告诉对方，对方登录后可以自己修改密码。')}
      </Typography>

      {users.isPending && <LoadingState />}
      {users.isError && <ErrorState onRetry={() => users.refetch()} />}
      <Stack spacing={1}>
        {users.data?.map((u) => (
          <Card key={u.id} sx={{ p: 2, display: 'flex', alignItems: 'center', gap: 2, opacity: u.disabled ? 0.6 : 1 }}>
            <Box sx={{ flex: 1 }}>
              <Typography sx={{ fontWeight: 500 }}>{u.displayName}</Typography>
              <Typography variant="caption" sx={{
                color: "text.secondary"
              }}>{u.username}{u.email ? ` · ${u.email}` : ''}</Typography>
            </Box>
            {u.isAdmin && <Chip size="small" label={tc('实例管理员', '管理员')} color="primary" variant="outlined" />}
            {u.twoFactorEnabled && <Chip size="small" label={t('两步验证')} />}
            {u.sso && <Chip size="small" label={t('单点登录')} />}
            {u.disabled && <Chip size="small" label={t('已停用')} />}
            <IconButton aria-label={t('管理 {name}', { name: u.username })} onClick={(e) => setMenu({ el: e.currentTarget, user: u })}><MoreHorizontal size={18} strokeWidth={SW} /></IconButton>
          </Card>
        ))}
      </Stack>

      <Menu anchorEl={menu?.el} open={!!menu} onClose={() => setMenu(null)}>
        {menu && !menu.user.disabled && menu.user.id !== me?.user.id && (
          <MenuItem onClick={() => act({ id: menu.user.id, disabled: true }, t('已停用，该账号的登录已退出'))}>{t('停用账号')}</MenuItem>
        )}
        {menu?.user.disabled && <MenuItem onClick={() => act({ id: menu.user.id, disabled: false }, t('已启用'))}>{t('启用账号')}</MenuItem>}
        {menu && <MenuItem onClick={() => act({ id: menu.user.id, isAdmin: !menu.user.isAdmin }, menu.user.isAdmin ? t('已取消管理员') : t('已设为管理员'))}>{menu.user.isAdmin ? t('取消管理员') : t('设为管理员')}</MenuItem>}
        {menu && menu.user.id !== me?.user.id && (
          <MenuItem onClick={() => { setResetting(menu.user); setMenu(null); }}>{t('重置密码')}</MenuItem>
        )}
        {menu && menu.user.twoFactorEnabled && menu.user.id !== me?.user.id && (
          <MenuItem onClick={() => { setDisabling2fa(menu.user); setMenu(null); }}>{t('关闭两步验证')}</MenuItem>
        )}
        {menu?.user.sso && (
          <MenuItem onClick={() => {
            const u = menu.user;
            setMenu(null);
            app.confirm({
              title: t('解除 {name} 的单点登录绑定？', { name: u.displayName || u.username }),
              body: t('解除后，下次用单点登录时会按邮箱重新找到这个账号并绑定。适用于对方在身份服务里换了账号的情况。'),
              verb: t('解除'),
              run: () => { app.confirm(null); unlink.mutate({ id: u.id }, { onSuccess: () => app.toast(t('已解除单点登录绑定')), onError: (e) => app.toast(e.message) }); },
            });
          }}>{t('解除单点登录绑定')}</MenuItem>
        )}
      </Menu>

      <CreateUserDialog open={creating} onClose={() => setCreating(false)} />
      <ResetPasswordDialog user={resetting} onClose={() => setResetting(null)} />
      <DisableTwoFactorDialog user={disabling2fa} onClose={() => setDisabling2fa(null)} />
    </>
  );
}

function CreateUserDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const app = useApp();
  const create = useCreateUser();
  const [email, setEmail] = useState('');
  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [isAdmin, setIsAdmin] = useState(false);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate({ email, username: username || undefined, password, displayName: displayName || undefined, isAdmin }, {
      onSuccess: () => { app.toast(t('已创建账号')); setEmail(''); setUsername(''); setDisplayName(''); setPassword(''); setIsAdmin(false); onClose(); },
    });
  };

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs">
      <form onSubmit={submit}>
        <DialogTitle>{t('新建账号')}</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            {create.error && <Alert severity="error">{create.error.message}</Alert>}
            <TextField label={t('邮箱')} type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus />
            <TextField label={t('用户名')} value={username} onChange={(e) => setUsername(e.target.value)} helperText={t('留空时根据邮箱自动生成')} />
            <TextField label={t('显示名称')} value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
            <TextField label={t('初始密码')} type="password" value={password} onChange={(e) => setPassword(e.target.value)} helperText={t('至少 8 个字符，请告知对方登录后修改')} required />
            <FormControlLabel control={<Checkbox checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} />} label={t('设为管理员')} />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>{t('取消')}</Button>
          <Button type="submit" variant="contained" disabled={create.isPending}>{t('创建')}</Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}

function ResetPasswordDialog({ user, onClose }: { user: User | null; onClose: () => void }) {
  const app = useApp();
  const reset = useSetUserPassword();
  const [password, setPassword] = useState('');

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!user) return;
    reset.mutate({ id: user.id, password }, { onSuccess: () => { app.toast(t('已重置密码，该账号的登录、已连接的手机和 API Token 都已失效')); setPassword(''); onClose(); } });
  };

  return (
    <Dialog open={user !== null} onClose={onClose} fullWidth maxWidth="xs">
      <form onSubmit={submit}>
        <DialogTitle>{t('重置“{name}”的密码', { name: user?.displayName ?? '' })}</DialogTitle>
        <DialogContent>
          {reset.error && <Alert severity="error" sx={{ mb: 2 }}>{reset.error.message}</Alert>}
          <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>{t('这个账号的所有登录、已连接的手机和 API Token 都会失效，手机需要重新配对。')}</Typography>
          <TextField label={t('新密码')} type="password" value={password} onChange={(e) => setPassword(e.target.value)} helperText={t('至少 8 个字符')} fullWidth required autoFocus sx={{ mt: 1 }} />
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>{t('取消')}</Button>
          <Button type="submit" variant="contained" disabled={reset.isPending}>{t('重置密码')}</Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}

function DisableTwoFactorDialog({ user, onClose }: { user: User | null; onClose: () => void }) {
  const app = useApp();
  const disable = useDisableUserTwoFactor();

  const confirm = () => {
    if (!user) return;
    disable.mutate({ id: user.id }, { onSuccess: () => { app.toast(t('已关闭两步验证')); onClose(); }, onError: (e) => app.toast(e.message) });
  };

  return (
    <Dialog open={user !== null} onClose={onClose} fullWidth maxWidth="xs">
      <DialogTitle>{t('关闭两步验证')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{
          color: "text.secondary"
        }}>{t('关闭 {name} 的两步验证后，对方下次登录只需要密码。', { name: user?.displayName ?? '' })}</Typography>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button color="error" variant="contained" onClick={confirm} disabled={disable.isPending}>{t('关闭两步验证')}</Button>
      </DialogActions>
    </Dialog>
  );
}

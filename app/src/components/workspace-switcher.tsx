// 侧边栏底部：当前空间与空间切换、新建空间、空间设置，以及账号菜单
import { useState } from 'react';
import { Box, Divider, ListItemButton, Menu, MenuItem, Typography } from '@mui/material';
import { Check, ChevronsUpDown, Plus } from 'lucide-react';
import type { Workspace } from '../api/client';
import { useLogout, useMe } from '../api/session';
import { useSwitchWorkspace, useWorkspaces } from '../api/workspaces';
import { useApp } from '../store/app-store';
import { t, tc } from '../i18n';
import { SpaceAvatar, SW } from './icons';
import { CreateWorkspaceDialog, WorkspaceSettingsDialog, workspaceCaption } from './workspace-dialogs';

export function WorkspaceSwitcher({ collapsed, go }: { collapsed: boolean; go: (to: string) => void }) {
  const app = useApp();
  const me = useMe().data;
  const workspaces = useWorkspaces();
  const switchTo = useSwitchWorkspace();
  const logout = useLogout();
  const [el, setEl] = useState<HTMLElement | null>(null);
  const [creating, setCreating] = useState(false);
  const [settings, setSettings] = useState(false);

  if (!me) return null;
  const current = me.workspace;
  const close = () => setEl(null);
  const select = async (ws: Workspace, message = t('已切换到“{name}”', { name: ws.name })) => {
    close();
    await switchTo(ws);
    go('/');
    app.toast(message);
  };
  const caption = { px: 2, py: 0.5, display: 'block', color: 'text.secondary' } as const;

  return (
    <>
      <ListItemButton
        aria-label={t('当前空间：{name}', { name: current.name })} aria-haspopup="menu" onClick={(e) => setEl(e.currentTarget)}
        sx={{ borderRadius: 4, minHeight: 48, px: 0.75, gap: 1.25, justifyContent: collapsed ? 'center' : 'flex-start' }}
      >
        <SpaceAvatar name={current.name} color={current.color} size={28} />
        {!collapsed && (
          <>
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Typography noWrap sx={{ fontSize: 13, fontWeight: 500 }}>{current.name}</Typography>
              <Typography noWrap sx={{ fontSize: 12, color: 'text.secondary' }}>{workspaceCaption(current)}</Typography>
            </Box>
            <ChevronsUpDown size={16} strokeWidth={SW} />
          </>
        )}
      </ListItemButton>

      <Menu
        anchorEl={el} open={!!el} onClose={close}
        anchorOrigin={{ vertical: 'top', horizontal: 'left' }} transformOrigin={{ vertical: 'bottom', horizontal: 'left' }}
        slotProps={{ paper: { sx: { width: 280 } } }}
      >
        <Typography variant="caption" sx={caption}>{tc('复数', '空间')}</Typography>
        {(workspaces.data ?? [current]).map((ws) => {
          const selected = ws.id === current.id;
          return (
            <MenuItem
              key={ws.id} role="menuitemradio" aria-checked={selected} selected={selected} sx={{ gap: 1.5 }}
              onClick={() => { if (selected) close(); else void select(ws); }}
            >
              <SpaceAvatar name={ws.name} color={ws.color} size={28} />
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography noWrap sx={{ fontSize: 13, fontWeight: 500 }}>{ws.name}</Typography>
                <Typography noWrap sx={{ fontSize: 12, color: 'text.secondary' }}>{workspaceCaption(ws)}</Typography>
              </Box>
              {selected && <Box sx={{ color: 'primary.main', display: 'flex' }}><Check size={16} strokeWidth={SW} /></Box>}
            </MenuItem>
          );
        })}
        <MenuItem onClick={() => { close(); setCreating(true); }} sx={{ gap: 1.5, color: 'text.secondary' }}>
          <Plus size={18} strokeWidth={SW} />{t('新建空间')}
        </MenuItem>
        <Divider />
        <MenuItem onClick={() => { close(); go('/settings/members'); }}>{t('空间成员')}</MenuItem>
        {current.role === 'owner' && <MenuItem onClick={() => { close(); setSettings(true); }}>{t('空间设置')}</MenuItem>}
        <Divider />
        <Typography variant="caption" sx={caption}>{me.user.displayName} · {me.user.email ?? me.user.username}</Typography>
        <MenuItem onClick={() => { close(); go('/settings/profile'); }}>{t('个人资料')}</MenuItem>
        <MenuItem onClick={() => { close(); go('/settings/ui'); }}>{t('界面设置')}</MenuItem>
        <Divider />
        <MenuItem onClick={() => { close(); logout.mutate(undefined, { onError: (e) => app.toast(t('没能退出登录：{error}', { error: e.message })) }); }}>{t('退出登录')}</MenuItem>
      </Menu>

      <CreateWorkspaceDialog
        open={creating} onClose={() => setCreating(false)}
        onCreated={(ws) => { setCreating(false); void select(ws, t('已新建并切换到“{name}”', { name: ws.name })); }}
      />
      <WorkspaceSettingsDialog
        workspace={current} open={settings} onClose={() => setSettings(false)}
        onSaved={() => { setSettings(false); app.toast(t('已保存')); }}
        onDeleted={(ws) => {
          setSettings(false);
          const personal = workspaces.data?.find((w) => w.kind === 'personal');
          // 个人空间不带请求头，只需要它的 kind；列表还没取到时也能回去
          void select(personal ?? { ...current, kind: 'personal' }, t('已删除“{name}”', { name: ws.name }));
        }}
      />
    </>
  );
}

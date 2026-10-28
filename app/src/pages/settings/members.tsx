// 设置 → 空间成员：成员列表、修改角色、移除、退出空间、添加已有账号；
// 空间管理员能看到每人上传了多少，并给个别成员设上传额度（v1.7.11）。
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Alert, Avatar, Box, Button, Card, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, IconButton, InputAdornment, Menu, MenuItem,
  Radio, RadioGroup, Select, Stack, TextField, Typography,
} from '@mui/material';
import { MoreHorizontal, Plus } from 'lucide-react';
import type { Workspace, WorkspaceRole } from '../../api/client';
import { ROLE_LABEL, ROLES, useAddMember, useMembers, useMemberUploads, useRemoveMember, useSetMemberUploadQuota, useUpdateMember, type Member, type MemberUpload } from '../../api/members';
import { bytes } from './system';
import { UploadQuotaDialog, eeBuild } from '@ee';
import { useMe } from '../../api/session';
import { useSwitchWorkspace } from '../../api/workspaces';
import { ErrorState, LoadingState } from '../../components/common';
import { SpaceAvatar, SW } from '../../components/icons';
import { workspaceCaption } from '../../components/workspace-dialogs';
import { useApp } from '../../store/app-store';
import { t } from '../../i18n';

const ROLE_HELP = t('管理员可以管理成员和抽屉；成员可以上传和整理文档；只看的成员可以浏览、下载和问答，也能做阅读批注，但不能上传或修改文档。');

function AddMemberDialog({ workspace, open, onClose }: { workspace: Workspace; open: boolean; onClose: () => void }) {
  const app = useApp();
  const add = useAddMember(workspace.id);
  const [login, setLogin] = useState('');
  const [role, setRole] = useState<WorkspaceRole>('editor');
  const loginRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) { setLogin(''); setRole('editor'); add.reset(); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const trimmed = login.trim();
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!trimmed || add.isPending) return;
    add.mutate({ login: trimmed, role }, { onSuccess: (m) => { onClose(); app.toast(t('已添加 {name}', { name: m.displayName })); } });
  };

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs" slotProps={{ transition: { onEntered: () => loginRef.current?.focus() } }}>
      <form onSubmit={submit}>
        <DialogTitle>{t('添加成员')}</DialogTitle>
        <DialogContent>
          <Stack spacing={2.5} sx={{ pt: 1 }}>
            <TextField
              label={t('用户名或邮箱')} value={login} onChange={(e) => { setLogin(e.target.value); add.reset(); }} required fullWidth inputRef={loginRef}
              error={!!add.error}
              helperText={add.error?.message ?? t('只能添加已有的账号。还没有账号的人，请先让管理员在“设置 → 账号管理”中创建。')}
            />
            <TextField select label={t('角色')} value={role} onChange={(e) => setRole(e.target.value as WorkspaceRole)} fullWidth helperText={ROLE_HELP}>
              {ROLES.map((r) => <MenuItem key={r} value={r}>{ROLE_LABEL[r]}</MenuItem>)}
            </TextField>
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 2 }}>
          <Button onClick={onClose}>{t('取消')}</Button>
          <Button type="submit" variant="contained" disabled={!trimmed || add.isPending}>{t('添加成员')}</Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}


export function MembersPanel() {
  const app = useApp();
  const nav = useNavigate();
  const me = useMe().data;
  const workspace = me?.workspace;
  const members = useMembers(workspace?.id);
  const update = useUpdateMember(workspace?.id ?? 0);
  const remove = useRemoveMember(workspace?.id ?? 0);
  const switchTo = useSwitchWorkspace();
  const [adding, setAdding] = useState(false);
  const [menu, setMenu] = useState<{ el: HTMLElement; member: Member } | null>(null);
  const [removing, setRemoving] = useState<Member | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [quotaFor, setQuotaFor] = useState<Member | null>(null);
  const uploads = useMemberUploads(workspace?.role === 'owner' && workspace.kind !== 'personal');

  if (!me || !workspace) return null;
  const isOwner = workspace.role === 'owner';
  const personal = workspace.kind === 'personal';
  const leaving = removing?.userId === me.user.id;

  const changeRole = (m: Member, role: WorkspaceRole) => {
    setError(null);
    update.mutate({ userId: m.userId, role }, {
      onSuccess: () => app.toast(t('已将 {name} 设为{role}', { name: m.displayName, role: ROLE_LABEL[role] })),
      onError: (e) => setError(e.message),
    });
  };
  const confirmRemove = () => {
    if (!removing) return;
    const target = removing;
    setError(null);
    remove.mutate({ userId: target.userId, self: leaving }, {
      onSuccess: async () => {
        setRemoving(null);
        if (target.userId === me.user.id) {
          // 退出后这个空间已不可访问：回到个人空间（不带请求头）
          await switchTo({ id: 0, kind: 'personal' });
          nav('/');
          app.toast(t('已退出“{name}”', { name: workspace.name }));
        } else {
          app.toast(t('已移除 {name}', { name: target.displayName }));
        }
      },
      onError: (e) => { setRemoving(null); setError(e.message); },
    });
  };

  return (
    <>
      <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center', mb: 2 }}>
        <SpaceAvatar name={workspace.name} color={workspace.color} size={40} />
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Typography variant="h4" noWrap>{workspace.name}</Typography>
          <Typography variant="body2" sx={{ color: 'text.secondary' }}>{workspaceCaption(workspace)}</Typography>
        </Box>
        {isOwner && !personal && (
          <Button variant="contained" startIcon={<Plus size={16} strokeWidth={SW} />} onClick={() => setAdding(true)}>{t('添加成员')}</Button>
        )}
      </Stack>

      {error && <Alert severity="error" onClose={() => setError(null)} sx={{ mb: 2 }}>{error}</Alert>}
      {members.isPending && <LoadingState />}
      {members.isError && <ErrorState onRetry={() => members.refetch()} />}
      {members.data && (
        <Card>
          {members.data.map((m, i) => {
            const self = m.userId === me.user.id;
            return (
              <Stack key={m.userId} direction="row" spacing={1.5} sx={{ alignItems: 'center', p: '12px 16px', borderTop: i ? 1 : 0, borderColor: 'divider' }}>
                <Avatar sx={{ width: 36, height: 36, fontSize: 14, bgcolor: 'surface.containerHigh', color: 'text.primary' }}>{m.displayName.slice(0, 1)}</Avatar>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <Typography variant="h6" noWrap>{m.displayName}{self && <Box component="span" sx={{ color: 'text.secondary', fontWeight: 400 }}>{t('（你）')}</Box>}</Typography>
                  <Typography variant="caption" noWrap sx={{ color: 'text.secondary', display: 'block' }}>{m.email ?? m.username}</Typography>
                  {(() => {
                    const u = uploads.data?.find((x) => x.userId === m.userId);
                    if (!u) return null;
                    const near = u.quota > 0 && u.uploaded >= u.quota * 0.9;
                    return (
                      <Typography variant="caption" noWrap sx={{ color: near ? 'warning.main' : 'text.secondary', display: 'block' }}>
                        {u.quota ? t('已上传 {size}，额度 {quota}', { size: bytes(u.uploaded), quota: bytes(u.quota) }) : t('已上传 {size}', { size: bytes(u.uploaded) })}
                      </Typography>
                    );
                  })()}
                </Box>
                {isOwner && !personal ? (
                  <>
                    <Select
                      size="small" value={m.role} disabled={update.isPending} onChange={(e) => changeRole(m, e.target.value as WorkspaceRole)}
                      SelectDisplayProps={{ 'aria-label': t('{name} 的角色', { name: m.displayName }) }} sx={{ width: 112, fontSize: 14 }}
                    >
                      {ROLES.map((r) => <MenuItem key={r} value={r}>{ROLE_LABEL[r]}</MenuItem>)}
                    </Select>
                    <IconButton aria-label={t('{name} 的更多操作', { name: m.displayName })} onClick={(e) => setMenu({ el: e.currentTarget, member: m })}>
                      <MoreHorizontal size={18} strokeWidth={SW} />
                    </IconButton>
                  </>
                ) : (
                  <>
                    <Typography variant="body2" sx={{ color: 'text.secondary' }}>{ROLE_LABEL[m.role]}</Typography>
                    {self && !personal && <Button size="small" color="inherit" onClick={() => setRemoving(m)} sx={{ color: 'text.secondary' }}>{t('退出空间')}</Button>}
                  </>
                )}
              </Stack>
            );
          })}
        </Card>
      )}
      <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', mt: 1.5 }}>
        {personal ? t('个人空间只有你自己。需要和家人或同事一起整理时，可以新建一个家庭或团队空间。') : ROLE_HELP}
      </Typography>

      <Menu anchorEl={menu?.el} open={!!menu} onClose={() => setMenu(null)}>
        {eeBuild && uploads.data && <MenuItem onClick={() => { setQuotaFor(menu!.member); setMenu(null); }}>{t('上传额度…')}</MenuItem>}
        <MenuItem onClick={() => { setRemoving(menu!.member); setMenu(null); }}>
          {menu?.member.userId === me.user.id ? t('退出空间') : t('移除')}
        </MenuItem>
      </Menu>

      <AddMemberDialog workspace={workspace} open={adding} onClose={() => setAdding(false)} />
      {quotaFor && <UploadQuotaDialog member={quotaFor} usage={uploads.data?.find((x) => x.userId === quotaFor.userId)} onClose={() => setQuotaFor(null)} />}

      <Dialog open={!!removing} onClose={() => setRemoving(null)} fullWidth maxWidth="xs">
        <DialogTitle>{leaving ? t('退出“{name}”？', { name: workspace.name }) : t('移除{name}？', { name: removing?.displayName ?? '' })}</DialogTitle>
        <DialogContent>
          <Typography sx={{ color: 'text.secondary' }}>
            {leaving
              ? t('退出后，你将看不到这个空间里的文件。需要再次加入时，请让空间管理员重新添加你。')
              : t('{name} 会立即失去对这个空间的访问。之后可以重新添加。', { name: removing?.displayName ?? '' })}
          </Typography>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 2 }}>
          <Button onClick={() => setRemoving(null)}>{t('取消')}</Button>
          <Button color="error" variant="contained" disabled={remove.isPending} onClick={confirmRemove}>{leaving ? t('退出') : t('移除')}</Button>
        </DialogActions>
      </Dialog>
    </>
  );
}

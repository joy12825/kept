// 新建空间、空间设置（改名、改颜色、删除）两个对话框
import { useEffect, useRef, useState, type FormEvent } from 'react';
import {
  Alert, Box, Button, ButtonBase, Dialog, DialogActions, DialogContent, DialogTitle, Divider, FormControlLabel, Radio, RadioGroup,
  Stack, TextField, Typography,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Check } from 'lucide-react';
import type { Workspace, WorkspaceColor } from '../api/client';
import { useCreateWorkspace, useDeleteWorkspace, useUpdateWorkspace } from '../api/workspaces';
import { SW } from './icons';
import { t } from '../i18n';

const COLORS: [WorkspaceColor, string][] = [
  ['blue', t('蓝')], ['cyan', t('青')], ['green', t('绿')], ['orange', t('橙')], ['red', t('红')], ['purple', t('紫')], ['pink', t('粉')], ['gray', t('灰')],
];
const NAME_MAX = 32;

/** 空间名称下方的一行说明：“个人空间”“家庭 · 3 位成员”“团队 · 8 位成员”。 */
export function workspaceCaption(ws: Pick<Workspace, 'kind' | 'memberCount'>): string {
  if (ws.kind === 'personal') return t('个人空间');
  return ws.kind === 'family' ? t('家庭 · {n} 位成员', { n: ws.memberCount }) : t('团队 · {n} 位成员', { n: ws.memberCount });
}

function ColorPicker({ value, onChange }: { value: WorkspaceColor; onChange: (c: WorkspaceColor) => void }) {
  const theme = useTheme();
  return (
    <Box>
      <Typography id="workspace-color-label" variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>{t('颜色')}</Typography>
      <Stack direction="row" spacing={1} role="radiogroup" aria-labelledby="workspace-color-label">
        {COLORS.map(([c, label]) => (
          <ButtonBase
            key={c} role="radio" aria-checked={value === c} aria-label={label} onClick={() => onChange(c)}
            sx={{
              width: 32, height: 32, borderRadius: '8px', bgcolor: theme.palette.drawer[c], color: '#fff',
              outline: value === c ? `2px solid ${theme.palette.drawer[c]}` : 'none', outlineOffset: 2,
              '&.Mui-focusVisible': { outline: `2px solid ${theme.palette.text.primary}`, outlineOffset: 2 },
            }}
          >
            {value === c && <Check size={16} strokeWidth={SW + 0.75} />}
          </ButtonBase>
        ))}
      </Stack>
    </Box>
  );
}

export function CreateWorkspaceDialog({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (ws: Workspace) => void }) {
  const create = useCreateWorkspace();
  const [name, setName] = useState('');
  const [kind, setKind] = useState<'family' | 'team'>('family');
  const [color, setColor] = useState<WorkspaceColor>('blue');
  const nameRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) { setName(''); setKind('family'); setColor('blue'); create.reset(); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const trimmed = name.trim();
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!trimmed || create.isPending) return;
    create.mutate({ name: trimmed, kind, color }, { onSuccess: onCreated });
  };

  return (
    // 对话框是从菜单里打开的：菜单关闭时会把焦点还给触发按钮，autoFocus 会被抢走，所以等进入动画结束后再聚焦
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs" slotProps={{ transition: { onEntered: () => nameRef.current?.focus() } }}>
      <form onSubmit={submit}>
        <DialogTitle>{t('新建空间')}</DialogTitle>
        <DialogContent>
          <Typography sx={{ color: 'text.secondary', mb: 2.5 }}>{t('和家人或同事一起整理文件。创建后，你是这个空间的管理员，可以添加成员。')}</Typography>
          {create.error && <Alert severity="error" sx={{ mb: 2.5 }}>{create.error.message}</Alert>}
          <Stack spacing={2.5}>
            <TextField
              label={t('名称')} value={name} onChange={(e) => setName(e.target.value)} required fullWidth inputRef={nameRef}
              placeholder={t('例如：王家')} slotProps={{ htmlInput: { maxLength: NAME_MAX } }}
            />
            <Box>
              <Typography id="workspace-kind-label" variant="body2" sx={{ color: 'text.secondary' }}>{t('类型')}</Typography>
              <RadioGroup row aria-labelledby="workspace-kind-label" value={kind} onChange={(e) => setKind(e.target.value as 'family' | 'team')}>
                <FormControlLabel value="family" control={<Radio size="small" />} label={t('家庭')} />
                <FormControlLabel value="team" control={<Radio size="small" />} label={t('团队')} />
              </RadioGroup>
            </Box>
            <ColorPicker value={color} onChange={setColor} />
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 2 }}>
          <Button onClick={onClose}>{t('取消')}</Button>
          <Button type="submit" variant="contained" disabled={!trimmed || create.isPending}>{t('创建空间')}</Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}

export function WorkspaceSettingsDialog({ workspace, open, onClose, onSaved, onDeleted }: {
  workspace: Workspace; open: boolean; onClose: () => void; onSaved: () => void; onDeleted: (ws: Workspace) => void;
}) {
  const update = useUpdateWorkspace();
  const remove = useDeleteWorkspace();
  const [name, setName] = useState(workspace.name);
  const [color, setColor] = useState<WorkspaceColor>(workspace.color);
  const [deleting, setDeleting] = useState(false);
  const [typed, setTyped] = useState('');

  useEffect(() => {
    if (open) { setName(workspace.name); setColor(workspace.color); setDeleting(false); setTyped(''); update.reset(); remove.reset(); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, workspace.id]);

  const trimmed = name.trim();
  const changed = trimmed !== workspace.name || color !== workspace.color;
  const save = (e: FormEvent) => {
    e.preventDefault();
    if (!trimmed || !changed || update.isPending) return;
    update.mutate({ id: workspace.id, name: trimmed, color }, { onSuccess: onSaved });
  };
  const error = update.error ?? remove.error;

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs">
      <DialogTitle>{t('空间设置')}</DialogTitle>
      <DialogContent>
        {error && <Alert severity="error" sx={{ mb: 2.5 }}>{error.message}</Alert>}
        <Box component="form" id="workspace-settings-form" onSubmit={save} sx={{ pt: 1 }}>
          <Stack spacing={2.5}>
            <TextField label={t('名称')} value={name} onChange={(e) => setName(e.target.value)} required fullWidth slotProps={{ htmlInput: { maxLength: NAME_MAX } }} />
            <ColorPicker value={color} onChange={setColor} />
          </Stack>
        </Box>
        {workspace.kind !== 'personal' && (
          <>
            <Divider sx={{ my: 3 }} />
            {!deleting ? (
              <Stack direction="row" spacing={2} sx={{ alignItems: 'center' }}>
                <Typography variant="body2" sx={{ color: 'text.secondary', flex: 1 }}>{t('删除后，所有成员立即无法访问；30 天后彻底清除。')}</Typography>
                <Button color="error" onClick={() => setDeleting(true)}>{t('删除空间…')}</Button>
              </Stack>
            ) : (
              <Stack spacing={2}>
                <Typography variant="body2">{t('删除“{name}”？所有成员会立即失去访问，空间里的文件在 30 天后彻底清除。', { name: workspace.name })}</Typography>
                <TextField label={t('输入空间名称以确认')} value={typed} onChange={(e) => setTyped(e.target.value)} placeholder={workspace.name} fullWidth autoFocus />
                <Stack direction="row" spacing={1} sx={{ justifyContent: 'flex-end' }}>
                  <Button onClick={() => { setDeleting(false); setTyped(''); }}>{t('不删除')}</Button>
                  <Button
                    color="error" variant="contained" disabled={typed !== workspace.name || remove.isPending}
                    onClick={() => remove.mutate({ id: workspace.id, name: typed }, { onSuccess: () => onDeleted(workspace) })}
                  >{t('删除空间')}</Button>
                </Stack>
              </Stack>
            )}
          </>
        )}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('关闭')}</Button>
        <Button type="submit" form="workspace-settings-form" variant="contained" disabled={!trimmed || !changed || update.isPending}>{t('保存')}</Button>
      </DialogActions>
    </Dialog>
  );
}

// “保存筛选”：把全部文件当前的搜索词、筛选条件、排序和显示方式（列表或网格、显示的列）存为常用筛选（空间成员共享），出现在侧边栏。
import { useState } from 'react';
import { Button, Dialog, DialogActions, DialogContent, DialogTitle, TextField, Typography } from '@mui/material';
import { toFilter, type DocListState } from '../api/filters';
import { viewDisplay } from './saved-view-item';
import { useCreateView } from '../api/views';
import { useApp } from '../store/app-store';
import { t } from '../i18n';

export function SaveViewDialog({ state, onClose }: { state: DocListState; onClose: () => void }) {
  const app = useApp();
  const create = useCreateView();
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const save = () => {
    const filter = toFilter(state);
    create.mutate({ name: name.trim(), filter: filter ? JSON.parse(filter) : {}, sort: `${state.sort}:${state.dir}`, display: viewDisplay(state) }, {
      onSuccess: (v) => { app.toast(t('已保存为常用筛选“{name}”，可在侧边栏找到', { name: v.name })); onClose(); },
      onError: (e) => setError(e.message),
    });
  };
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="save-view-title">
      <DialogTitle id="save-view-title">{t('保存筛选')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 2 }}>{t('当前的搜索词、筛选条件、排序和显示方式会保存下来，空间里的成员都能在侧边栏中套用。')}</Typography>
        <TextField autoFocus fullWidth label={t('名称')} value={name} error={!!error} helperText={error || ' '}
          onChange={(e) => { setName(e.target.value); setError(''); }} slotProps={{ htmlInput: { maxLength: 32 } }}
          onKeyDown={(e) => { if (e.key === 'Enter' && name.trim()) save(); }} />
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!name.trim() || create.isPending} onClick={save}>{t('保存')}</Button>
      </DialogActions>
    </Dialog>
  );
}

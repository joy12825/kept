// 新建或修改书单：名称和说明。
import { useEffect, useState } from 'react';
import { Alert, Button, Dialog, DialogActions, DialogContent, DialogTitle, Stack, TextField } from '@mui/material';
import { useCreateBookList, useUpdateBookList, type BookList } from '../../api/booklists';
import { t } from '../../i18n';

export function BookListDialog({ list, open, onClose, onSaved }: { list?: BookList; open: boolean; onClose: () => void; onSaved?: (l: BookList) => void }) {
  const create = useCreateBookList();
  const update = useUpdateBookList(list?.id ?? 0);
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');
  useEffect(() => { if (open) { setName(list?.name ?? ''); setDesc(list?.description ?? ''); create.reset(); update.reset(); } }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const m = list ? update : create;
  const submit = () => {
    const body = { name: name.trim(), description: desc.trim() };
    const done = { onSuccess: (l: BookList) => { onSaved?.(l); onClose(); } };
    if (list) update.mutate(body, done);
    else create.mutate(body, done);
  };
  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="booklist-title" component="form" onSubmit={(e: React.FormEvent) => { e.preventDefault(); submit(); }}>
      <DialogTitle id="booklist-title">{list ? t('修改书单') : t('新建书单')}</DialogTitle>
      <DialogContent>
        {m.error && <Alert severity="error" sx={{ mb: 2 }}>{m.error.message}</Alert>}
        <Stack spacing={2.5} sx={{ pt: 1 }}>
          <TextField autoFocus required label={t('名称')} value={name} onChange={(e) => setName(e.target.value)} helperText={t('例如 四大名著、孩子的暑假书单')} slotProps={{ htmlInput: { maxLength: 40 } }} />
          <TextField label={t('说明')} value={desc} onChange={(e) => setDesc(e.target.value)} multiline minRows={2} helperText={t('可选')} slotProps={{ htmlInput: { maxLength: 500 } }} />
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button type="submit" variant="contained" disabled={m.isPending || !name.trim()}>{list ? t('保存') : t('新建')}</Button>
      </DialogActions>
    </Dialog>
  );
}

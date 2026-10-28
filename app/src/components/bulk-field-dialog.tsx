// 批量设置字段（v1.5.2）：给选中的文档设同一个自定义字段的值，或清空它；例如把一批发票的“报销状态”设为“已报销”。
import { useEffect, useState } from 'react';
import { Button, Dialog, DialogActions, DialogContent, DialogTitle, MenuItem, Stack, TextField, Typography } from '@mui/material';
import { useBulk } from '../api/documents';
import { useTaxonomy, type CustomField } from '../api/taxonomy';
import { useApp } from '../store/app-store';
import { CustomFieldInput } from './doc-fields';
import { t, tc } from '../i18n';

export function BulkFieldDialog({ open, onClose, onDone, selection, count }: {
  open: boolean; onClose: () => void; onDone: () => void;
  selection: { ids: number[] } | { filter: Record<string, unknown> }; count: number;
}) {
  const app = useApp();
  const bulk = useBulk();
  const fields = ((useTaxonomy('custom-fields').data ?? []) as CustomField[]).filter((f) => f.dataType !== 'document_link');
  const [id, setId] = useState<number | ''>('');
  const [value, setValue] = useState<unknown>(null);
  useEffect(() => { if (open) { setId(''); setValue(null); } }, [open]);
  const field = fields.find((f) => f.id === id);
  const empty = value === null || value === undefined || value === '';
  const run = (v: unknown) => field && bulk.mutate({ ...selection, edit: { customFields: [{ fieldId: field.id, value: v }] } }, {
    onSuccess: (r) => { app.toast(v === null ? t('已清空 {n} 份文档的“{name}”', { n: r.count, name: field.name }) : t('已为 {n} 份文档设置“{name}”', { n: r.count, name: field.name })); onClose(); onDone(); },
    onError: (e) => app.toast(e.message),
  });
  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="bulk-field-title">
      <DialogTitle id="bulk-field-title">{t('给 {n} 份文档设字段', { n: count })}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          <TextField select label={t('字段')} value={id} onChange={(e) => { setId(Number(e.target.value)); setValue(null); }}>
            {fields.map((f) => <MenuItem key={f.id} value={f.id}>{f.name}</MenuItem>)}
          </TextField>
          {field && <CustomFieldInput field={field} value={value} selfId={0} onChange={setValue} />}
          {field && <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('会覆盖这些文档原来的“{name}”。', { name: field.name })}</Typography>}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        {field && <Button sx={{ mr: 'auto' }} color="inherit" disabled={bulk.isPending} onClick={() => run(null)}>{t('清空这个字段')}</Button>}
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!field || empty || bulk.isPending} onClick={() => run(value)}>{tc('动作', '设置')}</Button>
      </DialogActions>
    </Dialog>
  );
}

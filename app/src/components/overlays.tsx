import { useEffect, useState } from 'react';
import {
  Button, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, Snackbar, TextField, Typography,
} from '@mui/material';
import { X } from 'lucide-react';
import { useApp } from '../store/app-store';
import { SW } from './icons';
import { t } from '../i18n';

export function ConfirmDialog() {
  const app = useApp(); const c = app.s.confirm;
  const [typed, setTyped] = useState('');
  useEffect(() => setTyped(''), [c]);
  const ok = !c?.needText || typed === c.needText;
  return (
    <Dialog open={!!c} onClose={() => app.confirm(null)} maxWidth="xs" fullWidth>
      <DialogTitle>{c?.title}</DialogTitle>
      <DialogContent>
        <Typography sx={{
          color: "text.secondary"
        }}>{c?.body}</Typography>
        {c?.needText && <TextField autoFocus fullWidth sx={{ mt: 2 }} placeholder={c.needText} value={typed} onChange={(e) => setTyped(e.target.value)} />}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={() => app.confirm(null)}>{t('取消')}</Button>
        <Button variant="contained" color={c?.safe ? 'primary' : 'error'} disabled={!ok} onClick={() => c?.run()}>{c?.verb}</Button>
      </DialogActions>
    </Dialog>
  );
}

export function Toast() {
  const app = useApp(); const toast = app.s.toast;
  const n = app.s.uploads.length;
  const bottom = n ? (app.s.trayOpen ? 72 + Math.min(n * 64, 320) + 24 : 88) : 24;
  return (
    <Snackbar key={toast?.key} open={!!toast} anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }} sx={{ bottom: `${bottom}px !important` }} message={toast?.text}
      action={<>{toast?.undo && <Button size="small" sx={{ color: 'surface.inversePrimary' }} onClick={toast.undo}>{t('撤销')}</Button>}<IconButton size="small" sx={{ color: 'inherit' }} onClick={() => app.set({ toast: null })}><X size={16} strokeWidth={SW} /></IconButton></>} />
  );
}


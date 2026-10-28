// 目录 → 存放位置（v1.6.3）：纸质原件放在哪里。可以新建、改名、写说明、删除，
// 按原件编号范围把一盒原件一次放进去，点“查看文档”看放在这里的文档。
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Alert, Box, Button, ButtonBase, Dialog, DialogActions, DialogContent, DialogTitle, Stack, TextField, Typography } from '@mui/material';
import { Archive, Plus } from 'lucide-react';
import { useAssignLocationByAsn, useDeleteLocation, useLocations, useSaveLocation, type Location } from '../../api/locations';
import { emptyListFilters } from '../../api/filters';
import { useApp } from '../../store/app-store';
import { EmptyState, ErrorState } from '../../components/common';
import { SW } from '../../components/icons';
import { t } from '../../i18n';

export function LocationsPanel({ canWrite, creating, onCreated }: { canWrite: boolean; creating: boolean; onCreated: () => void }) {
  const list = useLocations();
  const app = useApp();
  const nav = useNavigate();
  const [editing, setEditing] = useState<Location | null>(null);
  const [assigning, setAssigning] = useState<Location | null>(null);
  const items = list.data ?? [];
  const browse = (l: Location) => {
    app.setDocList('files', (s) => ({ ...s, q: '', f: { ...emptyListFilters(), location: [l.id] }, extra: undefined, sel: [] }));
    nav('/files');
  };

  return (
    <Box sx={{ mt: 2 }}>
      {list.isError && <ErrorState onRetry={() => list.refetch()} />}
      {list.isSuccess && !items.length && (
        <EmptyState icon={<Archive size={40} strokeWidth={SW} />} title={t('还没有存放位置')}
          text={t('记下纸质原件放在哪里，比如“书房柜子第 2 层”“文件盒 A”“银行保险箱”。需要原件时，详情页会告诉你去哪里找。')}
          action={canWrite ? t('新建存放位置') : undefined} onAction={canWrite ? onCreated : undefined} />
      )}
      {items.map((l) => (
        <Stack key={l.id} direction={{ xs: 'column', sm: 'row' }} spacing={{ xs: 0.5, sm: 2 }} sx={{ alignItems: { sm: 'center' }, py: 1.25, px: 1, borderBottom: 1, borderColor: 'divider' }}>
          <ButtonBase onClick={() => (canWrite ? setEditing(l) : browse(l))} sx={{ flex: 1, minWidth: 0, justifyContent: 'flex-start', textAlign: 'left', borderRadius: 1, display: 'block' }}>
            <Typography noWrap>{l.name}</Typography>
            <Typography variant="body2" noWrap sx={{ color: 'text.secondary' }}>
              {l.documentCount ? t('{n} 份文档', { n: l.documentCount }) : t('还没有文档')}{l.note ? ` · ${l.note}` : ''}
            </Typography>
          </ButtonBase>
          <Stack direction="row" spacing={1} sx={{ flex: 'none' }}>
            <Button size="small" disabled={!l.documentCount} onClick={() => browse(l)}>{t('查看文档')}</Button>
            {canWrite && <Button size="small" onClick={() => setAssigning(l)}>{t('按原件编号放入')}</Button>}
          </Stack>
        </Stack>
      ))}
      {(creating || editing) && <LocationDialog location={editing} onClose={() => { setEditing(null); if (creating) onCreated(); }} />}
      {assigning && <AssignDialog location={assigning} onClose={() => setAssigning(null)} />}
    </Box>
  );
}

function LocationDialog({ location, onClose }: { location: Location | null; onClose: () => void }) {
  const app = useApp();
  const save = useSaveLocation();
  const del = useDeleteLocation();
  const [name, setName] = useState(location?.name ?? '');
  const [note, setNote] = useState(location?.note ?? '');
  const submit = () => save.mutate({ id: location?.id, body: { name: name.trim(), note: note.trim() } }, {
    onSuccess: (l) => { app.toast(location ? t('已保存') : t('已新建存放位置“{name}”', { name: l.name })); onClose(); },
  });
  const remove = () => location && app.confirm({
    title: t('删除“{name}”？', { name: location.name }), verb: t('删除'),
    body: location.documentCount ? t('{n} 份文档会不再记录存放位置，文档本身不删除。', { n: location.documentCount }) : t('这里还没有文档。'),
    run: () => del.mutate(location.id, { onSuccess: () => { app.toast(t('已删除')); onClose(); }, onError: (e) => app.toast(e.message) }),
  });
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="location-title">
      <DialogTitle id="location-title">{location ? t('编辑存放位置') : t('新建存放位置')}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          <TextField label={t('名称')} value={name} onChange={(e) => setName(e.target.value)} placeholder={t('例如：文件盒 A')} autoFocus slotProps={{ htmlInput: { maxLength: 64 } }} />
          <TextField label={t('说明')} value={note} onChange={(e) => setNote(e.target.value)} placeholder={t('例如：书房柜子第 2 层')} slotProps={{ htmlInput: { maxLength: 200 } }} />
          {save.error && <Alert severity="error">{save.error.message}</Alert>}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        {location && <Button color="error" onClick={remove} sx={{ mr: 'auto' }}>{t('删除')}</Button>}
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!name.trim() || save.isPending} onClick={submit}>{location ? t('保存') : t('新建')}</Button>
      </DialogActions>
    </Dialog>
  );
}

/** 按原件编号范围放入：一盒原件通常是连续的编号。 */
function AssignDialog({ location, onClose }: { location: Location; onClose: () => void }) {
  const app = useApp();
  const assign = useAssignLocationByAsn();
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  useEffect(() => { assign.reset(); }, [from, to]); // eslint-disable-line react-hooks/exhaustive-deps
  const a = Number(from), b = Number(to || from);
  const ok = Number.isInteger(a) && Number.isInteger(b) && a >= 1 && b >= a;
  const submit = () => assign.mutate({ locationId: location.id, from: a, to: b }, {
    onSuccess: (r) => { app.toast(r.updated ? t('已把 {n} 份文档放到“{name}”', { n: r.updated, name: location.name }) : t('这个范围里没有文档')); if (r.updated) onClose(); },
  });
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="assign-title">
      <DialogTitle id="assign-title">{t('放入“{name}”', { name: location.name })}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 2 }}>{t('原件编号在这个范围里的文档都记为放在这里（包括两端），原来记的位置会被替换。')}</Typography>
        <Stack direction="row" spacing={1.5}>
          <TextField label={t('从')} value={from} onChange={(e) => setFrom(e.target.value.replace(/\D/g, ''))} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 6 } }} />
          <TextField label={t('到')} value={to} onChange={(e) => setTo(e.target.value.replace(/\D/g, ''))} placeholder={from} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 6 } }} />
        </Stack>
        {assign.error && <Alert severity="error" sx={{ mt: 2 }}>{assign.error.message}</Alert>}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" startIcon={<Plus size={16} strokeWidth={SW} />} disabled={!ok || assign.isPending} onClick={submit}>{t('放入')}</Button>
      </DialogActions>
    </Dialog>
  );
}

// 详情页的备注：从新到旧，作者和时间；成员可以添加，本人或空间管理员可以删除。
import { useState } from 'react';
import { Box, Button, IconButton, Stack, TextField, Tooltip, Typography } from '@mui/material';
import { Trash2 } from 'lucide-react';
import { useAddNote, useDeleteNote, useNotes } from '../../api/documents';
import { useMe } from '../../api/session';
import { useRole } from '../../api/workspaces';
import { SW } from '../../components/icons';
import { locale, t } from '../../i18n';
import { useApp } from '../../store/app-store';
import { relativeTime } from './time';

export function Notes({ docId }: { docId: number }) {
  const app = useApp();
  const role = useRole();
  const me = useMe().data?.user.id;
  const notes = useNotes(docId);
  const add = useAddNote(docId);
  const del = useDeleteNote(docId);
  const [body, setBody] = useState('');
  const submit = () => add.mutate(body.trim(), { onSuccess: () => setBody(''), onError: (e) => app.toast(e.message) });
  const list = notes.data ?? [];
  return (
    <Box>
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>{t('备注')}{list.length ? ` · ${list.length}` : ''}</Typography>
      {role.canWrite && (
        <>
          <TextField fullWidth multiline minRows={2} placeholder={t('添加备注…')} value={body} onChange={(e) => setBody(e.target.value)}
            slotProps={{ htmlInput: { 'aria-label': t('添加备注'), maxLength: 2000 } }}
            onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && body.trim()) submit(); }} />
          {body.trim() && (
            <Stack direction="row" spacing={1} sx={{ justifyContent: 'flex-end', mt: 1 }}>
              <Button size="small" onClick={() => setBody('')}>{t('取消')}</Button>
              <Button size="small" variant="contained" disabled={add.isPending} onClick={submit}>{t('添加备注')}</Button>
            </Stack>
          )}
        </>
      )}
      <Stack spacing={1.5} sx={{ mt: 1.5 }}>
        {list.map((n) => (
          <Box key={n.id} sx={{ '&:hover .del': { opacity: 1 } }}>
            <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
              <Typography variant="caption" sx={{ color: 'text.secondary', flex: 1 }}>
                {n.authorName || t('已删除的账号')} · <span title={new Date(n.createdAt).toLocaleString(locale())}>{relativeTime(n.createdAt)}</span>
              </Typography>
              {(role.isOwner || (role.canWrite && n.authorId === me)) && (
                <Tooltip title={t('删除这条备注')}>
                  <IconButton className="del" size="small" aria-label={t('删除这条备注')} sx={{ opacity: 0, transition: 'opacity 150ms', '&:focus-visible': { opacity: 1 } }}
                    onClick={() => app.confirm({ title: t('删除这条备注？'), body: t('删除后无法恢复。'), verb: t('删除'), run: () => { app.confirm(null); del.mutate(n.id, { onError: (e) => app.toast(e.message) }); } })}>
                    <Trash2 size={14} strokeWidth={SW} />
                  </IconButton>
                </Tooltip>
              )}
            </Stack>
            <Typography sx={{ fontSize: 14, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{n.body}</Typography>
          </Box>
        ))}
      </Stack>
    </Box>
  );
}

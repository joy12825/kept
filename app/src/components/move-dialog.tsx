// 移到其他空间（v1.3.8）：最常见的是把只想自己看的文档移到个人空间。
// 只能移到自己是成员或管理员的空间；默认选中个人空间。移完在对话框里说明结果，可以直接切过去看。
import { useEffect, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useApp } from '../store/app-store';
import { Alert, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Radio, RadioGroup, Typography } from '@mui/material';
import { useMoveDocuments, type MoveResult } from '../api/documents';
import { useMe } from '../api/session';
import { useSwitchWorkspace, useWorkspaces } from '../api/workspaces';
import { SpaceAvatar } from './icons';
import { t } from '../i18n';

export function MoveDialog({ open, onClose, docs, onMoved }: {
  open: boolean;
  onClose: () => void;
  docs: { id: number; title: string }[];
  /** 移走之后（文档已不在当前空间） */
  onMoved?: () => void;
}) {
  const nav = useNavigate();
  const loc = useLocation();
  const app = useApp();
  const me = useMe().data;
  const spaces = useWorkspaces().data ?? [];
  const switchTo = useSwitchWorkspace();
  const move = useMoveDocuments();
  const targets = spaces.filter((w) => w.id !== me?.workspace.id && (w.role === 'owner' || w.role === 'editor'));
  const [to, setTo] = useState<number | null>(null);
  const [done, setDone] = useState<MoveResult | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    if (!open) return;
    setDone(null); setError('');
    setTo(targets.find((w) => w.kind === 'personal')?.id ?? targets[0]?.id ?? null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, spaces.length]);

  const run = () => to && move.mutate({ ids: docs.map((d) => d.id), workspaceId: to }, {
    onSuccess: (r) => {
      setDone(r);
      // 正打开着的文档已不在这个空间：回到来源列表
      if (docs.some((d) => loc.pathname === `/docs/${d.id}`)) nav(app.s.docNav?.from ?? '/files', { replace: true });
      onMoved?.();
    },
    onError: (e) => setError(e.message),
  });
  const close = () => { if (!move.isPending) onClose(); };
  const go = async () => {
    const w = spaces.find((x) => x.id === done?.workspaceId);
    onClose();
    if (!w) return;
    await switchTo(w);
    nav(docs.length === 1 ? `/docs/${docs[0].id}` : '/files');
  };

  return (
    <Dialog open={open} onClose={close} maxWidth="xs" fullWidth aria-label={t('移到其他空间')}>
      <DialogTitle>{done ? t('已移走') : t('移到其他空间')}</DialogTitle>
      <DialogContent>
        {done ? (
          <>
            <Typography>{done.moved === 1 && docs.length === 1 ? t('“{title}”已移到“{space}”。', { title: docs[0].title, space: done.workspaceName }) : t('已把 {n} 份文档移到“{space}”。', { n: done.moved, space: done.workspaceName })}</Typography>
            {done.created.length > 0 && (
              <Typography variant="body2" sx={{ color: 'text.secondary', mt: 1 }}>{t('在“{space}”里新建了{items}。', { space: done.workspaceName, items: done.created.join('、') })}</Typography>
            )}
            {done.asnCleared.length > 0 && (
              <Typography variant="body2" sx={{ color: 'text.secondary', mt: 1 }}>
                {t('{n} 份文档的原件编号在“{space}”里已被占用，已清空，需要时请重新编号。', { n: done.asnCleared.length, space: done.workspaceName })}
              </Typography>
            )}
          </>
        ) : (
          <>
            <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>
              {docs.length === 1
                ? t('把“{title}”移走后，这个空间的成员就看不到了。文件、备注、历史版本会一起带过去；类型、往来方、家人、标签、字段和存放位置在目标空间按名称对应，没有的会新建；按目标空间的收纳规则重新放入抽屉。这个空间里的分享链接和共享会失效。', { title: docs[0].title })
                : t('把选中的 {n} 份文档移走后，这个空间的成员就看不到了。文件、备注、历史版本会一起带过去；类型、往来方、家人、标签、字段和存放位置在目标空间按名称对应，没有的会新建；按目标空间的收纳规则重新放入抽屉。这个空间里的分享链接和共享会失效。', { n: docs.length })}
            </Typography>
            {targets.length === 0 ? (
              <Typography variant="body2">{t('你没有其他可以移入的空间。')}</Typography>
            ) : (
              <RadioGroup value={to ?? ''} onChange={(e) => setTo(Number(e.target.value))} aria-label={t('目标空间')}>
                {targets.map((w) => (
                  <FormControlLabel key={w.id} value={w.id} control={<Radio />}
                    label={<Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}><SpaceAvatar name={w.name} color={w.color} size={24} />{w.name}{w.kind === 'personal' && <Typography component="span" variant="caption" sx={{ color: 'text.secondary' }}>{t('只有你能看到')}</Typography>}</Box>} />
                ))}
              </RadioGroup>
            )}
            {error && <Alert severity="error" sx={{ mt: 1.5 }}>{error}</Alert>}
          </>
        )}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        {done ? (
          <>
            <Button onClick={onClose}>{t('完成')}</Button>
            <Button variant="contained" onClick={() => void go()}>{t('去看看')}</Button>
          </>
        ) : (
          <>
            <Button onClick={close}>{t('取消')}</Button>
            <Button variant="contained" disabled={!to || move.isPending} onClick={run}>{t('移过去')}</Button>
          </>
        )}
      </DialogActions>
    </Dialog>
  );
}

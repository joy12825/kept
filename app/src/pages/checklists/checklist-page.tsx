// 核对一份办事清单（v1.6.2）：每一项找到的最新文档，齐了、还差几份、没有；到期日已过的标出来。
// 找到的文档可以一起生成分享包、打包下载；缺的材料直接在这里上传或从已有文档里选（v1.6.7）。
import { useState } from 'react';
import { Link as RouterLink, useNavigate, useParams } from 'react-router-dom';
import { Alert, Box, Button, Card, Chip, Divider, LinearProgress, Link, Skeleton, Stack, ToggleButton, ToggleButtonGroup, Typography } from '@mui/material';
import { ArrowLeft, Download, Pencil, Share2, Trash2 } from 'lucide-react';
import { useChecklistCheck, useDeleteChecklist } from '../../api/checklists';
import { downloadUrl, startDownload } from '../../api/documents';
import { useRole } from '../../api/workspaces';
import { useApp } from '../../store/app-store';
import { ErrorState } from '../../components/common';
import { SW } from '../../components/icons';
import { ShareBundleDialog, eeBuild } from '@ee';
import { ChecklistDialog } from './checklist-dialog';
import { ChecklistItemCard } from './checklist-item';
import { useUnlocked } from '../../api/sensitive';
import { VerifyDialog } from '../../components/verify-dialog';
import { t } from '../../i18n';


export default function ChecklistPage() {
  const id = Number(useParams().id);
  const check = useChecklistCheck(id);
  const role = useRole();
  const app = useApp();
  const nav = useNavigate();
  const del = useDeleteChecklist();
  const [editing, setEditing] = useState(false);
  const [sharing, setSharing] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const [onlyLacking, setOnlyLacking] = useState(false);
  const unlocked = useUnlocked();

  if (check.isPending) return <Box sx={{ px: 3, pt: 2 }}><Skeleton variant="rounded" height={240} /></Box>;
  if (check.isError) return check.error.status === 404
    ? <Box sx={{ px: 3, pt: 2 }}><Alert severity="info">{t('这份清单不存在或已删除。')}<Link component={RouterLink} to="/checklists">{t('回到办事清单')}</Link></Alert></Box>
    : <ErrorState onRetry={() => void check.refetch()} />;
  const { checklist: c, items } = check.data;
  const docs = items.flatMap((it) => it.documents);
  const unique = [...new Map(docs.map((d) => [d.id, d])).values()];
  // 敏感文档（v1.6.4）不进分享包；下载前要确认过密码
  const shareable = unique.filter((d) => !d.sensitive);
  const sensitiveCount = unique.length - shareable.length;
  const downloadAll = () => {
    const url = downloadUrl({ ids: unique.map((d) => d.id) }, 'archive');
    if (sensitiveCount && !unlocked) setVerifying(true); else startDownload(url);
  };
  const ok = items.filter((it) => it.status === 'ok').length;
  const lacking = items.length - ok;
  const expired = items.reduce((n, it) => n + it.expiredIds.length, 0);
  const openDoc = (docId: number) => { app.set({ docNav: { ids: unique.map((d) => d.id), from: `/checklists/${id}`, label: c.name } }); nav(`/docs/${docId}`); };
  const remove = () => app.confirm({
    title: t('删除清单“{name}”？', { name: c.name }), body: t('只删除清单，文档不受影响。'), verb: t('删除'),
    run: () => del.mutate(id, { onSuccess: () => { app.confirm(null); app.toast(t('已删除清单')); nav('/checklists'); }, onError: (e) => { app.confirm(null); app.toast(e.message); } }),
  });

  const summary = (
    <Card variant="outlined" sx={{ p: 2.5, position: { md: 'sticky' }, top: { md: 80 } }}>
      <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('材料')}</Typography>
      <Typography sx={{ fontSize: 32, lineHeight: '40px', fontWeight: 500, fontVariantNumeric: 'tabular-nums', color: lacking ? 'text.primary' : 'success.main' }}>
        {ok}<Box component="span" sx={{ fontSize: 18, color: 'text.secondary' }}>{t(' / {n} 项齐了', { n: items.length })}</Box>
      </Typography>
      <LinearProgress variant="determinate" value={items.length ? (ok / items.length) * 100 : 0} color={lacking ? 'primary' : 'success'}
        aria-label={t('齐了 {ok} 项，共 {total} 项', { ok, total: items.length })} sx={{ height: 8, borderRadius: 4, my: 1.5 }} />
      <Typography variant="body2" sx={{ color: lacking ? 'warning.main' : 'success.main' }}>
        {lacking ? t('{total} 项材料里，{ok} 项齐了，{lacking} 项还缺。', { total: items.length, ok, lacking }) : t('{n} 项材料都齐了。', { n: items.length })}
      </Typography>
      {expired > 0 && <Typography variant="body2" sx={{ color: 'warning.main', mt: 0.5 }}>{t('有 {n} 份已经过了到期日，记得换新的。', { n: expired })}</Typography>}
      {lacking > 0 && (
        <Box sx={{ mt: 1.5 }}>
          <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('还缺')}</Typography>
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5, mt: 0.5 }}>
            {items.filter((it) => it.status !== 'ok').map((it) => (
              <Chip key={it.id} size="small" variant="outlined" color={it.status === 'partial' ? 'warning' : 'default'} label={it.label}
                onClick={() => document.getElementById(`item-${it.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })} />
            ))}
          </Box>
        </Box>
      )}
      {c.note && <Typography variant="body2" sx={{ color: 'text.secondary', mt: 1.5, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{c.note}</Typography>}
      <Divider sx={{ my: 2 }} />
      <Stack spacing={1}>
        {eeBuild && <Button variant="contained" startIcon={<Share2 size={16} strokeWidth={SW} />} disabled={!shareable.length || !role.canWrite} onClick={() => setSharing(true)}>{t('生成分享包')}</Button>}
        {sensitiveCount > 0 && <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('其中 {n} 份是敏感文档，不会放进分享包。', { n: sensitiveCount })}</Typography>}
        <Button variant="outlined" startIcon={<Download size={16} strokeWidth={SW} />} disabled={!unique.length} onClick={downloadAll}>{t('下载全部（{n} 份）', { n: unique.length })}</Button>
        {role.canWrite && (
          <Stack direction="row" spacing={1}>
            <Button sx={{ flex: 1 }} startIcon={<Pencil size={16} strokeWidth={SW} />} onClick={() => setEditing(true)}>{t('编辑')}</Button>
            <Button sx={{ flex: 1 }} color="error" startIcon={<Trash2 size={16} strokeWidth={SW} />} onClick={remove}>{t('删除')}</Button>
          </Stack>
        )}
      </Stack>
    </Card>
  );

  return (
    <Box sx={{ px: 3, pt: 1, pb: 8 }}>
      <Button size="small" startIcon={<ArrowLeft size={16} strokeWidth={SW} />} onClick={() => nav('/checklists')} sx={{ mb: 1 }}>{t('办事清单')}</Button>
      <Typography variant="h4" sx={{ overflowWrap: 'anywhere', mb: 2 }}>{c.name}</Typography>
      {/* 宽屏两栏：左边材料，右边概况和操作；窄屏时概况在上 */}
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'minmax(0,1fr) 320px' }, gap: 3, alignItems: 'start' }}>
        <Box sx={{ order: { xs: 2, md: 1 }, minWidth: 0 }}>
          {lacking > 0 && ok > 0 && (
            <ToggleButtonGroup size="small" exclusive value={onlyLacking ? 'lacking' : 'all'} onChange={(_, v) => v && setOnlyLacking(v === 'lacking')} aria-label={t('显示哪些材料')} sx={{ mb: 1.5 }}>
              <ToggleButton value="all">{t('全部 {n}', { n: items.length })}</ToggleButton>
              <ToggleButton value="lacking">{t('还缺 {n}', { n: lacking })}</ToggleButton>
            </ToggleButtonGroup>
          )}
          <Box component="ol" aria-label={t('材料')} sx={{ listStyle: 'none', p: 0, m: 0, display: 'grid', gap: 1.5 }}>
            {items.filter((it) => !onlyLacking || it.status !== 'ok').map((it) => (
              <ChecklistItemCard key={it.id} checklistId={id} it={it} canWrite={role.canWrite} onOpen={openDoc} />
            ))}
          </Box>
        </Box>
        <Box sx={{ order: { xs: 1, md: 2 } }}>{summary}</Box>
      </Box>
      {editing && <ChecklistDialog open checklist={c} onClose={() => setEditing(false)} />}
      {sharing && <ShareBundleDialog open docs={shareable.map((d) => ({ id: d.id, title: d.title }))} onClose={() => setSharing(false)} />}
      <VerifyDialog open={verifying} onClose={() => setVerifying(false)} onDone={() => startDownload(downloadUrl({ ids: unique.map((d) => d.id) }, 'archive'))} />
    </Box>
  );
}

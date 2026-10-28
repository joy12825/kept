// 详情页“版本”（阶段 19）：各版本的列表、下载、设为当前版本、删除，上传新版本；加密原件的“移除密码”对话框。
import { useState } from 'react';
import { Alert, Box, Button, Chip, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, Link, Skeleton, Stack, TextField, Tooltip, Typography } from '@mui/material';
import { Columns2, Download, FilePlus2, Trash2, Upload } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { useDeleteVersion, useDocumentVersions, useRemovePassword, useSetCurrentVersion, useAddVersion, useAddVersionFromDocument, versionFileUrl, type DocumentVersion } from '../../api/versions';
import { DocumentLinkInput } from '../../components/doc-fields';
import { startDownload } from '../../api/documents';
import { useApp } from '../../store/app-store';
import { SW } from '../../components/icons';
import { t } from '../../i18n';
import { bytes } from '../settings/system';
import { dateTime, relativeTime } from './time';

const REPROCESSING = t('已切换版本，正在按新版本重新识别文字和生成预览');

export function Versions({ docId, readOnly }: { docId: number; readOnly: boolean }) {
  const app = useApp();
  const list = useDocumentVersions(docId);
  const setCurrent = useSetCurrentVersion(docId);
  const del = useDeleteVersion(docId);
  const [adding, setAdding] = useState(false);
  const [folding, setFolding] = useState(false); // 从已有文档添加（v1.5.8）
  const nav = useNavigate();

  const makeCurrent = (v: DocumentVersion) => app.confirm({
    title: t('把第 {n} 版设为当前版本？', { n: v.number }),
    body: t('文档的标题、类型、标签等不变；识别文字、预览和页数会按这个版本重新生成，旋转、删除页面的编辑会清空。现在的版本会保留在列表里。'),
    verb: t('设为当前版本'),
    run: () => setCurrent.mutate(v.id, { onSuccess: () => { app.confirm(null); app.toast(REPROCESSING); }, onError: (e) => { app.confirm(null); app.toast(e.message); } }),
  });
  const remove = (v: DocumentVersion) => app.confirm({
    title: t('删除第 {n} 版？', { n: v.number }), body: t('这个版本的文件会被删除，不能恢复。'), verb: t('删除'),
    run: () => del.mutate(v.id, { onSuccess: () => { app.confirm(null); app.toast(t('已删除第 {n} 版', { n: v.number })); }, onError: (e) => { app.confirm(null); app.toast(e.message); } }),
  });

  if (list.isPending) return <Skeleton variant="rounded" height={48} />;
  if (list.error) return <Typography variant="body2" sx={{ color: 'error.main' }}>{list.error.message}</Typography>;
  return (
    <Box>
      {list.data.map((v, i) => (
        <Stack key={`${v.id}-${v.number}`} direction="row" spacing={1} sx={{ alignItems: 'center', py: 1, borderTop: i ? 1 : 0, borderColor: 'divider' }}>
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
              <Typography variant="body2" sx={{ fontWeight: 500 }}>{t('第 {n} 版', { n: v.number })}</Typography>
              {v.current && <Chip size="small" color="primary" label={t('当前')} />}
              {v.encrypted && <Chip size="small" label={t('有密码')} />}
            </Stack>
            {v.label && <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>{v.label}</Typography>}
            <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', overflowWrap: 'anywhere' }}>
              {v.filename} · {bytes(v.size)} · {v.creatorName ? `${v.creatorName} · ` : ''}<Tooltip title={dateTime(v.createdAt)}><span>{relativeTime(v.createdAt)}</span></Tooltip>
            </Typography>
            {!v.current && !readOnly && !v.encrypted && (
              <Link component="button" variant="caption" onClick={() => makeCurrent(v)} disabled={setCurrent.isPending}>{t('设为当前版本')}</Link>
            )}
          </Box>
          <Tooltip title={t('下载这个版本')}><IconButton size="small" aria-label={t('下载第 {n} 版', { n: v.number })} onClick={() => startDownload(versionFileUrl(docId, v.id))}><Download size={16} strokeWidth={SW} /></IconButton></Tooltip>
          {!v.current && !readOnly && (
            <Tooltip title={t('删除这个版本')}><IconButton size="small" aria-label={t('删除第 {n} 版', { n: v.number })} onClick={() => remove(v)}><Trash2 size={16} strokeWidth={SW} /></IconButton></Tooltip>
          )}
        </Stack>
      ))}
      {(!readOnly || list.data.length > 1) && (
        <Stack direction="row" spacing={1} sx={{ mt: 1, flexWrap: 'wrap' }}>
          {list.data.length > 1 && <Button size="small" startIcon={<Columns2 size={14} strokeWidth={SW} />} onClick={() => nav(`/docs/${docId}/compare`)}>{t('对比版本')}</Button>}
          {!readOnly && <Button size="small" startIcon={<Upload size={14} strokeWidth={SW} />} onClick={() => setAdding(true)}>{t('上传新版本')}</Button>}
          {!readOnly && <Button size="small" startIcon={<FilePlus2 size={14} strokeWidth={SW} />} onClick={() => setFolding(true)}>{t('从已有文档添加')}</Button>}
        </Stack>
      )}
      {adding && <AddVersionDialog docId={docId} onClose={() => setAdding(false)} />}
      {folding && <FoldInDialog docId={docId} onClose={() => setFolding(false)} />}
    </Box>
  );
}

function AddVersionDialog({ docId, onClose }: { docId: number; onClose: () => void }) {
  const app = useApp();
  const add = useAddVersion(docId);
  const [file, setFile] = useState<File | null>(null);
  const [label, setLabel] = useState('');
  const submit = () => file && add.mutate({ file, label }, { onSuccess: () => { app.toast(REPROCESSING); onClose(); } });
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="add-version-title">
      <DialogTitle id="add-version-title">{t('上传新版本')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 2 }}>{t('新文件会成为当前版本，文档的标题、类型、标签、备注等不变。现在的版本会保留，可以随时换回来。')}</Typography>
        {add.error && <Alert severity="error" sx={{ mb: 2 }}>{add.error.message}</Alert>}
        <Stack spacing={2}>
          <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center' }}>
            <Button variant="outlined" component="label">
              {t('选择文件')}
              <input hidden type="file" aria-label={t('新版本的文件')} onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
            </Button>
            <Typography variant="body2" noWrap sx={{ color: file ? 'text.primary' : 'text.secondary', minWidth: 0 }}>{file ? `${file.name} · ${bytes(file.size)}` : t('还没有选择')}</Typography>
          </Stack>
          <TextField label={t('说明')} value={label} onChange={(e) => setLabel(e.target.value)} placeholder={t('比如：第二稿、对方盖章版')} slotProps={{ inputLabel: { shrink: true }, htmlInput: { maxLength: 200 } }} />
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!file || add.isPending} onClick={submit}>{add.isPending ? t('上传中…') : t('上传')}</Button>
      </DialogActions>
    </Dialog>
  );
}

/** 把另一份文档并为这份的新版本（v1.5.8，参照 paperless-ngx 3）：续签的合同、更新后的保单已经单独收进来时，归到一起。 */
function FoldInDialog({ docId, onClose }: { docId: number; onClose: () => void }) {
  const app = useApp();
  const fold = useAddVersionFromDocument(docId);
  const [from, setFrom] = useState<number | undefined>();
  const [label, setLabel] = useState('');
  const submit = () => from && fold.mutate({ documentId: from, label }, { onSuccess: () => { app.toast(t('已并为新版本，正在重新识别文字和生成预览')); onClose(); } });
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="fold-in-title">
      <DialogTitle id="fold-in-title">{t('从已有文档添加版本')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 2 }}>
          {t('选中文档的原件会成为这份的当前版本；它的标签、备注和这份没有的字段一并过来，链接到它的文档改为链接到这份，然后它不再单独存在。现在的版本会保留。')}
        </Typography>
        {fold.error && <Alert severity="error" sx={{ mb: 2 }}>{fold.error.message}</Alert>}
        <Stack spacing={2} sx={{ pt: 0.5 }}>
          <DocumentLinkInput value={from} selfId={docId} label={t('要并入的文档')} fieldLabel={t('要并入的文档')} onChange={(v) => setFrom(typeof v === 'number' ? v : undefined)} />
          <TextField label={t('说明')} value={label} onChange={(e) => setLabel(e.target.value)} placeholder={t('不填时为“取自《标题》”')} slotProps={{ inputLabel: { shrink: true }, htmlInput: { maxLength: 200 } }} />
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!from || fold.isPending} onClick={submit}>{t('并为新版本')}</Button>
      </DialogActions>
    </Dialog>
  );
}

/** 加密的原件：输入密码，生成没有密码的新版本。 */
export function RemovePasswordDialog({ docId, open, onClose }: { docId: number; open: boolean; onClose: () => void }) {
  const app = useApp();
  const remove = useRemovePassword(docId);
  const [password, setPassword] = useState('');
  const submit = () => remove.mutate(password, { onSuccess: () => { app.toast(t('已生成没有密码的新版本，正在重新处理')); setPassword(''); onClose(); } });
  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="remove-pw-title">
      <DialogTitle id="remove-pw-title">{t('移除密码')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 2 }}>{t('用密码打开原件，另存为一个没有密码的新版本。有密码的原件会作为旧版本保留；密码只用这一次，不会保存。')}</Typography>
        {remove.error && <Alert severity="error" sx={{ mb: 2 }}>{remove.error.message}</Alert>}
        <TextField autoFocus fullWidth type="password" label={t('PDF 的密码')} value={password} onChange={(e) => setPassword(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter' && password) submit(); }} />
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!password || remove.isPending} onClick={submit}>{t('移除密码')}</Button>
      </DialogActions>
    </Dialog>
  );
}

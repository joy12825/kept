// 上传对话框、全局拖放与任务托盘：真实上传，进度来自入库任务（SSE 的 job.updated）
import { useEffect, useRef, useState, type DragEvent } from 'react';
import { useMatch, useNavigate } from 'react-router-dom';
import {
  Box, Button, Checkbox, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, IconButton, LinearProgress, MenuItem, Paper, Stack, TextField, Typography,
} from '@mui/material';
import { ChevronDown, CircleCheck, FileText, TriangleAlert, Upload as UploadIcon, X } from 'lucide-react';
import { MAX_UPLOAD_SIZE, useJobs, useResolveSameName, useRetryWithPassword, useUploader, type Job } from '../api/documents';
import { useDrawers } from '../api/drawers';
import { useTaxonomy } from '../api/taxonomy';
import { useRole } from '../api/workspaces';
import { useApp, type Upload } from '../store/app-store';
import { SW } from './icons';
import { RetryUpload } from './task-center';
import { EMPTY_PRESETS, presetsForUpload, presetsInvalid, UploadPresets, type MorePresets } from './upload-presets';
import { t, tc } from '../i18n';

export const ACCEPT = '.pdf,.jpg,.jpeg,.png,.tif,.tiff,.heic,.heif,.webp,.gif,.bmp,.txt,.md,application/pdf,image/jpeg,image/png,image/tiff,image/heic,image/heif,image/webp,image/gif,image/bmp,text/plain,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.odt,.ods,.odp,.rtf,.epub,application/epub+zip,.eml,message/rfc822,.msg,application/vnd.ms-outlook,.json,.csv,.tsv,.yaml,.yml,.toml,.ini,.log,.py,.go,.js,.ts,.sh,.sql,.mp3,.m4a,.aac,.wav,.ogg,.flac,.mp4,.mov,.webm,audio/*,video/*,.ofd,.zip,application/zip';

/** 正在看的抽屉：在抽屉页上传的文件默认放进这个抽屉（v1.7.13）。 */
function useRouteDrawer() {
  const m = useMatch('/drawers/:id');
  const id = Number(m?.params.id);
  return id > 0 ? id : undefined;
}

export function UploadDialog() {
  const app = useApp();
  const upload = useUploader();
  const types = useTaxonomy('document-types');
  const corrs = useTaxonomy('correspondents');
  const drawers = useDrawers();
  const routeDrawer = useRouteDrawer();
  const [files, setFiles] = useState<File[]>([]);
  const [typeId, setTypeId] = useState(0);
  const [corrId, setCorrId] = useState(0);
  const [drawerId, setDrawerId] = useState(0);
  const [more, setMore] = useState<MorePresets>(EMPTY_PRESETS);
  const [showMore, setShowMore] = useState(false);
  const [over, setOver] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const open = app.s.uploadOpen;
  useEffect(() => { if (open) { setFiles([]); setTypeId(0); setCorrId(0); setDrawerId(routeDrawer ?? 0); setMore(EMPTY_PRESETS); setShowMore(false); } }, [open, routeDrawer]);
  const close = () => app.set({ uploadOpen: false });
  // FileList 是“活的”：选择框随后清空 value 时它也会变空，所以要先复制出来
  const add = (list: FileList | null) => { const picked = Array.from(list ?? []); if (picked.length) setFiles((f) => [...f, ...picked]); };
  const onDrop = (e: DragEvent) => { e.preventDefault(); setOver(false); add(e.dataTransfer.files); };

  return (
    <Dialog open={open} onClose={close} fullWidth maxWidth="sm" aria-labelledby="upload-dialog-title">
      <DialogTitle id="upload-dialog-title">{t('上传文档')}</DialogTitle>
      <DialogContent>
        <Stack
          role="button" tabIndex={0} aria-label={t('选择文件')} spacing={0.75}
          onClick={() => input.current?.click()} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') input.current?.click(); }}
          onDragOver={(e) => { e.preventDefault(); setOver(true); }} onDragLeave={() => setOver(false)} onDrop={onDrop}
          sx={{
            alignItems: 'center', justifyContent: 'center', height: 160, border: '1.5px dashed', borderColor: over ? 'primary.main' : 'divider',
            borderRadius: 3, bgcolor: over ? 'surface.secondaryContainer' : 'surface.containerLow', color: 'text.secondary', mb: 2.5, cursor: 'pointer',
          }}>
          <UploadIcon size={22} strokeWidth={SW} />
          <Typography sx={{ color: 'text.primary' }}>{t('拖入文件或点击选择')}</Typography>
          <Typography variant="caption">{t('支持 PDF、图片（JPG、PNG、TIFF、HEIC、WebP、GIF、BMP）、文本和代码文件、EPUB 电子书、音频和视频、OFD 电子发票、zip 压缩包，以及 Office 文档和邮件文件 .eml（管理员配好转换服务后会生成可搜索版），单个文件不超过 {mb} MB', { mb: MAX_UPLOAD_SIZE >> 20 })}</Typography>
          <input ref={input} hidden multiple type="file" accept={ACCEPT} onChange={(e) => { add(e.target.files); e.target.value = ''; }} aria-label={t('上传文件')} />
        </Stack>
        {files.length > 0 && (
          <Stack spacing={0.5} sx={{ mb: 2.5 }}>
            {files.map((f, i) => (
              <Stack key={`${f.name}-${i}`} direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                <FileText size={16} strokeWidth={SW} />
                <Typography noWrap sx={{ flex: 1, fontSize: 13 }}>{f.name}</Typography>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>{formatSize(f.size)}</Typography>
                <IconButton size="small" aria-label={t('移除 {name}', { name: f.name })} onClick={() => setFiles((x) => x.filter((_, j) => j !== i))}><X size={14} strokeWidth={SW} /></IconButton>
              </Stack>
            ))}
          </Stack>
        )}
        <Typography variant="h6" sx={{ mb: 1.5 }}>{t('预设分类（可选）')}</Typography>
        <Stack spacing={1.5}>
          <TextField select label={t('类型')} value={typeId} onChange={(e) => setTypeId(Number(e.target.value))}>
            <MenuItem value={0}>{t('不预设')}</MenuItem>
            {types.data?.map((ty) => <MenuItem key={ty.id} value={ty.id}>{ty.name}</MenuItem>)}
          </TextField>
          <TextField select label={t('往来方')} value={corrId} onChange={(e) => setCorrId(Number(e.target.value))}>
            <MenuItem value={0}>{t('不预设')}</MenuItem>
            {corrs.data?.map((c) => <MenuItem key={c.id} value={c.id}>{c.name}</MenuItem>)}
          </TextField>
          <TextField select label={t('抽屉')} value={drawers.data?.items.some((d) => d.id === drawerId) ? drawerId : 0} onChange={(e) => setDrawerId(Number(e.target.value))}
            helperText={t('放进抽屉后，和抽屉里已有文档同名的文件可以作为它的新版本')}>
            <MenuItem value={0}>{t('按收纳规则')}</MenuItem>
            {drawers.data?.items.map((d) => <MenuItem key={d.id} value={d.id}>{d.name}</MenuItem>)}
          </TextField>
          {showMore
            ? <UploadPresets value={more} onChange={setMore} single={files.length <= 1} />
            : <Button size="small" endIcon={<ChevronDown size={16} strokeWidth={SW} />} onClick={() => setShowMore(true)} sx={{ alignSelf: 'flex-start' }}>
                {t('更多预设：家人、标签、日期、原件编号、字段')}
              </Button>}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={close}>{t('取消')}</Button>
        <Button variant="contained" disabled={!files.length || presetsInvalid(more, files.length === 1)}
          onClick={() => upload(files, { documentTypeId: typeId || undefined, correspondentId: corrId || undefined, drawerId: drawerId || undefined, ...presetsForUpload(more, files.length === 1) })}>
          {files.length ? t('上传 {n} 个文件', { n: files.length }) : t('上传')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

/** 整个窗口都可以拖入文件，松开即上传（只看的成员不行）。 */
export function GlobalDrop() {
  const upload = useUploader();
  const role = useRole();
  const drawerId = useRouteDrawer();
  const [active, setActive] = useState(false);
  useEffect(() => {
    if (!role.canWrite) return;
    let depth = 0;
    const hasFiles = (e: globalThis.DragEvent) => Array.from(e.dataTransfer?.types ?? []).includes('Files');
    const enter = (e: globalThis.DragEvent) => { if (hasFiles(e)) { depth++; setActive(true); } };
    const leave = () => { depth = Math.max(0, depth - 1); if (!depth) setActive(false); };
    const overH = (e: globalThis.DragEvent) => { if (hasFiles(e)) e.preventDefault(); };
    const drop = (e: globalThis.DragEvent) => {
      if (!hasFiles(e)) return;
      // 对话框里的拖放区自己处理
      if ((e.target as HTMLElement).closest?.('[role="dialog"]')) { depth = 0; setActive(false); return; }
      e.preventDefault(); depth = 0; setActive(false);
      if (e.dataTransfer?.files.length) upload(Array.from(e.dataTransfer.files), { drawerId });
    };
    window.addEventListener('dragenter', enter); window.addEventListener('dragleave', leave);
    window.addEventListener('dragover', overH); window.addEventListener('drop', drop);
    return () => {
      window.removeEventListener('dragenter', enter); window.removeEventListener('dragleave', leave);
      window.removeEventListener('dragover', overH); window.removeEventListener('drop', drop);
    };
  }, [upload, role.canWrite, drawerId]);
  if (!active) return null;
  return (
    <Box sx={{ position: 'fixed', inset: 12, zIndex: 1400, pointerEvents: 'none', border: '2px dashed', borderColor: 'primary.main', borderRadius: 4,
      bgcolor: 'rgba(37, 99, 235, 0.06)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <Paper sx={{ px: 3, py: 2, display: 'flex', gap: 1.5, alignItems: 'center' }}>
        <UploadIcon size={20} strokeWidth={SW} />
        <Typography>{drawerId ? t('松开即可上传到这个抽屉') : t('松开即可上传')}</Typography>
      </Paper>
    </Box>
  );
}

const STAGE: Record<string, string> = {
  queued: t('排队中'), inspect: t('读取中'), ocr: tc('阶段', '识别文字'), thumbnail: t('生成缩略图'), saving: t('保存中'), done: t('已进入待整理'),
};

type Row = { key: string; name: string; state: 'uploading' | 'running' | 'done' | 'failed'; pct: number; label: string; error?: string; documentId?: number; needsPassword?: boolean; version?: boolean; jobId?: number; retryable?: boolean };

function rowOf(u: Upload, job: Job | undefined): Row {
  const base = { key: u.key, name: u.name };
  if (u.error) return { ...base, state: 'failed', pct: 100, label: t('失败'), error: u.error, documentId: u.documentId, needsPassword: u.needsPassword };
  if (!u.jobId) return { ...base, state: 'uploading', pct: Math.round(u.pct * 0.1), label: t('上传中 {pct}%', { pct: u.pct }) };
  if (!job) return { ...base, state: 'running', pct: 10, label: t('排队中') };
  if (job.status === 'failed') return { ...base, state: 'failed', pct: 100, label: t('失败'), error: job.error ?? t('处理失败'), documentId: job.documentId, jobId: job.id, retryable: job.retryable };
  if (job.status === 'succeeded') return { ...base, state: 'done', pct: 100, label: u.versionOf ? t('已作为新版本') : STAGE.done, documentId: u.versionOf ?? job.documentId, version: !!u.versionOf };
  return { ...base, state: 'running', pct: Math.max(10, job.progress), label: STAGE[job.stage] ?? t('处理中') };
}

export function TaskTray() {
  const app = useApp(); const { s } = app; const nav = useNavigate();
  const jobs = useJobs();
  const [pwFor, setPwFor] = useState<Row | null>(null);
  if (!s.uploads.length) return null;
  const byId = new Map((jobs.data ?? []).map((j) => [j.id, j]));
  const rows = s.uploads.map((u) => rowOf(u, u.jobId ? byId.get(u.jobId) : undefined));
  const running = rows.some((r) => r.state === 'uploading' || r.state === 'running');
  const failed = rows.filter((r) => r.state === 'failed').length;
  const done = rows.filter((r) => r.state === 'done').length;
  const open = (r: Row) => { if (r.documentId) nav(r.version ? `/docs/${r.documentId}` : `/inbox?doc=${r.documentId}`); };
  return (
    <Paper elevation={2} sx={{ position: 'fixed', right: 24, bottom: 24, zIndex: 1250, width: 340, maxWidth: 'calc(100vw - 32px)', borderRadius: 4, overflow: 'hidden', bgcolor: 'surface.container' }}>
      <Box sx={{ display: 'flex', alignItems: 'center', height: 48, pl: 2, pr: 1, borderBottom: s.trayOpen ? 1 : 0, borderColor: 'divider' }}>
        <Typography variant="h6" sx={{ flex: 1 }}>{running ? t('正在处理 {n} 个文件', { n: rows.length }) : failed ? t('已完成 {n} 份，{failed} 份失败', { n: done, failed }) : t('已完成 {n} 份', { n: done })}</Typography>
        <IconButton size="small" aria-label={s.trayOpen ? tc('面板', '收起') : tc('面板', '展开')} onClick={() => app.set((p) => ({ trayOpen: !p.trayOpen }))}>
          <ChevronDown size={18} strokeWidth={SW} style={{ transform: s.trayOpen ? 'none' : 'rotate(180deg)', transition: 'transform 150ms' }} />
        </IconButton>
        <IconButton size="small" aria-label={t('关闭')} onClick={() => app.set({ uploads: [] })}><X size={18} strokeWidth={SW} /></IconButton>
      </Box>
      {s.trayOpen && (
        <Box sx={{ maxHeight: 320, overflowY: 'auto' }}>
          {rows.map((r) => (
            <Box key={r.key} onClick={() => open(r)} sx={{ px: 2, py: 1.25, cursor: r.documentId ? 'pointer' : 'default', '&:hover': { bgcolor: 'action.hover' } }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.75 }}>
                <Box sx={{ color: r.state === 'failed' ? 'error.main' : r.state === 'done' ? 'success.main' : 'primary.main', display: 'flex' }}>
                  {r.state === 'failed' ? <TriangleAlert size={16} strokeWidth={SW} /> : r.state === 'done' ? <CircleCheck size={16} strokeWidth={SW} /> : <UploadIcon size={16} strokeWidth={SW} />}
                </Box>
                <Typography noWrap sx={{ flex: 1, fontSize: 13 }}>{r.name}</Typography>
                <Typography sx={{ fontSize: 12, color: 'text.secondary', flex: 'none' }}>{r.label}</Typography>
              </Box>
              <LinearProgress variant="determinate" value={r.pct} color={r.state === 'failed' ? 'error' : r.state === 'done' ? 'success' : 'primary'} />
              {r.error && (
                <Typography sx={{ fontSize: 12, color: 'text.secondary', mt: 0.75 }}>
                  {r.documentId ? t('{error}，点击查看已有的文档', { error: r.error }) : r.error}
                </Typography>
              )}
              {r.jobId && r.retryable && <RetryUpload id={r.jobId} retryable />}
              {r.needsPassword && (
                <Button size="small" sx={{ mt: 0.5, ml: -0.75 }} onClick={(e) => { e.stopPropagation(); setPwFor(r); }}>{t('输入密码')}</Button>
              )}
            </Box>
          ))}
        </Box>
      )}
      <PasswordDialog row={pwFor} onClose={() => setPwFor(null)} />
    </Paper>
  );
}

/** 同一抽屉里已有同名文档（v1.7.13）：逐个选择作为新版本还是新文档，默认都作为新版本。 */
export function SameNameDialog() {
  const app = useApp();
  const resolve = useResolveSameName();
  const pending = app.s.sameName;
  const [versions, setVersions] = useState<Set<string>>(new Set());
  useEffect(() => { if (pending) setVersions(new Set(pending.matches.map((m) => m.name))); }, [pending]);
  if (!pending) return null;
  const toggle = (name: string, on: boolean) => setVersions((v) => { const n = new Set(v); if (on) n.add(name); else n.delete(name); return n; });
  const all = versions.size === pending.matches.length;
  return (
    <Dialog open onClose={() => resolve(pending.token, null)} fullWidth maxWidth="sm" aria-labelledby="same-name-title">
      <DialogTitle id="same-name-title">{t('有同名的文档')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>
          {t('勾选的文件作为已有文档的新版本：原来的内容作为旧版本保留，可以在文档的“版本”里查看和恢复。没勾选的作为新文档上传。')}
        </Typography>
        <FormControlLabel sx={{ mb: 0.5 }} control={<Checkbox checked={all} indeterminate={!all && versions.size > 0}
          onChange={(e) => setVersions(e.target.checked ? new Set(pending.matches.map((m) => m.name)) : new Set())} />}
          label={t('全部作为新版本')} />
        <Stack sx={{ pl: 2 }}>
          {pending.matches.map((m) => (
            <FormControlLabel key={m.name} control={<Checkbox checked={versions.has(m.name)} onChange={(e) => toggle(m.name, e.target.checked)} />}
              label={<Box><Typography variant="body2">{m.name}</Typography><Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('作为“{title}”的新版本', { title: m.title })}</Typography></Box>} />
          ))}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={() => resolve(pending.token, null)}>{t('取消')}</Button>
        <Button variant="contained" onClick={() => resolve(pending.token, versions)}>{t('上传')}</Button>
      </DialogActions>
    </Dialog>
  );
}

/** 加密的 PDF：输入密码后用同一个文件重新上传。密码只用于解密，不保存。 */
function PasswordDialog({ row, onClose }: { row: Row | null; onClose: () => void }) {
  const retry = useRetryWithPassword();
  const app = useApp();
  const [pw, setPw] = useState('');
  useEffect(() => { if (row) setPw(''); }, [row]);
  const submit = () => {
    if (!row || !pw) return;
    if (!retry(row.key, pw)) app.toast(t('文件已经不在了，请重新选择文件上传'));
    onClose();
  };
  return (
    <Dialog open={!!row} onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="pdf-password-title">
      <DialogTitle id="pdf-password-title">{t('输入 PDF 密码')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 2 }}>{t('“{name}”有密码保护。密码只用于这一次解密，不会保存；原件仍按原样保留。', { name: row?.name ?? '' })}</Typography>
        <TextField autoFocus fullWidth type="password" label={t('密码')} value={pw} onChange={(e) => setPw(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') submit(); }} />
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!pw} onClick={submit}>{t('上传')}</Button>
      </DialogActions>
    </Dialog>
  );
}

function formatSize(n: number) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

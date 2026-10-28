// 存储与配额（v1.0 阶段 36）：空间成员看本空间的用量；实例管理员在“实例管理 → 存储管理”里看全部空间、设置配额。
// 导出整个空间（v1.4.1）：空间管理员把文档导出为普通文件夹和清单，不装 kept 也能看。
// 存储位置（v1.7.0）：实例管理员看各个存储、测试能否读写，给空间换存储并把已有文档移过去。
// 文件安全（v1.13.9）：实例管理的存储页按“想要什么”组织，大白话做标题，技术名称做副标题（file-safety.tsx）。
import { useEffect, useState } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import { dateTime } from '../detail/time';
import { Alert, Box, Button, Card, Checkbox, Chip, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, InputAdornment, LinearProgress, Link, MenuItem, Radio, RadioGroup, Stack, Table, TableBody, TableCell, TableHead, TableRow, TextField, Typography } from '@mui/material';
import { exportUrl, startDownload } from '../../api/documents';
import { useRole } from '../../api/workspaces';
import { BackupSection, ColdDialog, ExportSection as StorageExportSection, QuotaDialog, eeBuild, useEncryptionSwitch } from '@ee';
import { EncryptionChip, EncryptionSection, encryptable } from './storage-encryption';
import { StorageFormDialog } from './storage-form';
import { quotaShare, useSetKeepVersions, useAdminSetCold, useAdminStorages, useDeleteStorage, useSetStorageCold, useSetStorageLocation, useStorageLocation, type StorageChoice, useAdminWorkspaces, useSetQuota, useSetWorkspaceStorage, useStorage, useTestStorage, type AdminStorage, type AdminWorkspace, type QuotaMode, type StorageTest, type StorageUsage, type WorkspaceStorage } from '../../api/storage';
import { ErrorState, LoadingState } from '../../components/common';
import { t, tc } from '../../i18n';
import { useApp } from '../../store/app-store';
import { bytes } from './system';
import { GoalCards, LocalBackupSection, SAFETY_ANCHOR, SectionTitle } from './file-safety';

function quotaText(u: StorageUsage, defaultQuota?: number) {
  if (u.quota === 0) return u.quotaDefault ? t('不限（跟随默认）') : tc('配额', '不限');
  return u.quotaDefault && defaultQuota !== undefined ? t('{size}（默认）', { size: bytes(u.quota) }) : bytes(u.quota);
}

function UsageBar({ u }: { u: StorageUsage }) {
  const share = quotaShare(u);
  if (share === null) return null;
  return <LinearProgress variant="determinate" value={Math.min(100, share * 100)} color={share >= 0.9 ? 'warning' : 'primary'} aria-label={t('已用配额')} sx={{ height: 8, borderRadius: 4 }} />;
}

/** 设置 → 存储：本空间的用量。 */
export function StoragePanel() {
  const st = useStorage();
  if (st.isPending) return <LoadingState />;
  if (st.isError) return <ErrorState onRetry={() => st.refetch()} />;
  const u = st.data;
  const share = quotaShare(u);
  const rows: [string, number, string][] = [
    [t('原件'), u.originals, t('{n} 份文档', { n: u.documents })],
    [t('可搜索版'), u.searchable, t('识别文字后生成的副本，和页面编辑前的底稿')],
    [t('缩略图与封面'), u.thumbnails, ''],
    [t('历史版本'), u.versions, t('上传新版本后保留的旧版')],
    [t('回收站'), u.trash, t('删除的文档 30 天后才清除；清空回收站可以马上腾出空间')],
  ];
  return (
    <>
      <Typography variant="h4" sx={{ mb: 3 }}>{t('存储')}</Typography>
      <Card sx={{ p: 2.5, mb: 2 }}>
        <Typography sx={{ fontSize: 28, fontWeight: 500 }}>{bytes(u.total)}</Typography>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: share === null ? 0 : 1.5 }}>
          {share === null ? t('已用，这个空间没有配额') : t('已用 {percent}%，配额 {quota}', { percent: Math.round(share * 100), quota: bytes(u.quota) })}
        </Typography>
        <UsageBar u={u} />
      </Card>
      {share !== null && share >= 1 && <Alert severity="error" sx={{ mb: 2 }}>{t('空间已满，暂时不能上传新文档。清空回收站或删除不需要的文档，或请管理员提高配额。')}</Alert>}
      {share !== null && share >= 0.9 && share < 1 && <Alert severity="warning" sx={{ mb: 2 }}>{t('空间快满了。清空回收站或删除不需要的文档，或请管理员提高配额。')}</Alert>}
      <Card>
        {rows.map(([label, n, note], i) => (
          <Stack key={label} direction="row" spacing={2} sx={{ alignItems: 'center', p: 2, borderTop: i ? 1 : 0, borderColor: 'divider' }}>
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Typography>{label}</Typography>
              {note && <Typography variant="caption" sx={{ color: 'text.secondary' }}>{note}</Typography>}
            </Box>
            <Typography sx={{ fontVariantNumeric: 'tabular-nums' }}>{bytes(n)}</Typography>
          </Stack>
        ))}
      </Card>
      <PrivateUsage />
      <VersionsSection keep={u.keepVersions ?? 0} />
      <LocationSection />
      <ExportSection />
    </>
  );
}

/** 旧版本保留几个（v1.7.13）：空间管理员设置，其他成员只看。 */
function VersionsSection({ keep }: { keep: number }) {
  const app = useApp();
  const role = useRole();
  const set = useSetKeepVersions();
  const [limit, setLimit] = useState(keep > 0);
  const [n, setN] = useState(String(keep || 5));
  useEffect(() => { setLimit(keep > 0); setN(String(keep || 5)); }, [keep]);
  const num = Number(n);
  const bad = limit && (!Number.isInteger(num) || num < 1 || num > 1000);
  const changed = limit ? !bad && num !== keep : keep !== 0;
  return (
    <Box component="section" aria-label={t('旧版本')} sx={{ mt: 4 }}>
      <Typography variant="h5" sx={{ mb: 0.5 }}>{t('旧版本')}</Typography>
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>
        {t('上传新版本后，原来的文件作为旧版本保留，可以随时查看和恢复。限制数量后，超出的最早的旧版本会在文档下次有新版本时删除。')}
      </Typography>
      {!role.isOwner ? (
        <Typography variant="body2">{keep > 0 ? t('每份文档最多保留 {n} 个旧版本', { n: keep }) : t('全部保留')}</Typography>
      ) : (
        <Card sx={{ p: 2 }}>
          <RadioGroup value={limit ? 'limit' : 'all'} onChange={(e) => setLimit(e.target.value === 'limit')} aria-label={t('旧版本保留几个')}>
            <FormControlLabel value="all" control={<Radio />} label={t('全部保留')} />
            <FormControlLabel value="limit" control={<Radio />} label={t('限制数量')} />
          </RadioGroup>
          {limit && (
            <TextField sx={{ mt: 1, width: 220 }} label={t('每份文档最多保留')} value={n} onChange={(e) => setN(e.target.value)} error={bad}
              helperText={bad ? t('填 1 到 1000 之间的整数') : ' '} slotProps={{ input: { endAdornment: <InputAdornment position="end">{t('个旧版本')}</InputAdornment> }, htmlInput: { inputMode: 'numeric' } }} />
          )}
          <Box>
            <Button variant="contained" sx={{ mt: 1 }} disabled={!changed || bad || set.isPending}
              onClick={() => set.mutate(limit ? num : 0, { onSuccess: () => app.toast(t('已保存')), onError: (e) => app.toast(e.message) })}>{t('保存')}</Button>
          </Box>
        </Card>
      )}
    </Box>
  );
}

/** 家人的私密文档（v1.8.0）：空间管理员看不到内容，只知道有几份、占多少。 */
function PrivateUsage() {
  const role = useRole();
  const loc = useStorageLocation(role.isOwner);
  const n = loc.data?.current.privateDocuments;
  if (!role.isOwner || !n) return null;
  return (
    <Typography variant="body2" sx={{ color: 'text.secondary', mt: 1.5 }}>
      {t('其中有家人的私密文档 {n} 份，占 {size}。它们只有本人和本人允许的人看得到，空间管理员也看不到内容。', { n, size: bytes(loc.data!.current.privateBytes ?? 0) })}
    </Typography>
  );
}

/** 文档放在哪里（v1.7.2）：空间管理员从实例管理员允许的存储中给本空间选一个。只有数据目录时不显示。 */
function LocationSection() {
  const role = useRole();
  const loc = useStorageLocation(role.isOwner);
  const [open, setOpen] = useState(false);
  const [cold, setCold] = useState(false);
  if (!role.isOwner || !loc.data) return null;
  const { current, choices } = loc.data;
  if (choices.length <= 1 && current.name === 'local' && current.pending === 0 && !current.cold) return null;
  const mine = choices.find((c) => c.name === current.name);
  return (
    <Box component="section" aria-label={t('文档放在哪里')} sx={{ mt: 4 }}>
      <Typography variant="h5" sx={{ mb: 0.5 }}>{t('文档放在哪里')}</Typography>
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>
        {t('新上传的文档写到这个存储。换存储时可以把已有的文档一起移过去，期间照常使用。缩略图始终放在服务器上。')}
      </Typography>
      <Card sx={{ p: 2 }}>
        <Stack direction="row" spacing={2} sx={{ alignItems: 'center' }}>
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <StorageCell s={current} label={choiceLabel(mine ?? { name: current.name, kind: '', description: '' })} noProgress />
          </Box>
          <Button variant="outlined" onClick={() => setOpen(true)}>{t('更换')}</Button>
        </Stack>
        <MoveStatus s={current} />
        {eeBuild && (
        <Stack direction="row" spacing={2} sx={{ alignItems: 'center', mt: 2, pt: 2, borderTop: 1, borderColor: 'divider' }}>
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography>{t('旧文件放到网盘')}</Typography>
            <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block' }}>{t('冷热分层')}</Typography>
            <Typography variant="body2" sx={{ color: 'text.secondary' }}>
              {current.cold ? t('文档日期超过 {n} 个月的，移到{storage}', { n: current.coldAfterMonths ?? 0, storage: choiceLabel(choices.find((c) => c.name === current.cold) ?? { name: current.cold, kind: '', description: '' }) }) : t('和新文档放在一起')}
            </Typography>
          </Box>
          <Button variant="outlined" onClick={() => setCold(true)} disabled={choices.length < 2}>{tc('设置一项', '设置')}</Button>
        </Stack>
        )}
      </Card>
      {open && <LocationDialog current={current} choices={choices} onClose={() => setOpen(false)} />}
      {cold && <ColdDialog current={current} choices={choices} label={choiceLabel} onClose={() => setCold(false)} />}
    </Box>
  );
}

/** 移动文档的进度（v1.7.14）：进行中显示进度条；结束后保留上次的结果。都可以到任务记录里看详情。 */
function MoveStatus({ s }: { s: WorkspaceStorage }) {
  const p = s.progress;
  const link = <Link component={RouterLink} to="/tasks?group=storage" underline="hover">{t('在任务记录里查看')}</Link>;
  if (s.moving) {
    const done = p ? p.moved + p.skipped + p.failed : 0;
    return (
      <Box sx={{ mt: 2 }} aria-label={t('移动文档的进度')}>
        <LinearProgress variant={p ? 'determinate' : 'indeterminate'} value={p ? Math.round((done * 100) / p.total) : undefined} aria-label={t('移动文档')} />
        <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.75 }}>
          {p ? t('正在移动文档：{done}/{n} 份', { done, n: p.total }) : t('正在准备移动文档')}
          {p && p.skipped > 0 && t('，{n} 份正在处理，稍后再移', { n: p.skipped })}{p && p.failed > 0 && t('，{n} 份失败', { n: p.failed })}{t('。期间照常使用。')}{link}
        </Typography>
      </Box>
    );
  }
  const l = s.last;
  if (!l) return null;
  return (
    <Typography variant="body2" sx={{ color: 'text.secondary', mt: 1.5 }}>
      {t('上次移动：{time} 完成，移了 {n} 份', { time: dateTime(l.finishedAt), n: l.moved })}{l.skipped > 0 && t('，{n} 份留待下次', { n: l.skipped })}{l.failed > 0 && t('，{n} 份失败', { n: l.failed })}{t('。')}{link}
    </Typography>
  );
}

function choiceLabel(c: StorageChoice) {
  if (c.name === 'local') return t('服务器的数据目录（local）');
  return t('{label}（{name}）', c.description ? { label: c.description, name: c.name } : { label: c.name, name: KIND[c.kind as AdminStorage['kind']] ?? c.kind });
}


function LocationDialog({ current, choices, onClose }: { current: WorkspaceStorage; choices: StorageChoice[]; onClose: () => void }) {
  const app = useApp();
  const set = useSetStorageLocation();
  const [name, setName] = useState(current.name);
  const [move, setMove] = useState(true);
  const changed = name !== current.name;
  const can = changed || (move && current.pending > 0 && !current.moving);
  const save = () => set.mutate({ storage: name, move }, {
    onSuccess: (r) => { app.toast(r.jobId ? t('已更换，正在后台移动文档') : t('已更换，新文档会写到这里')); onClose(); },
    onError: (e) => app.toast(e.message),
  });
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="xs">
      <DialogTitle>{t('文档放在哪里')}</DialogTitle>
      <DialogContent>
        <RadioGroup value={name} onChange={(e) => setName(e.target.value)} aria-label={t('存储')}>
          {choices.map((c) => (
            <FormControlLabel key={c.name} value={c.name} control={<Radio />} label={c.name === current.name ? t('{name}（现在）', { name: choiceLabel(c) }) : choiceLabel(c)} />
          ))}
        </RadioGroup>
        {(changed || current.pending > 0) && (
          <FormControlLabel sx={{ mt: 1 }} control={<Checkbox checked={move} onChange={(e) => setMove(e.target.checked)} />}
            label={changed ? t('把已有的文档也移过去') : t('把还在其他存储的 {n} 份移过来', { n: current.pending })} />
        )}
        {move && (changed || current.pending > 0) && (
          <Typography variant="body2" sx={{ color: 'text.secondary', ml: 4 }}>{t('逐份复制、读回核对后才删除原处的文件，文档多时需要一些时间。')}</Typography>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!can || set.isPending} onClick={save}>{t('保存')}</Button>
      </DialogActions>
    </Dialog>
  );
}

/** 导出整个空间：按“抽屉/年份/日期 标题”放文件，附清单。 */
function ExportSection() {
  const app = useApp();
  const role = useRole();
  const [file, setFile] = useState<'searchable' | 'original'>('searchable');
  if (!role.isOwner) return null;
  return (
    <Box component="section" aria-label={t('导出整个空间')} sx={{ mt: 4 }}>
      <Typography variant="h5" sx={{ mb: 0.5 }}>{t('导出整个空间')}</Typography>
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>
        {t('把所有文档下载成一个 zip：每个抽屉一个文件夹，按年份分开，文件名是“日期 标题”；附一份清单（Excel、WPS 可打开），列出每份文档的类型、往来方、标签、字段和备注。不装任何软件也能翻看，适合长期留存或换用别的工具。回收站里的不导出。这不是备份，不能导回来。')}
      </Typography>
      <Card sx={{ p: 2 }}>
        <RadioGroup value={file} onChange={(e) => setFile(e.target.value as 'searchable' | 'original')} aria-label={t('导出哪个文件')}>
          <FormControlLabel value="searchable" control={<Radio />} label={t('可搜索版（识别过文字的 PDF，没有时用原件），推荐')} />
          <FormControlLabel value="original" control={<Radio />} label={t('上传时的原件')} />
        </RadioGroup>
        <Button variant="contained" sx={{ mt: 1 }} onClick={() => { startDownload(exportUrl(file)); app.toast(t('开始导出，浏览器会边打包边下载；文档多时需要一些时间')); }}>{t('导出')}</Button>
      </Card>
    </Box>
  );
}

const KIND: Record<AdminStorage['kind'], string> = { local: t('数据目录'), dir: t('本机目录'), s3: t('S3 兼容'), webdav: 'WebDAV', other: t('其他'), missing: t('没有配置') };

/** 实例管理 → 文件安全（v1.7.3 从系统状态移出，v1.13.9 改名并加上三张目的卡片）：文件放在哪里、多存一份、各空间放在哪。 */
export function InstanceStoragePanel() {
  return (
    <>
      <Typography variant="h4">{t('文件安全')}</Typography>
      <Typography sx={{ color: 'text.secondary', mt: 0.5 }}>{t('手机、电脑看到的都是这台服务器上的同一份，不用同步。这里管的是文件放在哪里，以及在别处多存一份。只有实例管理员可以查看。')}</Typography>
      <GoalCards />
      <StorageLocations />
      <EncryptionSection />
      <Box id={SAFETY_ANCHOR.local} sx={{ scrollMarginTop: 16 }}><LocalBackupSection /></Box>
      <Box id={SAFETY_ANCHOR.backup} sx={{ scrollMarginTop: 16 }}><BackupSection /></Box>
      <StorageExportSection />
      <Box id={SAFETY_ANCHOR.spaces} sx={{ scrollMarginTop: 16 }}><WorkspacesStorage /></Box>
    </>
  );
}

/** 存储位置：文档的文件放在哪些地方，能不能读写；添加、修改、删除存储。 */
export function StorageLocations() {
  const app = useApp();
  const list = useAdminStorages();
  const test = useTestStorage();
  const del = useDeleteStorage();
  const [editing, setEditing] = useState<AdminStorage | 'new' | null>(null);
  const [results, setResults] = useState<Record<string, StorageTest | string>>({});
  const [running, setRunning] = useState<Record<string, boolean>>({});
  const encryption = useEncryptionSwitch();
  if (list.isPending) return <LoadingState />;
  if (list.isError) return <ErrorState onRetry={() => list.refetch()} />;
  const missing = list.data.filter((s) => s.kind === 'missing' && s.documents > 0);
  // 每一行各自测试，结果各自显示
  const run = (name: string) => {
    setRunning((m) => ({ ...m, [name]: true }));
    test.mutateAsync(name)
      .then((r) => setResults((m) => ({ ...m, [name]: r })), (e: Error) => setResults((m) => ({ ...m, [name]: e.message })))
      .finally(() => setRunning((m) => ({ ...m, [name]: false })));
  };
  const remove = (st: AdminStorage) => app.confirm({
    title: t('删除存储“{name}”？', { name: st.name }), body: t('只删除这项设置，不会动存储里的文件。'), verb: t('删除'),
    run: () => del.mutate(st.name, { onSuccess: () => { app.confirm(null); app.toast(t('已删除')); }, onError: (e) => { app.confirm(null); app.toast(e.message); } }),
  });
  return (
    <Box component="section" aria-label={t('文件放在哪里')} sx={{ mt: 4 }}>
      <SectionTitle title={t('文件放在哪里')} tech={t('存储：服务器上的目录、S3 兼容存储、WebDAV')}
        action={<Button variant="outlined" size="small" onClick={() => setEditing('new')}>{t('添加')}</Button>} />
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>
        {t('文档的原件、可搜索版和历史版本放在这些地方，缩略图和封面始终在数据目录。可以添加 S3 兼容存储（MinIO、阿里云 OSS、腾讯云 COS、NAS 的 S3 服务等）或服务器上的目录，然后在下面给空间换存储，空间管理员也可以在自己空间的“设置 → 空间 → 存储”里换。部署文件（KEPT_STORAGES）里配置的存储也列在这里，只能在部署文件里修改。')}
      </Typography>
      {list.data.length > 1 && (
        <Alert severity="info" sx={{ mb: 1.5 }}>{t('备份（KEPT_BACKUP_DIR、kept backup）只包含数据目录里的文件；放在其他存储的文件请另行备份。')}</Alert>
      )}
      {missing.map((s) => (
        <Alert key={s.name} severity="error" sx={{ mb: 1.5 }}>
          {t('有 {n} 份文档放在存储“{name}”里，但已经没有这个存储，这些文档打不开。请把它加回部署配置，或用同样的名字重新添加。', { n: s.documents, name: s.name })}
        </Alert>
      ))}
      <Card sx={{ overflowX: 'auto' }}>
        <Table size="small">
          <TableHead>
            <TableRow><TableCell>{t('名称')}</TableCell><TableCell>{t('位置')}</TableCell><TableCell align="right">{t('文档')}</TableCell><TableCell>{t('已用')}</TableCell><TableCell>{t('写入的空间')}</TableCell><TableCell>{t('加密')}</TableCell><TableCell /></TableRow>
          </TableHead>
          <TableBody>
            {list.data.map((st) => {
              const r = results[st.name];
              return (
                <TableRow key={st.name}>
                  <TableCell>
                    {st.name}
                    <Typography variant="caption" sx={{ display: 'block', whiteSpace: 'nowrap', color: st.kind === 'missing' ? 'error.main' : 'text.secondary' }}>
                      {KIND[st.kind]}{st.source === 'env' ? ` · ${t('部署配置')}` : ''}{st.source === 'page' && !st.selectable ? ` · ${t('仅实例管理员分配')}` : ''}
                    </Typography>
                    {st.description && <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>{st.description}</Typography>}
                    {st.error && <Typography variant="caption" sx={{ display: 'block', color: 'error.main', maxWidth: 240 }}>{st.error}</Typography>}
                  </TableCell>
                  <TableCell sx={{ fontFamily: 'monospace', fontSize: 13, overflowWrap: 'anywhere' }}>{st.location || '—'}</TableCell>
                  <TableCell align="right" sx={{ fontVariantNumeric: 'tabular-nums' }}>{st.documents}</TableCell>
                  <TableCell sx={{ whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums', minWidth: 110 }}>
                    {bytes(st.used ?? 0)}{st.capacity ? ` / ${bytes(st.capacity)}` : ''}
                    {!!st.capacity && (
                      <LinearProgress variant="determinate" value={Math.min(100, ((st.used ?? 0) / st.capacity) * 100)} aria-label={t('{name} 的容量', { name: st.name })}
                        color={(st.used ?? 0) >= st.capacity * 0.9 ? 'warning' : 'primary'} sx={{ mt: 0.5, height: 4, borderRadius: 2 }} />
                    )}
                  </TableCell>
                  <TableCell>{st.workspaces.length ? st.workspaces.join(t('、')) : <Box component="span" sx={{ color: 'text.secondary' }}>—</Box>}</TableCell>
                  <TableCell sx={{ whiteSpace: 'nowrap' }}>
                    <EncryptionChip st={st} />
                    {st.encryption?.error && !st.encryption.locked && <Typography variant="caption" sx={{ display: 'block', color: 'error.main', maxWidth: 240, whiteSpace: 'normal' }}>{st.encryption.error}</Typography>}
                  </TableCell>
                  <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                    {r !== undefined && (
                      typeof r === 'string' || !r.ok
                        ? <Typography component="span" variant="body2" sx={{ color: 'error.main', mr: 1 }}>{typeof r === 'string' ? r : r.error}</Typography>
                        : <Chip size="small" color="success" variant="outlined" label={t('可以读写 · {ms} ms', { ms: r.tookMs })} sx={{ mr: 1 }} />
                    )}
                    {st.kind !== 'missing' && (
                      <Button size="small" disabled={!!running[st.name]} onClick={() => run(st.name)}>{t('测试')}</Button>
                    )}
                    {eeBuild && encryptable(st) && (
                      <Button size="small" onClick={() => encryption.ask(st)}>{st.encryption?.enabled ? t('关闭加密') : t('开启加密')}</Button>
                    )}
                    {st.source === 'page' && <Button size="small" onClick={() => setEditing(st)}>{t('修改')}</Button>}
                    {st.source === 'page' && <Button size="small" color="error" onClick={() => remove(st)}>{t('删除')}</Button>}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </Card>
      {editing && (
        <StorageFormDialog storage={editing === 'new' ? undefined : editing} onClose={() => setEditing(null)}
          onCreated={(name, encrypt) => { if (encrypt) encryption.turn(name, true); }} />
      )}
      {encryption.dialog}
    </Box>
  );
}

/** 空间的存储：名字，以及移动文档的进度或剩余。noProgress：进度另外显示（空间自己的存储页）。 */
function StorageCell({ s, label, noProgress }: { s: WorkspaceStorage; label?: string; noProgress?: boolean }) {
  const p = s.progress;
  let note = '';
  if (s.moving && noProgress) note = '';
  else if (s.moving) note = p ? t('正在移动 {done}/{total}', { done: p.moved + p.skipped + p.failed, total: p.total }) : t('等待移动');
  else if (s.pending > 0) note = t('还有 {n} 份在其他存储', { n: s.pending });
  return (
    <>
      {label ?? s.name}
      {s.cold && <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>{t('超过 {n} 个月的放在 {storage}', { n: s.coldAfterMonths ?? 0, storage: s.cold })}</Typography>}
      {note && <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>{note}</Typography>}
      {s.error && (
        <Typography variant="caption" title={s.error}
          sx={{ display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden', color: 'error.main', maxWidth: 280, overflowWrap: 'anywhere' }}>
          {s.error}
        </Typography>
      )}
    </>
  );
}

/** 各空间的存储：实例管理员设置配额、换存储。 */
export function WorkspacesStorage() {
  const list = useAdminWorkspaces();
  const storages = useAdminStorages();
  const [editing, setEditing] = useState<AdminWorkspace | null>(null);
  const [moving, setMoving] = useState<AdminWorkspace | null>(null);
  if (list.isPending) return <LoadingState />;
  if (list.isError) return <ErrorState onRetry={() => list.refetch()} />;
  const def = list.data.defaultQuota;
  const usable = (storages.data ?? []).filter((s) => s.kind !== 'missing');
  // 只有数据目录、也没有文档在别处时，不显示存储这一列
  const showStorage = usable.length > 1 || list.data.items.some((w) => w.storage.name !== 'local' || w.storage.pending > 0);
  return (
    <Box component="section" aria-label={t('各空间放在哪')} sx={{ mt: 4 }}>
      <SectionTitle title={t('各空间放在哪')} tech={t('各空间的存储、冷热分层与配额')} />
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>
        {t('默认配额：{quota}（部署时用 KEPT_DEFAULT_QUOTA 设置）。超出配额的空间不能再上传，已有的文档照常可用。', { quota: def ? bytes(def) : t('不限') })}
      </Typography>
      <Card sx={{ overflowX: 'auto' }}>
        <Table size="small" sx={{ '& th': { whiteSpace: 'nowrap' } }}>
          <TableHead>
            <TableRow><TableCell>{t('空间')}</TableCell><TableCell>{tc('空间的管理员', '管理员')}</TableCell><TableCell align="right">{t('已用')}</TableCell><TableCell>{t('配额')}</TableCell>{showStorage && <TableCell>{t('存储')}</TableCell>}<TableCell /></TableRow>
          </TableHead>
          <TableBody>
            {list.data.items.map((w) => {
              const share = quotaShare(w.usage);
              return (
                <TableRow key={w.id}>
                  <TableCell>{w.name}<Typography variant="caption" sx={{ color: 'text.secondary', display: 'block' }}>{t('{n} 位成员', { n: w.members })}</Typography></TableCell>
                  <TableCell>{w.owners}</TableCell>
                  <TableCell align="right" sx={{ fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap', color: share !== null && share >= 0.9 ? 'warning.main' : undefined }}>
                    {bytes(w.usage.total)}{share !== null ? t('（{percent}%）', { percent: Math.round(share * 100) }) : ''}
                  </TableCell>
                  <TableCell>{quotaText(w.usage, def)}</TableCell>
                  {showStorage && <TableCell><StorageCell s={w.storage} /></TableCell>}
                  <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                    {eeBuild && <Button size="small" onClick={() => setEditing(w)}>{t('改配额')}</Button>}
                    {showStorage && <Button size="small" onClick={() => setMoving(w)}>{t('换位置')}</Button>}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </Card>
      {editing && <QuotaDialog w={editing} defaultQuota={def} onClose={() => setEditing(null)} />}
      {moving && <StorageDialog w={moving} storages={usable} onClose={() => setMoving(null)} />}
    </Box>
  );
}


function StorageDialog({ w, storages, onClose }: { w: AdminWorkspace; storages: AdminStorage[]; onClose: () => void }) {
  const app = useApp();
  const set = useSetWorkspaceStorage();
  const setCold = useAdminSetCold();
  const cur = w.storage;
  const [cold, setColdName] = useState(cur.cold ?? '');
  const [months, setMonths] = useState(String(cur.coldAfterMonths ?? 24));
  const coldChanged = cold !== (cur.cold ?? '') || (cold !== '' && Number(months) !== cur.coldAfterMonths);
  const monthsOk = cold === '' || (Number.isInteger(Number(months)) && Number(months) >= 1 && Number(months) <= 1200);
  const [name, setName] = useState(cur.name);
  const [move, setMove] = useState(true);
  const changed = name !== cur.name;
  // 换到别处时问要不要把已有的文档移过去；留在原处时只问没移完的
  const ask = changed || cur.pending > 0;
  const can = (changed || (move && cur.pending > 0 && !cur.moving) || coldChanged) && monthsOk && !(cold !== '' && cold === name);
  const save = async () => {
    try {
      let job: number | undefined;
      if (changed || (move && cur.pending > 0)) job = (await set.mutateAsync({ id: w.id, storage: name, move })).jobId ?? job;
      if (coldChanged) job = (await setCold.mutateAsync({ id: w.id, storage: cold, months: cold ? Number(months) : 0 })).jobId ?? job;
      app.toast(job ? t('已设置，正在后台移动文档') : t('已设置'));
      onClose();
    } catch (e) {
      app.toast((e as Error).message);
    }
  };
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>{t('“{name}”的存储', { name: w.name })}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>{t('新上传的文档写到选中的存储。')}</Typography>
        <RadioGroup value={name} onChange={(e) => setName(e.target.value)} aria-label={t('存储')}>
          {storages.map((s) => (
            <FormControlLabel key={s.name} value={s.name} control={<Radio />} sx={{ alignItems: 'flex-start', mb: 0.5, '& .MuiRadio-root': { pt: 0.5 } }}
              label={<Box sx={{ pt: 0.75 }}>
                <Typography>{s.name === cur.name ? t('{name}（现在）', { name: s.name }) : s.name}</Typography>
                <Typography variant="caption" sx={{ color: 'text.secondary', fontFamily: 'monospace', overflowWrap: 'anywhere' }}>{s.location}</Typography>
              </Box>} />
          ))}
        </RadioGroup>
        {ask && (
          <>
            <FormControlLabel sx={{ mt: 1 }} control={<Checkbox checked={move} onChange={(e) => setMove(e.target.checked)} />}
              label={changed ? t('把已有的文档也移过去') : t('把还在其他存储的 {n} 份移过来', { n: cur.pending })} />
            {move && (
              <Typography variant="body2" sx={{ color: 'text.secondary', ml: 4 }}>
                {t('在后台逐份复制、读回核对后，才删除原处的文件；期间照常使用。正在处理的文档稍后再移。')}
              </Typography>
            )}
          </>
        )}
        {eeBuild && (
          <>
        <Typography variant="subtitle2" sx={{ mt: 3 }}>{t('旧文件放到网盘')}</Typography>
        <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('冷热分层：按文档日期，把较早的移到另一个地方')}</Typography>
        <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} sx={{ mt: 1.5 }}>
          <TextField select label={t('文档日期较早的移到')} value={cold} onChange={(e) => setColdName(e.target.value)} sx={{ minWidth: 220 }}
            error={cold !== '' && cold === name} helperText={cold !== '' && cold === name ? t('要和新文档的存储不同') : ' '}>
            <MenuItem value="">{t('不移动')}</MenuItem>
            {storages.map((s) => <MenuItem key={s.name} value={s.name}>{s.name}</MenuItem>)}
          </TextField>
          {cold && (
            <TextField label={t('超过')} type="number" value={months} onChange={(e) => setMonths(e.target.value)} error={!monthsOk} sx={{ width: 160 }}
              slotProps={{ input: { endAdornment: <InputAdornment position="end">{t('个月')}</InputAdornment> }, htmlInput: { min: 1, max: 1200 } }} helperText=" " />
          )}
        </Stack>
          </>
        )}
        {cur.moving && <Alert severity="info" sx={{ mt: 2 }}>{t('正在移动文档。改了存储后，任务会按新的存储继续。')}</Alert>}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!can || set.isPending || setCold.isPending} onClick={() => void save()}>{t('保存')}</Button>
      </DialogActions>
    </Dialog>
  );
}

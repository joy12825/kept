// 文件安全（v1.13.9）：实例管理里的存储设置按“想要什么”组织——每天多存一份、旧文件放到网盘、整个换个地方放。
// 卡片只说结论和状态，技术名称放在灰色小字里；“设置”跳到下面对应的分区，那里是原来的完整设置。
import { useEffect, useState, type ReactNode } from 'react';
import { Alert, Box, Button, Card, Chip, Stack, TextField, Typography } from '@mui/material';
import { eeBuild } from '@ee';
import { hasFeature, useEdition } from '../../api/edition';
import { useAdminWorkspaces, useBackupTarget, type AdminWorkspace, type BackupTarget } from '../../api/storage';
import { useRunBackup, useSetBackupDir, useSystemStatus, type SystemStatus } from '../../api/system';
import { ErrorState, LoadingState } from '../../components/common';
import { t } from '../../i18n';
import { useApp } from '../../store/app-store';
import { relativeTime } from '../detail/time';
import { bytes } from './system';

/** 分区的锚点：卡片上的“设置”跳到这里。 */
export const SAFETY_ANCHOR = { local: 'safety-local', backup: 'safety-backup', spaces: 'safety-spaces' } as const;

type LocalBackup = SystemStatus['backup'];

type State = 'on' | 'off' | 'warn' | 'paid' | 'info';

const CHIP: Record<Exclude<State, 'info'>, { label: string; color: 'success' | 'default' | 'warning' | 'primary' }> = {
  on: { label: t('已开启'), color: 'success' },
  off: { label: t('未开启'), color: 'default' },
  warn: { label: t('要留意'), color: 'warning' },
  paid: { label: t('收费版'), color: 'primary' },
};

/** 区块标题：大白话做标题，技术名称做下面的灰色小字（给熟悉存储的人对号入座）。 */
export function SectionTitle({ title, tech, action }: { title: string; tech: string; action?: ReactNode }) {
  return (
    <Stack direction="row" spacing={2} sx={{ alignItems: 'flex-start', mb: 0.5 }}>
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <Typography variant="h5">{title}</Typography>
        <Typography variant="caption" sx={{ color: 'text.secondary' }}>{tech}</Typography>
      </Box>
      {action}
    </Stack>
  );
}

function jump(anchor: string) {
  document.getElementById(anchor)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
}

function GoalCard({ title, about, tech, state, status, anchor }: { title: string; about: string; tech: string; state: State; status: string; anchor?: string }) {
  const chip = state === 'info' ? null : CHIP[state];
  return (
    <Card component="article" aria-label={title} sx={{ p: 2.5, display: 'flex', flexDirection: 'column', gap: 1 }}>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
        <Typography variant="h6" sx={{ flex: 1, fontSize: 17 }}>{title}</Typography>
        {chip && <Chip size="small" variant="outlined" color={chip.color} label={chip.label} />}
      </Stack>
      <Typography variant="body2" sx={{ color: 'text.secondary' }}>{about}</Typography>
      <Typography variant="body2" sx={{ color: state === 'warn' ? 'warning.main' : 'text.primary', flex: 1 }}>{status}</Typography>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', pt: 0.5 }}>
        <Typography variant="caption" sx={{ color: 'text.secondary', flex: 1, minWidth: 0 }}>{tech}</Typography>
        {anchor && <Button size="small" onClick={() => jump(anchor)}>{t('设置')}</Button>}
      </Stack>
    </Card>
  );
}

/** “每天多存一份”的一句话状态：先看存到网络位置的（收费版），再看存到另一块硬盘的（各版本，v1.13.9）。 */
export function backupStatus(target: BackupTarget | undefined, local: LocalBackup | undefined, has: boolean, now = new Date()): { state: State; status: string } {
  if (!target || !local) return { state: 'info', status: '…' };
  const dir = local.dir ? t('另外每天 02:30 存到 {dir}。', { dir: local.dir }) : '';
  if (target.storage) {
    const r = target.last;
    if (!r) return { state: 'on', status: t('已开启：每天 02:45 存到“{storage}”，还没运行过。', { storage: target.storage }) + dir };
    const when = relativeTime(r.at, now);
    if (r.status === 'queued' || r.status === 'running') return { state: 'on', status: t('正在多存一份到“{storage}”…', { storage: target.storage }) };
    if (r.status === 'failed') return { state: 'warn', status: t('最近一次没存成功（{when}）。下面“每天多存一份”里有原因，修好后会在下一次自动补上。', { when }) };
    if (r.skipped) return { state: 'warn', status: t('最近一次没有存（{when}）：{reason}', { when, reason: r.skipped }) };
    if (r.missing || r.external) return { state: 'warn', status: t('{when}已多存一份到“{storage}”，但有 {n} 个文件没存进去，下面有详情。', { when, storage: target.storage, n: (r.missing ?? 0) + (r.external ?? 0) }) };
    return { state: 'on', status: t('{when}已多存一份到“{storage}”，{n} 份文档。', { when, storage: target.storage, n: r.documents ?? 0 }) + dir };
  }
  if (local.dir) {
    const r = local.last;
    if (!r) return { state: 'on', status: t('已开启：每天 02:30 存到 {dir}，还没运行过。', { dir: local.dir }) };
    const when = relativeTime(r.at, now);
    if (r.status === 'queued' || r.status === 'running') return { state: 'on', status: t('正在多存一份到 {dir}…', { dir: local.dir }) };
    if (r.status === 'failed') return { state: 'warn', status: t('最近一次没存成功（{when}）。下面“存到另一块硬盘”里有原因，修好后会在下一次自动补上。', { when }) };
    if (r.missing) return { state: 'warn', status: t('{when}已多存一份到 {dir}，但有 {n} 个文件没存进去，下面有详情。', { when, dir: local.dir, n: r.missing }) };
    return { state: 'on', status: t('{when}已多存一份到 {dir}，{n} 份文档。', { when, dir: local.dir, n: r.documents ?? 0 }) };
  }
  if (!has) return { state: 'off', status: t('还没开启。可以在下面选服务器上的另一块硬盘，每天存一份；收费版还能存到 NAS 或网盘。') };
  return { state: 'off', status: t('还没开启。服务器的硬盘坏了，文件就找不回来了。') };
}

/** “旧文件放到网盘”的一句话状态。 */
export function coldStatus(spaces: AdminWorkspace[] | undefined, has: boolean): { state: State; status: string } {
  if (!has) return { state: 'paid', status: t('收费版可以把几年前的旧文件放到更便宜的网盘或云存储，用的时候照常打开。') };
  if (!spaces) return { state: 'info', status: '…' };
  const cold = spaces.filter((w) => w.storage.cold);
  if (!cold.length) return { state: 'off', status: t('还没开启。服务器放得下就不用开。') };
  const w = cold[0];
  const vars = { name: w.name, n: w.storage.coldAfterMonths ?? 0, storage: w.storage.cold ?? '' };
  return { state: 'on', status: cold.length === 1 ? t('“{name}”超过 {n} 个月的文档放在“{storage}”。', vars) : t('{k} 个空间已开启，例如“{name}”超过 {n} 个月的文档放在“{storage}”。', { ...vars, k: cold.length }) };
}

/** “整个换个地方放”的一句话状态。 */
export function placeStatus(spaces: AdminWorkspace[] | undefined): { state: State; status: string } {
  if (!spaces) return { state: 'info', status: '…' };
  const moving = spaces.find((w) => w.storage.moving);
  if (moving) {
    const p = moving.storage.progress;
    return { state: 'info', status: p ? t('正在把“{name}”的文档移到“{storage}”：{done}/{total}。', { name: moving.name, storage: moving.storage.name, done: p.moved + p.skipped + p.failed, total: p.total }) : t('正在准备移动“{name}”的文档。', { name: moving.name }) };
  }
  const left = spaces.find((w) => w.storage.pending > 0);
  if (left) return { state: 'warn', status: t('“{name}”还有 {n} 份文档在原来的位置。', { name: left.name, n: left.storage.pending }) };
  const elsewhere = spaces.filter((w) => w.storage.name !== 'local');
  if (!elsewhere.length) return { state: 'info', status: t('文档都放在服务器的数据目录里。') };
  return { state: 'info', status: elsewhere.length === 1 ? t('“{name}”的文档放在“{storage}”，其余在服务器的数据目录。', { name: elsewhere[0].name, storage: elsewhere[0].storage.name }) : t('{k} 个空间放在别处，例如“{name}”放在“{storage}”。', { k: elsewhere.length, name: elsewhere[0].name, storage: elsewhere[0].storage.name }) };
}

/** 文件安全页顶部的三张卡片。 */
export function GoalCards() {
  const edition = useEdition().data;
  const has = eeBuild && hasFeature(edition, 'storage');
  const backup = useBackupTarget();
  const local = useSystemStatus();
  const spaces = useAdminWorkspaces();
  const b = backupStatus(backup.data, local.data?.backup, has);
  const c = coldStatus(spaces.data?.items, has);
  const p = placeStatus(spaces.data?.items);
  return (
    <Box component="section" aria-label={t('想做什么')} sx={{ mt: 3, display: 'grid', gap: 2, gridTemplateColumns: { xs: '1fr', md: 'repeat(3, minmax(0, 1fr))' } }}>
      <GoalCard title={t('每天多存一份')} about={t('服务器的硬盘坏了、机器丢了，文件也还在。')} tech={backup.data?.storage || (has && !local.data?.backup.dir) ? t('备份到存储 · 每天 02:45') : t('本机备份 · 每天 02:30')} {...b} anchor={has && backup.data?.storage ? SAFETY_ANCHOR.backup : SAFETY_ANCHOR.local} />
      <GoalCard title={t('旧文件放到网盘')} about={t('服务器放不下了，几年前的文件移过去，用的时候照常打开。')} tech={t('冷热分层 · 按空间设置')} {...c} anchor={has ? SAFETY_ANCHOR.spaces : undefined} />
      <GoalCard title={t('整个换个地方放')} about={t('新文件和已有的文件都搬到另一个位置，期间照常使用。')} tech={t('给空间换存储 · 按空间设置')} {...p} anchor={SAFETY_ANCHOR.spaces} />
    </Box>
  );
}

/** 存到另一块硬盘（v1.13.9，各版本都有）：选服务器上的一个目录，每天 02:30 备份过去；部署时设置了 KEPT_BACKUP_DIR 的以它为准。 */
export function LocalBackupSection() {
  const app = useApp();
  const status = useSystemStatus();
  const set = useSetBackupDir();
  const run = useRunBackup();
  const saved = status.data?.backup.dir ?? '';
  const [dir, setDir] = useState(saved);
  useEffect(() => { setDir(saved); }, [saved]);
  if (status.isPending) return <LoadingState />;
  if (status.isError) return <ErrorState onRetry={() => status.refetch()} />;
  const b = status.data.backup;
  const l = b.last;
  const busy = run.isPending || l?.status === 'queued' || l?.status === 'running';
  const changed = dir.trim() !== saved;
  const save = (value: string) => set.mutate(value, {
    onSuccess: () => app.toast(value ? t('每天 02:30 备份到 {dir}', { dir: value }) : t('已停止每天备份')),
    onError: (e) => app.toast(e.message),
  });
  const stop = () => app.confirm({
    title: t('停止每天备份？'), body: t('已经备份的文件留在那个目录里，不会删除。'), verb: t('停止'),
    run: () => { app.confirm(null); save(''); },
  });
  const now = () => run.mutate(undefined, { onSuccess: () => app.toast(t('已开始备份')), onError: (e) => app.toast(e.message) });
  return (
    <Box component="section" aria-label={t('存到另一块硬盘')} sx={{ mt: 4 }}>
      <SectionTitle title={t('存到另一块硬盘')} tech={t('本机备份：数据库快照和全部文件，每天 02:30（KEPT_BACKUP_DIR）')} />
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>
        {t('选服务器上的一个目录，最好在另一块硬盘上，例如接在服务器上的移动硬盘，或挂载进来的 NAS 目录。每天 02:30 把数据库快照和全部文件备份过去，只复制新增或变化的；服务器的硬盘坏了时，用它恢复。')}
      </Typography>
      <Card sx={{ p: 2 }}>
        {b.dirFromEnv ? (
          <Typography variant="body2">{t('部署时设置的目录：{dir}。要改请修改部署配置（KEPT_BACKUP_DIR）。', { dir: b.dir ?? '' })}</Typography>
        ) : (
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5} sx={{ alignItems: { sm: 'flex-start' } }}>
            <TextField id="backup-dir" label={t('备份目录')} placeholder="/mnt/usb/kept-backup" value={dir} onChange={(e) => setDir(e.target.value)} sx={{ flex: 1 }}
              helperText={t('服务器上的完整路径；目录要已经存在，收好要能往里写。用 Docker 时，先把硬盘挂载进容器。')} slotProps={{ htmlInput: { spellCheck: false } }} />
            <Stack direction="row" spacing={1} sx={{ pt: { sm: 1 } }}>
              <Button variant="contained" disabled={!changed || !dir.trim() || set.isPending} onClick={() => save(dir.trim())}>{t('保存')}</Button>
              {saved && <Button color="error" disabled={set.isPending} onClick={stop}>{t('停止')}</Button>}
            </Stack>
          </Stack>
        )}
        {b.dir && (
          <Stack direction="row" spacing={2} sx={{ alignItems: 'center', mt: 2, pt: 2, borderTop: 1, borderColor: 'divider' }}>
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <LocalLastRun b={b} />
            </Box>
            <Button variant="outlined" disabled={busy || changed} onClick={now}>{busy ? t('正在备份…') : t('立即备份')}</Button>
          </Stack>
        )}
      </Card>
    </Box>
  );
}

function LocalLastRun({ b }: { b: LocalBackup }) {
  const l = b.last;
  if (!l) return <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('还没有备份过')}</Typography>;
  if (l.status === 'queued' || l.status === 'running') return <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('正在备份…')}</Typography>;
  const when = relativeTime(l.at);
  if (l.status === 'failed') return <Alert severity="error">{t('最近一次没存成功（{when}）：{error}', { when, error: l.error ?? '' })}</Alert>;
  return (
    <>
      <Typography variant="body2">
        {t('最近一次：{when}，', { when })}{t('{documents} 份文档、{files} 个文件，这次复制 {copied} 个（{size}）', { documents: l.documents ?? '', files: l.files ?? '', copied: l.copied ?? '', size: bytes(l.bytes ?? 0) })}
      </Typography>
      <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('数据库快照：{n} 份（保留最近 7 份）', { n: b.snapshots })}</Typography>
      {!!l.missing && <Typography variant="body2" sx={{ color: 'warning.main' }}>{t('有 {n} 个文件在数据目录中缺失，没有备份', { n: l.missing })}</Typography>}
    </>
  );
}

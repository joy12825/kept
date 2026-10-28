// 设置 → 任务记录（空间成员）；系统状态、日志（实例管理员）。UI 设计规范 5.11。
import type React from 'react';
import { useState } from 'react';
import { Link as RouterLink, useSearchParams } from 'react-router-dom';
import { Box, Button, Card, Chip, FormControlLabel, Link, MenuItem, Select, Skeleton, Stack, Switch, TextField, Tooltip, Typography } from '@mui/material';
import { ListChecks, RefreshCw, ScrollText } from 'lucide-react';
import { useTestSmtp } from '../../api/notify';
import { useLogs, useRunBackup, useSetWatch, useSystemStatus, useTasks, type LogLevel, type SystemStatus, type TaskGroup, type TaskStatus } from '../../api/system';
import { RetryUpload, TASK_STATUS, TaskProgress } from '../../components/task-center';
import { t, tc } from '../../i18n';
import { useApp } from '../../store/app-store';
import { useMe } from '../../api/session';
import { EmptyState, ErrorState } from '../../components/common';
import { SW } from '../../components/icons';
import { fontMono } from '../../theme';
import { dateTime, relativeTime } from '../detail/time';

const filterSx = { minWidth: 132, fontSize: 14 } as const;

function Rows({ n = 4 }: { n?: number }) {
  return (
    <Stack spacing={1} aria-busy>
      {[100, 92, 84, 96, 88].slice(0, n).map((w) => <Skeleton key={w} variant="rounded" height={52} width={`${w}%`} />)}
    </Stack>
  );
}

/** 字节数写成“12.3 GB”这样。 */
export function bytes(n: number): string {
  if (n < 0) return t('未知');
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${i === 0 || v >= 100 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

/** 运行时长：“3 天 4 小时”“2 小时 5 分钟”“5 分钟”。 */
export function uptime(since: string, now = Date.now()): string {
  const m = Math.max(0, Math.floor((now - new Date(since).getTime()) / 60_000));
  const d = Math.floor(m / 1440);
  const h = Math.floor((m % 1440) / 60);
  if (d) return t('{d} 天 {h} 小时', { d, h });
  if (h) return t('{h} 小时 {m} 分钟', { h, m: m % 60 });
  return t('{n} 分钟', { n: m });
}

// ---- 任务记录 ----

const STATUS = TASK_STATUS;
const GROUPS: [TaskGroup, string][] = [['upload', t('上传')], ['storage', t('存储')], ['organize', t('整理')], ['other', t('其他')]];

export function TasksPanel() {
  const me = useMe().data;
  const [status, setStatus] = useState<TaskStatus | ''>('');
  const [days, setDays] = useState(30);
  // 从存储页等处带着 ?group=storage 进来时先按种类筛选（v1.7.14）
  const [params] = useSearchParams();
  const [group, setGroup] = useState<TaskGroup | ''>(GROUPS.some(([g]) => g === params.get('group')) ? params.get('group') as TaskGroup : '');
  const [mine, setMine] = useState(false);
  const tasks = useTasks(me?.workspace.id, { status: status || undefined, days, group: group || undefined, mine: mine || undefined });
  const items = tasks.data ?? [];

  return (
    <>
      <Typography sx={{ color: 'text.secondary', mb: 2.5 }}>{t('这个空间在后台处理的任务：上传后的识别、重新处理、重新分类、拆分与合并、整理抽屉、移动文档到其他存储、提醒检查和完整性检查。进行中的任务显示进度，失败的会写明原因。')}</Typography>

      <Stack direction="row" spacing={1} sx={{ flexWrap: 'wrap', rowGap: 1, mb: 2 }}>
        <Select size="small" value={status} displayEmpty onChange={(e) => setStatus(e.target.value as TaskStatus | '')} SelectDisplayProps={{ 'aria-label': t('状态') }} sx={filterSx}>
          <MenuItem value="">{t('全部状态')}</MenuItem>
          {(Object.keys(STATUS) as TaskStatus[]).map((k) => <MenuItem key={k} value={k}>{STATUS[k].label}</MenuItem>)}
        </Select>
        <Select size="small" value={group} displayEmpty onChange={(e) => setGroup(e.target.value as TaskGroup | '')} SelectDisplayProps={{ 'aria-label': t('种类') }} sx={filterSx}>
          <MenuItem value="">{t('全部种类')}</MenuItem>
          {GROUPS.map(([g, label]) => <MenuItem key={g} value={g}>{label}</MenuItem>)}
        </Select>
        <Select size="small" value={days} onChange={(e) => setDays(Number(e.target.value))} SelectDisplayProps={{ 'aria-label': t('时间') }} sx={filterSx}>
          {[7, 30, 90].map((d) => <MenuItem key={d} value={d}>{t('最近 {n} 天', { n: d })}</MenuItem>)}
        </Select>
        <FormControlLabel sx={{ ml: 0.5 }} control={<Switch checked={mine} onChange={(e) => setMine(e.target.checked)} />} label={t('只看我的')} />
      </Stack>

      {tasks.isPending && <Rows />}
      {tasks.isError && <ErrorState onRetry={() => tasks.refetch()} />}
      {tasks.isSuccess && !items.length && (
        <EmptyState icon={<ListChecks size={40} strokeWidth={SW} />} title={status || group || mine ? t('没有符合条件的任务') : t('最近没有后台任务')} text={status || group || mine ? t('换一个筛选条件试试。') : t('上传文档、重新处理或修改抽屉规则后，任务会出现在这里。')} />
      )}
      {!!items.length && (
        <Card>
          {items.map((task, i) => (
            <Stack key={task.id} direction="row" spacing={2} sx={{ alignItems: 'flex-start', p: '12px 16px', borderTop: i ? 1 : 0, borderColor: 'divider' }}>
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography sx={{ overflowWrap: 'anywhere' }}>
                  {task.label}
                  {task.summary && <Box component="span" sx={{ color: 'text.secondary' }}>
                    {' · '}{task.documentId ? <Link component={RouterLink} to={`/docs/${task.documentId}`} underline="hover">{task.summary}</Link> : task.summary}
                  </Box>}
                </Typography>
                {(task.status === 'running' || task.status === 'queued') && <Box sx={{ mt: 0.75, maxWidth: 360 }}><TaskProgress t={task} /></Box>}
                {task.error && <Typography variant="body2" sx={{ color: 'error.main', mt: 0.25, overflowWrap: 'anywhere' }}>{task.error}</Typography>}
                {task.status === 'failed' && <RetryUpload id={task.id} retryable={task.retryable} expired={task.expired} />}
                {(task.creatorName || task.attempts > 1) && <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>
                  {[task.creatorName, task.attempts > 1 ? t('已尝试 {n} 次', { n: task.attempts }) : ''].filter(Boolean).join(' · ')}
                </Typography>}
              </Box>
              <Chip size="small" label={STATUS[task.status].label} color={STATUS[task.status].color} variant={task.status === 'queued' ? 'outlined' : 'filled'} sx={{ flex: 'none' }} />
              <Tooltip title={dateTime(task.createdAt)}>
                <Typography variant="body2" sx={{ color: 'text.secondary', flex: 'none', minWidth: 88, textAlign: 'right' }}>{relativeTime(task.createdAt)}</Typography>
              </Tooltip>
            </Stack>
          ))}
        </Card>
      )}
    </>
  );
}

// ---- 系统状态 ----

function Section({ title, action, children }: { title: string; action?: React.ReactNode; children: React.ReactNode }) {
  return (
    <>
      <Stack direction="row" sx={{ alignItems: 'center', mt: 3, mb: 1.5 }}><Typography variant="h5" sx={{ flex: 1 }}>{title}</Typography>{action}</Stack>
      <Card>{children}</Card>
    </>
  );
}

function Row({ label, value, tone, wide }: { label: string; value: React.ReactNode; tone?: 'warning' | 'error'; wide?: boolean }) {
  return (
    <Stack direction="row" spacing={2} sx={{ alignItems: 'baseline', p: '10px 16px', '& + &': { borderTop: 1, borderColor: 'divider' } }}>
      <Typography sx={{ width: wide ? { xs: 112, sm: 240 } : { xs: 112, sm: 160 }, flex: 'none', color: 'text.secondary', overflowWrap: 'anywhere' }}>{label}</Typography>
      <Typography component="div" sx={{ flex: 1, minWidth: 0, overflowWrap: 'anywhere', color: tone ? `${tone}.main` : undefined }}>{value}</Typography>
    </Stack>
  );
}

function SystemBody({ s }: { s: SystemStatus }) {
  const st = s.storage;
  return (
    <>
      <Section title={t('版本')}>
        <Row label={t('版本')} value={s.commit ? t('{version}（{commit}）', { version: s.version, commit: s.commit.slice(0, 8) }) : s.version} />
        {s.buildTime && <Row label={t('构建时间')} value={s.buildTime} />}
        <Row label={t('运行时长')} value={<Tooltip title={t('启动于 {time}', { time: dateTime(s.startedAt) })}><span>{uptime(s.startedAt)}</span></Tooltip>} />
        <Row label="Go" value={s.goVersion} />
      </Section>

      <Section title={t('数据')}>
        <Row label={t('账号与空间')} value={`${t('{n} 个账号', { n: s.users })} · ${t('{n} 个空间', { n: s.workspaces })}`} />
        <Row label={t('文档')} value={s.trash ? t('{n} 份，回收站 {trash} 份', { n: s.documents, trash: s.trash }) : t('{n} 份', { n: s.documents })} />
        <Row label={t('数据库')} value={bytes(st.database)} />
        <Row label={tc('多个', '文件')} value={bytes(st.media)} />
        <Row label={t('搜索索引')} value={bytes(st.index)} />
        <Row label={t('升级前备份')} value={bytes(st.backups)} />
        <Row label={t('磁盘剩余空间')} value={bytes(st.free)} tone={st.free >= 0 && st.free < 5 * 1024 ** 3 ? 'warning' : undefined} />
      </Section>

      <Section title={t('搜索索引')}>
        <Row label={t('状态')} value={s.indexReady ? (s.indexBehind ? t('正在写入，还有 {n} 份', { n: s.indexBehind }) : t('已就绪')) : t('正在重建')} tone={s.indexReady ? undefined : 'warning'} />
        <Row label={t('已索引')} value={t('{n} 份', { n: s.indexDocuments })} />
      </Section>

      <Section title={t('后台任务')}>
        <Row label={t('排队中')} value={s.oldestQueued ? t('{n} 个，最早的排了 {wait}', { n: s.jobsQueued, wait: uptime(s.oldestQueued) }) : t('{n} 个', { n: s.jobsQueued })} />
        <Row label={t('进行中')} value={t('{n} 个', { n: s.jobsRunning })} />
        <Row label={t('最近 24 小时失败')} value={t('{n} 个', { n: s.jobsFailed })} tone={s.jobsFailed ? 'error' : undefined} />
      </Section>

      <Section title={t('外部工具')}>
        {s.tools.map((tool) => (
          <Row key={tool.name} label={tool.name} tone={tool.available ? undefined : 'warning'}
            value={<>{tool.available ? (tool.version ? `${t('可用')} · ${tool.version}` : t('可用')) : tool.version || t('不可用')}<Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>{tool.purpose}</Typography></>} />
        ))}
      </Section>

      <Section title={t('完整性检查')}>
        {!s.integrity.length && <Box sx={{ p: 2 }}><Typography sx={{ color: 'text.secondary' }}>{t('还没有检查过。每周自动检查一次，也可以在服务器上运行 kept check 立即检查。')}</Typography></Box>}
        {s.integrity.map((c) => {
          const ok = !c.missingCount && !c.orphanCount;
          return (
            <Row key={c.workspaceId} label={c.workspaceName} tone={ok ? undefined : 'error'} value={<>
              {ok ? t('{n} 份文档，没有发现问题', { n: c.documents }) : [c.missingCount && t('{n} 份文档缺少文件', { n: c.missingCount }), c.orphanCount && t('{n} 个文件没有对应的文档', { n: c.orphanCount })].filter(Boolean).join(t('；'))}
              <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>{t('检查于 {time}', { time: dateTime(c.checkedAt) })}</Typography>
              {c.missingFiles?.map((f) => (
                <Typography key={f.documentId} variant="body2" sx={{ color: 'text.secondary' }}>
                  <Link component={RouterLink} to={`/docs/${f.documentId}`} underline="hover">{f.title}</Link>{t('：')}{f.file}
                </Typography>
              ))}
              {c.orphanFiles?.map((f) => <Typography key={f} variant="body2" sx={{ color: 'text.secondary', fontFamily: fontMono, fontSize: 12 }}>{f}</Typography>)}
            </>} />
          );
        })}
      </Section>

      <WatchSection w={s.watch} />

      <BackupSection b={s.backup} />

      <SmtpSection m={s.smtp} />

      {!!Object.keys(s.settings).length && (
        <Section title={t('配置')}>
          {Object.entries(s.settings).map(([k, v]) => <Row key={k} wide label={k} value={<Box component="span" sx={{ fontFamily: fontMono, fontSize: 13 }}>{v || t('（未设置）')}</Box>} />)}
        </Section>
      )}
    </>
  );
}

function WatchSection({ w }: { w: SystemStatus['watch'] }) {
  const set = useSetWatch();
  const app = useApp();
  return (
    <Section title={t('监听目录')}>
      {!w.dir ? (
        <Box sx={{ p: 2 }}>
          <Typography sx={{ color: 'text.secondary' }}>{t('没有配置监听目录。设置环境变量 KEPT_CONSUME_DIR 后，扫描仪或其他程序放进空间文件夹的文件会自动入库。')}</Typography>
        </Box>
      ) : (
        <>
          <Row label={t('目录')} value={<>
            <Box component="span" sx={{ fontFamily: fontMono, fontSize: 13 }}>{w.dir}</Box>
            <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>{t('文件放进下面的空间文件夹后自动入库；子文件夹的名称会成为标签；“双面”文件夹里先后放入正面和反面，会合并成一份')}</Typography>
          </>} />
          {w.folders.map((f) => (
            <Row key={f.workspaceId} label={f.workspaceName} value={<Box component="span" sx={{ fontFamily: fontMono, fontSize: 13 }}>{f.name}/</Box>} />
          ))}
          <Row label={t('最近一次扫描')} tone={w.error ? 'error' : undefined} value={<>
            {w.lastScan ? relativeTime(w.lastScan) : t('还没有扫描')}
            {w.error && <Typography variant="caption" sx={{ display: 'block' }}>{w.error}</Typography>}
          </>} />
          <Row label={t('等待中')} value={t('{n} 个文件', { n: w.waiting })} />
          <Row label={t('未能入库')} tone={w.failed ? 'warning' : undefined} value={w.failed ? t('{n} 个文件，原因写在 _未能入库 里同名的 .原因.txt 中', { n: w.failed }) : t('没有')} />
          <Row label={t('同一路径')} value={
            <FormControlLabel control={<Switch checked={!!w.versions} disabled={set.isPending}
              onChange={(e) => set.mutate({ versions: e.target.checked }, { onSuccess: () => app.toast(t('已保存')), onError: (err) => app.toast(err.message) })} />}
              label={<Box><Typography variant="body2">{t('作为新版本')}</Typography><Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('文件按原来的路径和文件名再次放入时，作为上次那份文档的新版本，而不是新文档')}</Typography></Box>} />
          } />
        </>
      )}
    </Section>
  );
}

const BACKUP_STATUS: Record<string, string> = { queued: t('排队中'), running: t('正在备份'), succeeded: t('成功'), failed: t('失败') };

function BackupSection({ b }: { b: SystemStatus['backup'] }) {
  const run = useRunBackup();
  const app = useApp();
  const l = b.last;
  const busy = run.isPending || l?.status === 'queued' || l?.status === 'running';
  const action = b.dir ? (
    <Button variant="outlined" size="small" disabled={busy} onClick={() => run.mutate(undefined, { onSuccess: () => app.toast(t('已开始备份')), onError: (e) => app.toast(e.message) })}>
      {busy ? t('正在备份…') : t('立即备份')}
    </Button>
  ) : undefined;
  return (
    <Section title={t('备份')} action={action}>
      {!b.dir ? (
        <Box sx={{ p: 2 }}>
          <Typography sx={{ color: 'text.secondary' }}>{t('没有设置每天备份。可以在“实例管理 → 文件安全”里选一个目录，或设置环境变量 KEPT_BACKUP_DIR；也可以在服务器上运行 kept backup <目录>。')}</Typography>
        </Box>
      ) : (
        <>
          <Row label={t('备份目录')} value={<Box component="span" sx={{ fontFamily: fontMono, fontSize: 13 }}>{b.dir}</Box>} />
          <Row label={t('数据库快照')} value={t('{n} 份（保留最近 7 份）', { n: b.snapshots })} />
          <Row label={t('最近一次')} tone={l?.status === 'failed' ? 'error' : l?.missing ? 'warning' : undefined} value={!l ? t('还没有备份过') : <>
            {BACKUP_STATUS[l.status] ?? l.status} · {dateTime(l.at)}
            {l.status === 'succeeded' && (
              <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>
                {t('{documents} 份文档、{files} 个文件，这次复制 {copied} 个（{size}）', { documents: l.documents ?? '', files: l.files ?? '', copied: l.copied ?? '', size: bytes(l.bytes ?? 0) })}
              </Typography>
            )}
            {!!l.missing && <Typography variant="caption" sx={{ display: 'block' }}>{t('有 {n} 个文件在数据目录中缺失，没有备份', { n: l.missing })}</Typography>}
            {l.error && <Typography variant="caption" sx={{ display: 'block' }}>{l.error}</Typography>}
          </>} />
        </>
      )}
    </Section>
  );
}

const SECURITY: Record<string, string> = { starttls: 'STARTTLS', tls: t('TLS（SSL）'), none: t('不加密') };

function SmtpSection({ m }: { m: SystemStatus['smtp'] }) {
  const app = useApp();
  const me = useMe().data;
  const test = useTestSmtp();
  const [to, setTo] = useState(me?.user.email ?? '');
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);
  const send = () => {
    setResult(null);
    test.mutate(to.trim(), {
      onSuccess: () => { setResult({ ok: true, text: t('已发送到 {to}，请查收', { to: to.trim() }) }); app.toast(t('测试邮件已发送')); },
      onError: (e) => setResult({ ok: false, text: e.message }),
    });
  };
  return (
    <Section title={t('发信')}>
      {!m.configured ? (
        <Box sx={{ p: 2 }}>
          <Typography sx={{ color: 'text.secondary' }}>{t('没有配置发信。设置环境变量 KEPT_SMTP_HOST、KEPT_SMTP_FROM 等以后，成员可以通过邮件发送文档、收到提醒邮件。')}</Typography>
        </Box>
      ) : (
        <>
          <Row label={t('邮件服务器')} value={<Box component="span" sx={{ fontFamily: fontMono, fontSize: 13 }}>{m.host}:{m.port}</Box>} />
          <Row label={t('加密方式')} value={SECURITY[m.security ?? ''] ?? m.security} />
          <Row label={t('登录用户名')} value={m.username || t('不登录')} />
          <Row label={t('发件人')} value={m.from} />
          <Row label={t('附件上限')} value={t('每封邮件 {size}', { size: bytes(m.maxAttachments ?? 0) })} />
          <Stack direction="row" spacing={1.5} sx={{ alignItems: 'flex-start', p: 2, borderTop: 1, borderColor: 'divider' }}>
            <TextField size="small" label={t('测试收件人')} value={to} onChange={(e) => setTo(e.target.value)} sx={{ flex: 1, maxWidth: 360 }}
              error={result?.ok === false} helperText={result?.text ?? t('发一封测试邮件，确认设置可以正常使用')} />
            <Button variant="outlined" sx={{ mt: '4px' }} disabled={!to.trim() || test.isPending} onClick={send}>{test.isPending ? t('发送中…') : t('发送测试邮件')}</Button>
          </Stack>
        </>
      )}
    </Section>
  );
}

export function SystemPanel() {
  const status = useSystemStatus();
  return (
    <>
      <Stack direction="row" sx={{ alignItems: 'center' }}>
        <Typography variant="h4" sx={{ flex: 1 }}>{t('系统状态')}</Typography>
        <Button variant="outlined" size="small" startIcon={<RefreshCw size={16} strokeWidth={SW} />} disabled={status.isFetching} onClick={() => status.refetch()}>{t('刷新')}</Button>
      </Stack>
      <Typography sx={{ color: 'text.secondary', mt: 0.5, mb: 1 }}>{t('只有实例管理员可以查看。')}</Typography>
      {status.isPending && <Rows n={5} />}
      {status.isError && <ErrorState onRetry={() => status.refetch()} />}
      {status.data && <SystemBody s={status.data} />}
    </>
  );
}

// ---- 日志 ----

const LEVELS: [LogLevel | '', string][] = [['', t('全部级别')], ['info', t('信息及以上')], ['warn', t('警告及以上')], ['error', t('只看错误')]];
const LEVEL_CHIP: Record<string, { label: string; color: 'default' | 'warning' | 'error' }> = {
  DEBUG: { label: t('调试'), color: 'default' },
  INFO: { label: t('信息'), color: 'default' },
  WARN: { label: t('警告'), color: 'warning' },
  ERROR: { label: t('错误'), color: 'error' },
};

export function LogsPanel() {
  const [level, setLevel] = useState<LogLevel | ''>('');
  const [q, setQ] = useState('');
  const logs = useLogs({ level: level || undefined, q: q.trim() || undefined });
  const items = logs.data ?? [];
  return (
    <>
      <Stack direction="row" sx={{ alignItems: 'center' }}>
        <Typography variant="h4" sx={{ flex: 1 }}>{t('日志')}</Typography>
        <Button variant="outlined" size="small" startIcon={<RefreshCw size={16} strokeWidth={SW} />} disabled={logs.isFetching} onClick={() => logs.refetch()}>{t('刷新')}</Button>
      </Stack>
      <Typography sx={{ color: 'text.secondary', mt: 0.5, mb: 2.5 }}>{t('最近的 2000 条日志，保存在内存里，服务重启后清空；成功的访问记录不保留。完整的日志由 Docker 收集（docker logs）。')}</Typography>

      <Stack direction="row" spacing={1} sx={{ flexWrap: 'wrap', rowGap: 1, mb: 2, alignItems: 'center' }}>
        <Select size="small" value={level} displayEmpty onChange={(e) => setLevel(e.target.value as LogLevel | '')} SelectDisplayProps={{ 'aria-label': t('级别') }} sx={filterSx}>
          {LEVELS.map(([v, l]) => <MenuItem key={v} value={v}>{l}</MenuItem>)}
        </Select>
        <TextField size="small" label={t('关键字')} value={q} onChange={(e) => setQ(e.target.value)} sx={{ width: 240 }} />
      </Stack>

      {logs.isPending && <Rows />}
      {logs.isError && <ErrorState onRetry={() => logs.refetch()} />}
      {logs.isSuccess && !items.length && (
        <EmptyState icon={<ScrollText size={40} strokeWidth={SW} />} title={level || q ? t('没有符合条件的日志') : t('还没有日志')} text={level || q ? t('换一个筛选条件试试。') : undefined} />
      )}
      {!!items.length && (
        <Card>
          {items.map((e, i) => {
            const chip = LEVEL_CHIP[e.level] ?? { label: e.level, color: 'default' as const };
            const attrs = Object.entries(e.attrs);
            return (
              <Stack key={i} direction="row" spacing={1.5} sx={{ alignItems: 'flex-start', p: '8px 16px', borderTop: i ? 1 : 0, borderColor: 'divider' }}>
                <Typography variant="body2" sx={{ color: 'text.secondary', flex: 'none', fontFamily: fontMono, fontSize: 12, pt: '3px' }}>{dateTime(e.time).slice(5)}</Typography>
                <Chip size="small" label={chip.label} color={chip.color} variant={chip.color === 'default' ? 'outlined' : 'filled'} sx={{ flex: 'none' }} />
                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>{e.message}</Typography>
                  {!!attrs.length && (
                    <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary', fontFamily: fontMono, overflowWrap: 'anywhere' }}>
                      {attrs.map(([k, v]) => `${k}=${v}`).join(' ')}
                    </Typography>
                  )}
                </Box>
              </Stack>
            );
          })}
        </Card>
      )}
    </>
  );
}

// 顶栏的任务入口（v1.7.14）：图标是进度条组，有进行中的任务时横条动起来并显示数量（v1.7.17）；点开看进行中的上传、移动文档等任务的进度和最近完成的任务，底部进入任务记录（/tasks）。
// 只列有人发起的任务（v1.7.16）：收取邮件这类定时检查不在这里，在任务记录里看。
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Badge, Box, Button, Chip, IconButton, LinearProgress, Popover, Stack, Tooltip, Typography } from '@mui/material';
import { activeTasks, useRecentTasks, type Task, type TaskStatus } from '../api/system';
import { useMe } from '../api/session';
import { useRetryJob } from '../api/documents';
import { useApp } from '../store/app-store';
import { relativeTime } from '../pages/detail/time';
import { SW } from './icons';
import { t } from '../i18n';

export const TASK_STATUS: Record<TaskStatus, { label: string; color: 'default' | 'primary' | 'success' | 'error' }> = {
  queued: { label: t('排队中'), color: 'default' },
  running: { label: t('进行中'), color: 'primary' },
  succeeded: { label: t('已完成'), color: 'success' },
  failed: { label: t('失败'), color: 'error' },
};

/** 任务的进度条：知道百分比时按比例，否则来回滚动；排队时为空。 */
export function TaskProgress({ t: task }: { t: Task }) {
  if (task.status === 'queued') return <LinearProgress variant="determinate" value={0} aria-label={t('{label}排队中', { label: task.label })} />;
  return task.progress === undefined
    ? <LinearProgress aria-label={t('{label}进行中', { label: task.label })} />
    : <LinearProgress variant="determinate" value={task.progress} aria-label={`${task.label} ${task.progress}%`} />;
}

/** 失败的上传（v1.9.12）：文件还在就能“重试”，同一个任务回到排队；超过 7 天文件已清理，只能重新上传。 */
export function RetryUpload({ id, retryable, expired, dense }: { id: number; retryable?: boolean; expired?: boolean; dense?: boolean }) {
  const retry = useRetryJob();
  const app = useApp();
  if (expired) return <Typography sx={{ fontSize: 12, color: 'text.secondary', mt: 0.25 }}>{t('文件已清理（失败的上传保留 7 天），请重新上传')}</Typography>;
  if (!retryable) return null;
  return (
    <Button size="small" disabled={retry.isPending} sx={{ mt: dense ? 0.25 : 0.5, ml: -0.75 }}
      onClick={(e) => { e.stopPropagation(); retry.mutate(id, { onSuccess: () => app.toast(t('已重新开始处理')), onError: (err) => app.toast(err.message) }); }}>
      {t('重试')}
    </Button>
  );
}

function Item({ t: task, onOpen }: { t: Task; onOpen: (task: Task) => void }) {
  const running = task.status === 'queued' || task.status === 'running';
  return (
    <Box onClick={() => onOpen(task)} sx={{ px: 2, py: 1.25, cursor: task.documentId ? 'pointer' : 'default', '&:hover': { bgcolor: task.documentId ? 'action.hover' : undefined } }}>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
        <Typography noWrap sx={{ flex: 1, minWidth: 0, fontSize: 13 }}>
          {task.label}{task.summary && <Box component="span" sx={{ color: 'text.secondary' }}> · {task.summary}</Box>}
        </Typography>
        {running
          ? <Typography sx={{ fontSize: 12, color: 'text.secondary', flex: 'none' }}>{task.progress !== undefined ? `${task.progress}%` : TASK_STATUS[task.status].label}</Typography>
          : <Chip size="small" label={TASK_STATUS[task.status].label} color={TASK_STATUS[task.status].color} sx={{ flex: 'none' }} />}
      </Stack>
      {running && <Box sx={{ mt: 0.75 }}><TaskProgress t={task} /></Box>}
      {task.error && <Typography sx={{ fontSize: 12, color: 'error.main', mt: 0.5, overflowWrap: 'anywhere' }}>{task.error}</Typography>}
      {task.status === 'failed' && <RetryUpload id={task.id} retryable={task.retryable} expired={task.expired} dense />}
      <Typography sx={{ fontSize: 12, color: 'text.secondary', mt: 0.25 }}>{[task.creatorName, relativeTime(task.createdAt)].filter(Boolean).join(' · ')}</Typography>
    </Box>
  );
}

// 进度条组（lucide chart-gantt）的三根横条，pathLength=1 方便按比例画出来
const BARS = ['M10 6h8', 'M8 11h7', 'M12 16h6'];

/**
 * 任务图标（v1.7.17）：坐标轴加三根错开的横条，和面板里的一排进度条对应。
 * busy 时横条变蓝，依次从左往右填满、循环，表示“正在做”；系统要求减少动态效果时不动，只保持蓝色。
 */
export function TaskIcon({ busy }: { busy: boolean }) {
  return (
    <Box component="svg" viewBox="0 0 24 24" aria-hidden data-busy={busy || undefined}
      sx={{ width: 20, height: 20, fill: 'none', stroke: 'currentColor', strokeWidth: SW, strokeLinecap: 'round', strokeLinejoin: 'round', display: 'block',
        '@keyframes kept-task-bar': { '0%': { strokeDashoffset: 1 }, '55%, 100%': { strokeDashoffset: 0 } } }}>
      <path d="M3 3v16a2 2 0 0 0 2 2h16" />
      {BARS.map((d, i) => (
        <Box component="path" key={d} d={d} pathLength={1}
          sx={busy ? {
            stroke: (theme) => theme.palette.primary.main, strokeDasharray: 1,
            animation: `kept-task-bar 1.6s ${i * 0.2}s ease-in-out infinite both`,
            '@media (prefers-reduced-motion: reduce)': { animation: 'none', strokeDashoffset: 0 },
          } : undefined} />
      ))}
    </Box>
  );
}

export function TaskButton() {
  const me = useMe().data;
  const nav = useNavigate();
  const tasks = useRecentTasks(me?.workspace.id);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const items = tasks.data ?? [];
  const active = activeTasks(items);
  const recent = items.filter((x) => !active.includes(x)).slice(0, 5);
  const close = () => setAnchor(null);
  const open = (task: Task) => { if (task.documentId) { close(); nav(`/docs/${task.documentId}`); } };
  const label = active.length ? t('任务，{n} 个进行中', { n: active.length }) : t('任务');
  return (
    <>
      <Tooltip title={label}>
        <IconButton aria-label={label} onClick={(e) => setAnchor(e.currentTarget)}>
          <Badge badgeContent={active.length} color="primary" max={99}><TaskIcon busy={active.length > 0} /></Badge>
        </IconButton>
      </Tooltip>
      <Popover open={!!anchor} anchorEl={anchor} onClose={close} anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }} transformOrigin={{ vertical: 'top', horizontal: 'right' }}
        slotProps={{ paper: { sx: { width: 360, maxWidth: 'calc(100vw - 32px)', borderRadius: 3 } } }}>
        <Box role="dialog" aria-label={t('任务')}>
          <Typography variant="h6" sx={{ px: 2, pt: 1.5, pb: 1 }}>{t('任务')}</Typography>
          <Box sx={{ maxHeight: 420, overflowY: 'auto' }}>
            {active.length > 0 && <>
              <Typography variant="caption" sx={{ px: 2, color: 'text.secondary' }}>{t('进行中')}</Typography>
              {active.map((task) => <Item key={task.id} t={task} onOpen={open} />)}
            </>}
            {recent.length > 0 && <>
              <Typography variant="caption" sx={{ display: 'block', px: 2, pt: active.length ? 1 : 0, color: 'text.secondary' }}>{t('最近')}</Typography>
              {recent.map((task) => <Item key={task.id} t={task} onOpen={open} />)}
            </>}
            {tasks.isSuccess && !items.length && <Typography variant="body2" sx={{ px: 2, pb: 1.5, color: 'text.secondary' }}>{t('最近 7 天没有任务。上传文档、更换存储后，进度会显示在这里；收取邮件这类定时检查在任务记录里看。')}</Typography>}
          </Box>
          <Box sx={{ borderTop: 1, borderColor: 'divider', px: 1, py: 0.5 }}>
            <Button onClick={() => { close(); nav('/tasks'); }}>{t('查看全部任务')}</Button>
          </Box>
        </Box>
      </Popover>
    </>
  );
}

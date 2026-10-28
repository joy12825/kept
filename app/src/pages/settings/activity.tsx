// 设置 → 活动记录（空间管理员）与账号活动（实例管理员）：谁在什么时候做了什么
import { useState } from 'react';
import { Box, Button, Card, Chip, MenuItem, Select, Skeleton, Stack, Tooltip, Typography } from '@mui/material';
import { History } from 'lucide-react';
import { useAudit, type AuditFilter, type AuditScope } from '../../api/audit';
import { useMembers } from '../../api/members';
import { useMe } from '../../api/session';
import { EmptyState, ErrorState } from '../../components/common';
import { SW } from '../../components/icons';
import { t } from '../../i18n';

const COPY: Record<AuditScope, { title: string; help: string }> = {
  workspace: { title: t('活动记录'), help: t('这个空间里谁在什么时候做了什么。只有空间管理员可以查看，记录保留一年。') },
  instance: { title: t('账号活动'), help: t('账号的创建、停用、重置密码等操作。只有实例管理员可以查看，不包含任何空间里的记录。') },
};

const ACTION_OPTIONS: [string, string][] = [['', t('全部类型')], ['workspace.', t('空间')], ['member.', t('成员')], ['device.', t('手机')]];
const RANGE_OPTIONS: [number, string][] = [[0, t('全部时间')], [7, t('最近 {n} 天', { n: 7 })], [30, t('最近 {n} 天', { n: 30 })]];
const DAY = 24 * 60 * 60 * 1000;

const pad = (n: number) => String(n).padStart(2, '0');
const exactTime = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;

/** “3 分钟前”这样的相对时间；超过 30 天显示日期。 */
function relativeTime(d: Date, now = Date.now()): string {
  const minutes = Math.floor((now - d.getTime()) / 60_000);
  if (minutes < 1) return t('刚刚');
  if (minutes < 60) return t('{n} 分钟前', { n: minutes });
  if (minutes < 24 * 60) return t('{n} 小时前', { n: Math.floor(minutes / 60) });
  if (minutes < 30 * 24 * 60) return t('{n} 天前', { n: Math.floor(minutes / (24 * 60)) });
  return exactTime(d).slice(0, 10);
}

const filterSx = { minWidth: 132, fontSize: 14 } as const;

export function ActivityPanel({ scope }: { scope: AuditScope }) {
  const me = useMe().data;
  const members = useMembers(scope === 'workspace' ? me?.workspace.id : undefined);
  const [action, setAction] = useState('');
  const [actorId, setActorId] = useState(0);
  const [range, setRange] = useState<{ days: number; from?: string }>({ days: 0 });

  const filter: AuditFilter = { action: action || undefined, actorId: actorId || undefined, from: range.from };
  const audit = useAudit(scope, filter);
  const items = audit.data?.pages.flatMap((p) => p.items) ?? [];
  const filtered = !!action || !!actorId || !!range.days;

  return (
    <>
      <Typography variant="h4">{COPY[scope].title}</Typography>
      <Typography sx={{ color: 'text.secondary', mt: 0.5, mb: 2.5 }}>{COPY[scope].help}</Typography>

      <Stack direction="row" spacing={1} sx={{ flexWrap: 'wrap', rowGap: 1, mb: 2 }}>
        {scope === 'workspace' && (
          <>
            <Select size="small" value={actorId} onChange={(e) => setActorId(Number(e.target.value))} SelectDisplayProps={{ 'aria-label': t('成员') }} sx={filterSx}>
              <MenuItem value={0}>{t('全部成员')}</MenuItem>
              {members.data?.map((m) => <MenuItem key={m.userId} value={m.userId}>{m.displayName}</MenuItem>)}
            </Select>
            <Select size="small" value={action} displayEmpty onChange={(e) => setAction(e.target.value)} SelectDisplayProps={{ 'aria-label': t('操作类型') }} sx={filterSx}>
              {ACTION_OPTIONS.map(([v, l]) => <MenuItem key={v} value={v}>{l}</MenuItem>)}
            </Select>
          </>
        )}
        <Select
          size="small" value={range.days} SelectDisplayProps={{ 'aria-label': t('时间') }} sx={filterSx}
          onChange={(e) => { const days = Number(e.target.value); setRange({ days, from: days ? new Date(Date.now() - days * DAY).toISOString() : undefined }); }}
        >
          {RANGE_OPTIONS.map(([v, l]) => <MenuItem key={v} value={v}>{l}</MenuItem>)}
        </Select>
      </Stack>

      {audit.isPending && (
        <Stack spacing={1} aria-busy>
          {[100, 92, 84, 96].map((w) => <Skeleton key={w} variant="rounded" height={52} width={`${w}%`} />)}
        </Stack>
      )}
      {audit.isError && <ErrorState onRetry={() => audit.refetch()} />}
      {audit.isSuccess && !items.length && (
        <EmptyState
          icon={<History size={40} strokeWidth={SW} />}
          title={filtered ? t('没有符合条件的记录') : t('还没有活动记录')}
          text={filtered ? t('换一个筛选条件试试。') : scope === 'workspace' ? t('添加成员、修改角色、修改空间设置后，会记录在这里。') : t('创建、停用账号或重置密码后，会记录在这里。')}
        />
      )}
      {!!items.length && (
        <Card>
          {items.map((e, i) => {
            const at = new Date(e.createdAt);
            return (
              <Stack key={e.id} direction="row" spacing={2} sx={{ alignItems: 'center', p: '12px 16px', borderTop: i ? 1 : 0, borderColor: 'divider' }}>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <Typography sx={{ overflowWrap: 'anywhere' }}>{e.summary}</Typography>
                </Box>
                {e.actorVia === 'token' && <Chip size="small" label={t('通过 API Token')} />}
                {e.actorVia === 'device' && <Chip size="small" label={t('通过手机')} />}
                {e.actorVia === 'system' && <Chip size="small" label={t('系统')} />}
                <Tooltip title={exactTime(at)}>
                  <Typography variant="body2" sx={{ color: 'text.secondary', flex: 'none', minWidth: 72, textAlign: 'right' }}>{relativeTime(at)}</Typography>
                </Tooltip>
              </Stack>
            );
          })}
        </Card>
      )}
      {audit.hasNextPage && (
        <Box sx={{ textAlign: 'center', mt: 2 }}>
          <Button variant="outlined" disabled={audit.isFetchingNextPage} onClick={() => audit.fetchNextPage()}>{t('加载更多')}</Button>
        </Box>
      )}
    </>
  );
}

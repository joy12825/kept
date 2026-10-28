// 详情页的修改历史（来自活动记录）：谁在什么时候改了什么，修改前 → 修改后。
import { Box, Button, Stack, Typography } from '@mui/material';
import { useHistory } from '../../api/documents';
import { locale, t } from '../../i18n';
import { relativeTime } from './time';

const show = (v: unknown) => (v === true ? t('是') : v === false ? t('否') : v === '' || v === null || v === undefined ? t('（空）') : String(v));

export function History({ docId }: { docId: number }) {
  const h = useHistory(docId);
  const items = h.data?.pages.flatMap((p) => p.items) ?? [];
  if (h.isPending) return <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('正在读取…')}</Typography>;
  if (!items.length) return <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('还没有修改记录。')}</Typography>;
  return (
    <Stack spacing={1.25}>
      {items.map((e) => (
        <Box key={e.id}>
          <Typography sx={{ fontSize: 13 }}>{e.summary}</Typography>
          <Typography variant="caption" sx={{ color: 'text.secondary' }} title={new Date(e.createdAt).toLocaleString(locale())}>{relativeTime(e.createdAt)}</Typography>
          {Object.entries(e.changes).map(([k, v]) => {
            const [before, after] = v as [unknown, unknown];
            return (
              <Typography key={k} sx={{ fontSize: 12, color: 'text.secondary', pl: 1.5 }}>
                {t('{field}：{before} → {after}', { field: k, before: show(before), after: show(after) })}
              </Typography>
            );
          })}
        </Box>
      ))}
      {h.hasNextPage && <Button size="small" sx={{ alignSelf: 'flex-start' }} disabled={h.isFetchingNextPage} onClick={() => void h.fetchNextPage()}>{t('加载更早的记录')}</Button>}
    </Stack>
  );
}

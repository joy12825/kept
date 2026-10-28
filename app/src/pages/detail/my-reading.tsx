// 详情页的“我的阅读”（阶段 27）：状态、评分、日期、进度和批注条数，只有自己看得到。
import { Link as RouterLink, useNavigate } from 'react-router-dom';
import { Box, Button, Link, Rating, Stack, ToggleButton, ToggleButtonGroup, Typography } from '@mui/material';
import { STATUS_LABEL, useReadingProgress, useSetReadingState, type ReadingStatePatch, type ReadingStatus } from '../../api/reading';
import { DateInput, FieldRow } from '../../components/doc-fields';
import { t, tc } from '../../i18n';
import { useApp } from '../../store/app-store';

export function MyReading({ docId, readable }: { docId: number; readable: boolean }) {
  const app = useApp();
  const nav = useNavigate();
  const progress = useReadingProgress(docId);
  const save = useSetReadingState(docId);
  const p = progress.data;
  if (!p) return null;
  const set = (patch: ReadingStatePatch) => save.mutate(patch, { onError: (e) => app.toast(e.message) });
  const pct = Math.round(p.percent * 100);
  return (
    <Box component="section" aria-label={t('我的阅读')}>
      <Stack direction="row" sx={{ alignItems: 'baseline', mb: 0.5, gap: 1 }}>
        <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('我的阅读')}</Typography>
        <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('只有你自己看得到')}</Typography>
      </Stack>
      <FieldRow label={t('状态')}>
        <ToggleButtonGroup size="small" exclusive value={p.status || null} aria-label={t('阅读状态')}
          onChange={(_, v: ReadingStatus | null) => set({ status: v ?? '' })}>
          {(Object.entries(STATUS_LABEL) as [ReadingStatus, string][]).map(([k, l]) => <ToggleButton key={k} value={k} sx={{ px: 1.5 }}>{l}</ToggleButton>)}
        </ToggleButtonGroup>
      </FieldRow>
      <FieldRow label={t('评分')}>
        <Rating value={p.rating || null} onChange={(_, v) => set({ rating: v ?? 0 })} aria-label={t('评分')} />
      </FieldRow>
      {(p.status === 'reading' || p.status === 'finished' || p.startedOn) && (
        <FieldRow label={tc('开始日期', '开始')} top>
          <DateInput key={`s-${p.startedOn}`} label={t('开始读的日期')} value={p.startedOn} clearable onChange={(v) => set({ startedOn: v ?? '' })} />
        </FieldRow>
      )}
      {(p.status === 'finished' || p.finishedOn) && (
        <FieldRow label={t('读完')} top>
          <DateInput key={`f-${p.finishedOn}`} label={t('读完的日期')} value={p.finishedOn} clearable onChange={(v) => set({ finishedOn: v ?? '' })} />
        </FieldRow>
      )}
      <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center', mt: 0.5, flexWrap: 'wrap' }}>
        {p.position && <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('读到 {pct}%', { pct })}</Typography>}
        {readable && <Button size="small" onClick={() => nav(`/read/${docId}`, { state: { from: `/docs/${docId}` } })}>{p.position ? t('继续阅读') : t('开始阅读')}</Button>}
        {p.highlightCount > 0 && <Link component={RouterLink} to={`/highlights?doc=${docId}`} variant="body2">{t('批注 {n} 条', { n: p.highlightCount })}</Link>}
      </Stack>
    </Box>
  );
}

// 我的批注（阶段 27）：自己在这个空间里的全部高亮和批注，按书分组；可以搜索、按颜色筛选，点“打开”在阅读器中跳到那里。
// 阶段 33：顶部的复习卡片入口，每本书旁“做成复习卡片”。
import { useMemo, useState } from 'react';
import { Link as RouterLink, useNavigate, useSearchParams } from 'react-router-dom';
import { Box, Button, Chip, Link, Stack, TextField, ToggleButton, ToggleButtonGroup, Typography } from '@mui/material';
import { colorCss, HIGHLIGHT_COLORS, useSearchHighlights, type Highlight, type HighlightColor } from '../../api/reading';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import { locatorLabel } from '../reader/highlight-tools';
import { dateTime } from '../detail/time';
import { MakeCardsButton, ReviewBar } from '@ee';
import { t } from '../../i18n';

export default function HighlightsPage() {
  const nav = useNavigate();
  const [params, setParams] = useSearchParams();
  const doc = Number(params.get('doc')) || undefined;
  const [text, setText] = useState('');
  const [q, setQ] = useState('');
  const [color, setColor] = useState<HighlightColor | ''>('');
  const list = useSearchHighlights(q, color, doc);
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  // 按书分组，组的顺序按每本书最新的一条
  const groups = useMemo(() => {
    const m = new Map<number, { title: string; items: Highlight[] }>();
    for (const h of items) {
      const g = m.get(h.documentId) ?? { title: h.documentTitle ?? '', items: [] };
      g.items.push(h);
      m.set(h.documentId, g);
    }
    return [...m.entries()];
  }, [items]);
  const open = (h: Highlight) => nav(`/read/${h.documentId}?at=${encodeURIComponent(h.locator)}`, { state: { from: `/highlights${doc ? `?doc=${doc}` : ''}` } });

  return (
    <Box sx={{ px: 3, pt: 0.5, pb: 4, maxWidth: 880 }}>
      <Typography variant="h4" component="h1">{t('我的批注')}</Typography>
      <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5 }}>{t('你在这个空间的书里划的线和写的批注，只有你自己看得到。')}</Typography>
      <Box sx={{ mt: 2.5 }}><ReviewBar /></Box>
      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} sx={{ mb: 3, alignItems: { sm: 'center' } }}>
        <TextField size="small" label={t('搜索摘录和批注')} value={text} onChange={(e) => setText(e.target.value)} sx={{ width: { sm: 280 } }}
          onKeyDown={(e) => { if (e.key === 'Enter') setQ(text.trim()); }} onBlur={() => setQ(text.trim())} />
        <ToggleButtonGroup size="small" exclusive value={color} onChange={(_, v) => setColor(v ?? '')} aria-label={t('按颜色筛选')}>
          <ToggleButton value="">{t('全部')}</ToggleButton>
          {HIGHLIGHT_COLORS.map((c) => (
            <ToggleButton key={c.value} value={c.value} aria-label={t('{color}色', { color: c.label })}><Box sx={{ width: 14, height: 14, borderRadius: '50%', bgcolor: c.css }} /></ToggleButton>
          ))}
        </ToggleButtonGroup>
        {doc && <Chip label={t('只看《{title}》', { title: groups[0]?.[1].title ?? t('这本书') })} onDelete={() => setParams({})} />}
      </Stack>
      {list.isPending ? <LoadingState />
        : list.isError ? <ErrorState onRetry={() => void list.refetch()} />
          : items.length === 0
            ? <EmptyState title={q || color ? t('没有找到批注') : t('还没有批注')} text={q || color ? t('换个关键词或颜色试试。') : t('在阅读器里选中文字，就可以高亮、写批注。')} />
            : groups.map(([docId, g]) => (
              <Box key={docId} component="section" aria-label={g.title} sx={{ mb: 3.5 }}>
                <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                  <Link component={RouterLink} to={`/docs/${docId}`} variant="h6" underline="hover" sx={{ color: 'text.primary', flex: 1 }}>{g.title}</Link>
                  <MakeCardsButton docId={docId} />
                </Stack>
                <Stack spacing={1.5} sx={{ mt: 1 }}>
                  {g.items.map((h) => (
                    <Box key={h.id} sx={{ borderLeft: 4, borderColor: colorCss(h.color), pl: 1.5, py: 0.25 }}>
                      {h.quote && <Typography sx={{ whiteSpace: 'pre-wrap' }}>{h.quote}</Typography>}
                      {h.note && <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5, whiteSpace: 'pre-wrap' }}>{h.note}</Typography>}
                      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mt: 0.5 }}>
                        <Typography variant="caption" sx={{ color: 'text.secondary' }}>{[locatorLabel(h.locator), dateTime(h.createdAt)].filter(Boolean).join(' · ')}</Typography>
                        <Button size="small" onClick={() => open(h)}>{t('打开')}</Button>
                      </Stack>
                    </Box>
                  ))}
                </Stack>
              </Box>
            ))}
      {list.hasNextPage && <Button onClick={() => void list.fetchNextPage()} disabled={list.isFetchingNextPage}>{t('加载更多')}</Button>}
    </Box>
  );
}

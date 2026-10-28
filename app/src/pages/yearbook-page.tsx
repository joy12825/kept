// 年度回顾（UI 设计规范 5.7）：开篇数字（固定句式，不用 AI）与上一年的对比、抽屉管家的叙述（注明由 AI 生成，v0.5）、近五年、每月文档、各抽屉（份数加未入抽屉等于全年总数）、
// 用途标签小结、大事记（规则可以编辑）、存储占用。导出 PDF 用浏览器打印，打印时隐藏应用的导航。
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Box, Button, ButtonBase, GlobalStyles, IconButton, Skeleton, Stack, Typography, useMediaQuery, type Theme } from '@mui/material';
import { ChevronLeft, ChevronRight, CircleDashed, Download } from 'lucide-react';
import { useYearbook, useYears, type Yearbook } from '../api/drawers';
import { ErrorState } from '../components/common';
import { MonthsChart } from '../components/charts-lazy';
import { DrawerIcon, SW } from '../components/icons';
import { useApp } from '../store/app-store';
import { moneyInt } from '../utils/facts';
import { useRole } from '../api/workspaces';
import { BooksSection, compareText, Delta, HistoryBars, MilestoneRulesDialog, monthName, NarrativeSection, StorageSection } from './yearbook-extras';
import { t } from '../i18n';

const printCss = {
  '@media print': {
    '.kept-chrome, .MuiSnackbar-root': { display: 'none !important' },
    body: { background: '#fff' },
    '.yearbook': { padding: '0 !important' },
    '.yearbook-actions': { display: 'none !important' },
  },
};

export default function YearbookPage() {
  const years = useYears();
  const [year, setYear] = useState<number>();
  useEffect(() => { if (years.data && year === undefined) setYear(years.data[0]); }, [years.data, year]);
  const yb = useYearbook(year);
  // 今年还没有文档时，第一次打开改看最近有文档的一年
  const [fellBack, setFellBack] = useState(false);
  useEffect(() => {
    if (fellBack || !yb.data || !years.data || year !== years.data[0]) return;
    setFellBack(true);
    if (yb.data.documents === 0 && years.data.length > 1) setYear(years.data[1]);
  }, [yb.data, years.data, year, fellBack]);
  const list = years.data ?? [];
  const idx = year === undefined ? -1 : list.indexOf(year);
  const shown = list.length > 5 ? list.slice(Math.max(0, Math.min(idx - 2, list.length - 5)), Math.max(0, Math.min(idx - 2, list.length - 5)) + 5) : list;
  return (
    <Box className="yearbook" sx={{ px: 3, pt: 1, pb: 12 }}>
      <GlobalStyles styles={printCss} />
      <Stack className="yearbook-actions" direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
        <Box sx={{ flex: 1 }} />
        <IconButton aria-label={t('上一年')} disabled={idx < 0 || idx >= list.length - 1} onClick={() => setYear(list[idx + 1])}><ChevronLeft size={18} strokeWidth={SW} /></IconButton>
        {[...shown].reverse().map((v) => (
          <Button key={v} size="small" aria-current={v === year ? 'true' : undefined} onClick={() => setYear(v)}
            sx={{ color: v === year ? 'text.primary' : 'text.secondary', fontWeight: v === year ? 600 : 400, minWidth: 0 }}>{v}</Button>
        ))}
        <IconButton aria-label={t('下一年')} disabled={idx <= 0} onClick={() => setYear(list[idx - 1])}><ChevronRight size={18} strokeWidth={SW} /></IconButton>
        <Box sx={{ flex: 1, display: 'flex', justifyContent: 'flex-end' }}>
          <Button variant="outlined" size="small" startIcon={<Download size={16} strokeWidth={SW} />} disabled={!yb.data} onClick={() => window.print()}>{t('导出 PDF')}</Button>
        </Box>
      </Stack>
      {years.error || yb.error ? <ErrorState onRetry={() => { void years.refetch(); void yb.refetch(); }} />
        : !yb.data ? <Box sx={{ mt: 5 }}><Skeleton width={160} height={72} /><Skeleton width={320} /><Skeleton height={220} /></Box>
          : <Review y={yb.data} onPick={setYear} />}
    </Box>
  );
}

function Review({ y, onPick }: { y: Yearbook; onPick: (year: number) => void }) {
  const app = useApp();
  const role = useRole();
  const [rulesOpen, setRulesOpen] = useState(false);
  const prevDrawer = new Map((y.previous?.drawers ?? []).map((d) => [d.id, d]));
  const prevTag = new Map((y.previous?.tags ?? []).map((tg) => [tg.id, tg]));
  const compare = compareText(y);
  const nav = useNavigate();
  const wide = useMediaQuery((th: Theme) => th.breakpoints.up('lg'));
  const asOf = y.asOf ? t('截至 {month} 月 {d} 日', { month: monthName(Number(y.asOf.slice(5, 7))), d: Number(y.asOf.slice(8, 10)) }) : '';
  const facts = [
    t('新增往来方 {n} 家', { n: y.newCorrespondents }),
    y.topType ? t('{name}最多（{n} 份）', { name: y.topType.name, n: y.topType.count }) : '',
    y.busiestMonth ? t('{month} 月最忙', { month: monthName(y.busiestMonth) }) : '',
  ].filter(Boolean).join(' · ');
  const openFiles = (drawer: number[]) => {
    app.setDocList('files', (l) => ({ ...l, q: '', f: { drawer, type: [], correspondent: [], tag: [], year: [String(y.year)] }, extra: undefined, sel: [] }));
    nav('/files');
  };
  const openDrawer = (id: number) => {
    app.setDocList(`drawer:${id}`, (l) => ({ ...l, f: { ...l.f, year: [String(y.year)] }, sel: [] }));
    nav(`/drawers/${id}`);
  };
  // 句子里的份数用大字：找到数字的位置拆开
  const total = t('这一年，你整理了 {n} 份文档', { n: y.documents });
  const at = total.indexOf(String(y.documents));
  const row = { width: '100%', display: 'flex', gap: 1.5, minHeight: 56, px: 1, borderBottom: 1, borderColor: 'divider', justifyContent: 'flex-start', alignItems: 'center', '&:hover': { bgcolor: 'action.hover' } };
  return (
    <>
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0,1fr)', lg: 'minmax(0,5fr) minmax(0,7fr)' }, gap: { xs: 5, lg: 8 }, mt: 5, alignItems: 'start' }}>
        <Box>
          <Typography variant="h1" component="h1">{y.year}</Typography>
          {asOf && <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5 }}>{asOf}</Typography>}
          <Typography sx={{ fontSize: 16, mt: 3 }}>{total.slice(0, at)}<Box component="span" sx={{ fontSize: 28, fontWeight: 500 }}>{y.documents}</Box>{total.slice(at + String(y.documents).length)}</Typography>
          {y.documents > 0 && <Typography sx={{ color: 'text.secondary', mt: 0.75 }}>{facts}</Typography>}
          {compare && <Typography sx={{ color: 'text.secondary', mt: 0.5 }}>{compare}</Typography>}
          {y.booksFinished > 0 && <Typography sx={{ color: 'text.secondary', mt: 0.5 }}>{t('读完了 {n} 本书', { n: y.booksFinished })}</Typography>}
          <NarrativeSection y={y} canWrite={role.canWrite} />
          <HistoryBars y={y} onPick={onPick} />
        </Box>
        <Box sx={{ p: { lg: 3 }, borderRadius: 4, bgcolor: { lg: 'surface.containerLow' } }}>
          <Typography variant="h6" sx={{ color: 'text.secondary', mb: 1.5 }}>{t('每月文档')}<Typography component="span" variant="caption" sx={{ ml: 1 }}>{t('按文档日期')}</Typography></Typography>
          <MonthsChart months={y.months} height={wide ? 220 : 140} />
        </Box>
      </Box>
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0,1fr)', lg: 'minmax(0,7fr) minmax(0,5fr)' }, gap: { xs: 0, lg: 8 }, mt: 6, alignItems: 'start' }}>
        <Box component="section" aria-label={t('各抽屉')}>
          <Typography variant="h5" sx={{ mb: 1 }}>{t('各抽屉')}</Typography>
          {y.drawers.map((D) => (
            <ButtonBase key={D.id} onClick={() => openDrawer(D.id)} sx={row}>
              <DrawerIcon drawer={D} size={28} />
              <Typography noWrap sx={{ width: 72, fontWeight: 500, textAlign: 'left' }}>{D.name}</Typography>
              <Typography sx={{ width: 84, textAlign: 'left' }}>{t('{n} 份', { n: D.count })}<Delta now={D.count} before={y.previous ? prevDrawer.get(D.id)?.count ?? 0 : undefined} /></Typography>
              <Typography variant="body2" sx={{ color: 'text.secondary', flex: 1, textAlign: 'left' }}>{D.sums.map((s, i) => {
                const before = prevDrawer.get(D.id)?.sums.find((p) => p.label === s.label);
                return <span key={i}>{i > 0 && ' · '}{s.label} {s.money ? moneyInt(s.total) : s.total}{before && <Delta now={Number(s.total)} before={Number(before.total)} money={s.money} />}</span>;
              })}</Typography>
              <ChevronRight size={18} strokeWidth={SW} />
            </ButtonBase>
          ))}
          {y.unfiled > 0 && (
            <ButtonBase onClick={() => openFiles([0])} sx={row}>
              <Box sx={{ width: 28, display: 'flex', justifyContent: 'center', color: 'text.secondary' }}><CircleDashed size={18} strokeWidth={SW} /></Box>
              <Typography sx={{ width: 72, fontWeight: 500, textAlign: 'left', whiteSpace: 'nowrap' }}>{t('未入抽屉')}</Typography>
              <Typography sx={{ width: 60, textAlign: 'left' }}>{t('{n} 份', { n: y.unfiled })}</Typography>
              <Box sx={{ flex: 1 }} /><ChevronRight size={18} strokeWidth={SW} />
            </ButtonBase>
          )}
          {!y.drawers.length && !y.unfiled && <Typography sx={{ color: 'text.secondary' }}>{t('这一年还没有文档。')}</Typography>}
        </Box>
        <Box>
          <Box component="section" aria-label={t('用途标签')} sx={{ mt: { xs: 5, lg: 0 } }}>
            <Typography variant="h5" sx={{ mb: 1 }}>{t('用途标签')}</Typography>
            {y.tags.map((tg) => (
              <Stack key={tg.id} direction="row" spacing={1} sx={{ alignItems: 'center', minHeight: 48, px: 1, borderBottom: 1, borderColor: 'divider' }}>
                <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: tg.color, printColorAdjust: 'exact' }} />
                <Typography sx={{ minWidth: 48, fontWeight: 500 }}>{tg.name}</Typography>
                <Typography sx={{ color: 'text.secondary' }}>
                  {t('{n} 份', { n: tg.count })}<Delta now={tg.count} before={y.previous ? prevTag.get(tg.id)?.count ?? 0 : undefined} />
                  {tg.amount && Number(tg.amount) ? <>{t('，合计 {amount}', { amount: moneyInt(tg.amount) })}<Delta now={Number(tg.amount)} before={prevTag.get(tg.id)?.amount ? Number(prevTag.get(tg.id)!.amount) : undefined} money /></> : ''}
                </Typography>
              </Stack>
            ))}
            {!y.tags.length && <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('在目录里编辑标签，勾选“在年度回顾中显示”，这里会显示它这一年的份数和金额合计。')}</Typography>}
          </Box>
          {(y.events.length > 0 || role.isOwner) && (
            <Box component="section" aria-label={t('大事记')} sx={{ mt: 5 }}>
              <Stack direction="row" sx={{ alignItems: 'center', mb: 1 }}>
                <Typography variant="h5" sx={{ flex: 1 }}>{t('大事记')}</Typography>
                {role.isOwner && <Button className="yearbook-actions" size="small" onClick={() => setRulesOpen(true)}>{t('规则')}</Button>}
              </Stack>
              {!y.events.length && <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('这一年没有符合大事记规则的文档。')}</Typography>}
              {y.events.map((ev, i) => (
                <Stack key={i} direction="row" spacing={2.5} sx={{ alignItems: 'center', minHeight: 48, px: 1, borderBottom: 1, borderColor: 'divider', cursor: ev.documentId || ev.drawerId ? 'pointer' : 'default' }}
                  onClick={() => (ev.documentId ? nav(`/docs/${ev.documentId}`) : ev.drawerId ? nav(`/drawers/${ev.drawerId}`) : undefined)}>
                  <Typography variant="body2" sx={{ color: 'text.secondary', width: 44, flex: 'none' }}>{ev.date}</Typography>
                  <Typography>{ev.text}</Typography>
                </Stack>
              ))}
            </Box>
          )}
          <BooksSection y={y} />
          <StorageSection y={y} />
        </Box>
      </Box>
      <MilestoneRulesDialog open={rulesOpen} onClose={() => setRulesOpen(false)} />
    </>
  );
}

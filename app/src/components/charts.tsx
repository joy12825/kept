// 图表（v1.3.6，MUI X Charts）：年度回顾的每月文档、近五年，目录条目的每年文档数。
// 只经 charts-lazy.tsx 按需加载，不放进首屏的包里。颜色取自主题，深色模式与打印跟着走。
import { alpha, useTheme } from '@mui/material';
import { BarChart } from '@mui/x-charts/BarChart';
import { LineChart } from '@mui/x-charts/LineChart';
import { SparkLineChart } from '@mui/x-charts/SparkLineChart';
import { t } from '../i18n';

const fen = (v: number | null) => t('{n} 份', { n: v ?? 0 });
// 纵轴只用整数刻度（份数没有半份）：最多 5 个，顶端取到刻度上
function intTicks(max: number) {
  const step = Math.max(1, Math.ceil(max / 4));
  const top = Math.max(step, Math.ceil(max / step) * step);
  return { ticks: Array.from({ length: top / step + 1 }, (_, i) => i * step), top };
}

/** 每月文档：12 根柱子，横轴 1–12 月，悬停看份数。 */
export function MonthsChart({ months, height }: { months: number[]; height: number }) {
  const theme = useTheme();
  const { ticks, top } = intTicks(Math.max(...months));
  return (
    <BarChart height={height} hideLegend borderRadius={4} grid={{ horizontal: true }}
      margin={{ top: 8, right: 0, bottom: 0, left: 0 }}
      xAxis={[{ scaleType: 'band', data: months.map((_, i) => i + 1), valueFormatter: (m: number, c) => (c.location === 'tooltip' ? t('{m} 月', { m }) : String(m)),
        disableLine: true, disableTicks: true, categoryGapRatio: 0.35 }]}
      yAxis={[{ width: 28, min: 0, max: top, tickInterval: ticks, valueFormatter: (v: number) => String(Math.round(v)), disableLine: true, disableTicks: true }]}
      series={[{ data: months, color: alpha(theme.palette.primary.main, 0.75), valueFormatter: fen, label: t('文档') }]}
      sx={{ '& .MuiChartsGrid-line': { stroke: theme.palette.divider, strokeDasharray: '2 4' } }} />
  );
}

/** 近五年：柱顶标份数，当年颜色更深；点有文档的一年跳到那一年。 */
export function YearsChart({ years, current, onPick }: { years: { year: number; documents: number }[]; current: number; onPick: (year: number) => void }) {
  const theme = useTheme();
  const main = theme.palette.primary.main;
  return (
    <BarChart height={110} hideLegend borderRadius={4} margin={{ top: 4, right: 0, bottom: 0, left: 0 }}
      xAxis={[{ scaleType: 'band', data: years.map((y) => y.year), valueFormatter: (y: number) => String(y), disableLine: true, disableTicks: true, categoryGapRatio: 0.6,
        colorMap: { type: 'ordinal', values: years.map((y) => y.year), colors: years.map((y) => (y.year === current ? alpha(main, 0.9) : alpha(main, 0.35))) } }]}
      yAxis={[{ position: 'none', min: 0, max: Math.max(1, ...years.map((y) => y.documents)) * 1.3 }]}
      series={[{ data: years.map((y) => y.documents), valueFormatter: fen, label: t('文档'), barLabel: 'value', barLabelPlacement: 'outside' }]}
      onItemClick={(_, item) => { const y = years[item.dataIndex ?? -1]; if (y?.documents) onPick(y.year); }}
      sx={{
        '& .MuiBarElement-root': { cursor: 'pointer' },
        '& .MuiBarLabel-root': { fill: theme.palette.text.secondary, fontSize: 12 },
        [`& .MuiChartsAxis-tickLabel tspan`]: { fill: theme.palette.text.secondary },
      }} />
  );
}

/** 每年文档数的小柱图（目录条目的标题旁）。 */
export function YearSpark({ span }: { span: { year: number | string; count: number }[] }) {
  const theme = useTheme();
  return (
    <SparkLineChart plotType="bar" width={Math.max(40, span.length * 13)} height={32} color={alpha(theme.palette.primary.main, 0.6)}
      data={span.map((s) => s.count)} showTooltip showHighlight valueFormatter={fen}
      xAxis={{ scaleType: 'band', data: span.map((s) => s.year), valueFormatter: (y: number | string) => t('{year} 年', { year: y }) }} />
  );
}

/** 一个指标的折线（v1.8.2）：每次的值连成线，每次单子自己的参考范围画成浅色带；点一个点打开那张单子。 */
export function MetricChart({ points, unit, height, onPoint }: {
  points: { date: string; value?: number; refLow?: number; refHigh?: number; documentId: number }[];
  unit: string; height: number; onPoint?: (documentId: number) => void;
}) {
  const theme = useTheme();
  const dates = points.map((p) => p.date);
  const low = points.map((p) => (p.refLow !== undefined && p.refHigh !== undefined ? p.refLow : null));
  const band = points.map((p) => (p.refLow !== undefined && p.refHigh !== undefined ? p.refHigh - p.refLow : null));
  const fmt = (v: number | null) => (v === null ? '' : `${Math.round(v * 100) / 100}${unit ? ` ${unit}` : ''}`);
  return (
    <LineChart height={height} hideLegend grid={{ horizontal: true }}
      margin={{ top: 12, right: 36, bottom: 4, left: 4 }}
      xAxis={[{ scaleType: 'point', data: dates, valueFormatter: (d: string) => d }]}
      yAxis={[{ width: 48 }]}
      onMarkClick={(_, item) => { const p = points[item.dataIndex ?? -1]; if (p && onPoint) onPoint(p.documentId); }}
      series={[
        { id: 'low', data: low, stack: 'ref', area: true, showMark: false, color: 'transparent', valueFormatter: () => null, connectNulls: true },
        { id: 'band', data: band, stack: 'ref', area: true, showMark: false, color: alpha(theme.palette.success.main, 0.14), label: t('参考范围'), valueFormatter: () => null, connectNulls: true },
        { id: 'value', data: points.map((p) => p.value ?? null), label: t('结果'), color: theme.palette.primary.main, valueFormatter: fmt, showMark: true },
      ]}
      sx={{ '& .MuiChartsGrid-line': { stroke: theme.palette.divider, strokeDasharray: '2 4' }, '& .MuiLineElement-series-low, & .MuiLineElement-series-band': { display: 'none' } }} />
  );
}

/** 小折线：健康页每个指标的最近几次。 */
export function MetricSpark({ values, width, height }: { values: number[]; width: number; height: number }) {
  const theme = useTheme();
  return <SparkLineChart data={values} width={width} height={height} color={theme.palette.primary.main} curve="linear" />;
}

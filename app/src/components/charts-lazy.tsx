// 图表按需加载（v1.3.6）：MUI X Charts 只在用到图表的页面才下载。
// 外层的 role="img" 与 aria-label 不等图表加载，屏幕阅读器照样读出每一项；加载时先占好高度，页面不跳动。
import { Component, lazy, Suspense, type ComponentProps, type ReactNode } from 'react';
import { Box } from '@mui/material';
import { t } from '../i18n';

const load = () => import('./charts');
const MonthsImpl = lazy(() => load().then((m) => ({ default: m.MonthsChart })));
const YearsImpl = lazy(() => load().then((m) => ({ default: m.YearsChart })));
const SparkImpl = lazy(() => load().then((m) => ({ default: m.YearSpark })));
const MetricImpl = lazy(() => load().then((m) => ({ default: m.MetricChart })));
const MetricSparkImpl = lazy(() => load().then((m) => ({ default: m.MetricSpark })));

// 图表脚本没取到（断网、发布后旧的分包已不在）时只空出图表的位置，页面其余部分照常显示
class Quiet extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? null : this.props.children; }
}

function Frame({ label, height, width, children }: { label: string; height: number; width?: number; children: ReactNode }) {
  return (
    <Box role="img" aria-label={label} sx={{ height, width, minWidth: 0, printColorAdjust: 'exact' }}>
      <Quiet><Suspense fallback={null}>{children}</Suspense></Quiet>
    </Box>
  );
}

export function MonthsChart(props: ComponentProps<typeof MonthsImpl>) {
  return <Frame label={t('每月文档：{list}', { list: props.months.map((c, i) => t('{m} 月 {n} 份', { m: i + 1, n: c })).join(t('，')) })} height={props.height}><MonthsImpl {...props} /></Frame>;
}

export function YearsChart(props: ComponentProps<typeof YearsImpl>) {
  return <Frame label={t('近五年：{list}', { list: props.years.map((c) => t('{year} 年 {n} 份', { year: c.year, n: c.documents })).join(t('，')) })} height={110}><YearsImpl {...props} /></Frame>;
}

export function YearSpark(props: ComponentProps<typeof SparkImpl>) {
  return <Frame label={t('每年文档数：{list}', { list: props.span.map((s) => t('{year} 年 {n} 份', { year: s.year, n: s.count })).join(t('，')) })} height={32} width={Math.max(40, props.span.length * 13)}><SparkImpl {...props} /></Frame>;
}

export function MetricChart(props: ComponentProps<typeof MetricImpl> & { label: string }) {
  const { label, ...rest } = props;
  return <Frame label={label} height={props.height}><MetricImpl {...rest} /></Frame>;
}

export function MetricSpark(props: ComponentProps<typeof MetricSparkImpl> & { label: string }) {
  const { label, ...rest } = props;
  return <Frame label={label} height={props.height} width={props.width}><MetricSparkImpl {...rest} /></Frame>;
}

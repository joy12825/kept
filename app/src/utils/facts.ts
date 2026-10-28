// 要点的显示（UI 设计规范 5.3）：把接口算好的值写成卡片上的名称、数值、说明，以及汇总的月度小柱。
import { formatAmount } from '../api/documents';
import type { FactKind, FactValue } from '../api/drawers';
import { FLAG_LABEL, showValue } from '../api/health';
import { currentLang, t } from '../i18n';

export interface FactView {
  label: string;
  value?: string;
  note?: string;
  warn: boolean;
  bars?: { h: number; title: string }[];
  links?: { id: number; title: string }[];
  /** 指标：最近几次的值，画成小折线 */
  spark?: number[];
  /** 点卡片去的页面 */
  href?: string;
}

/** 金额取整显示（卡片上的大数字）：¥3,812。 */
export const moneyInt = (s: string) => `¥${Math.round(Number(s)).toLocaleString('en-US')}`;

export const FACT_KINDS: { kind: FactKind; label: string; help: string }[] = [
  { kind: 'latest', label: t('最新值'), help: t('最近一份符合条件的文档，可以显示它的某个字段，例如“最近一份电费 ¥326.40”') },
  { kind: 'due', label: t('到期'), help: t('日期字段最近的一个到期日，例如“租约 158 天后到期”；在提前天数内会提醒') },
  { kind: 'sum', label: t('汇总'), help: t('把金额或数字字段加起来，例如“今年水电费合计 ¥3,812”，附带每月的小柱图') },
  { kind: 'expect', label: t('周期检查'), help: t('每个周期应该有一份，例如“每月一份电费账单”；缺了会提醒') },
  { kind: 'pinned', label: t('固定文档'), help: t('常用的几份文档，例如房产证、购房合同') },
  { kind: 'metric', label: t('指标'), help: t('体检与化验里一个指标最近的值和变化，例如“谷丙转氨酶 45 U/L，上次 32”') },
];

/** 月份：中文写数字（“3 月”），英文写月份缩写。 */
const monthLabel = (m: number) => (currentLang() === 'zh' ? String(m) : new Intl.DateTimeFormat('en', { month: 'short', timeZone: 'UTC' }).format(new Date(Date.UTC(2000, m - 1, 1))));

const PERIOD: Record<string, string> = { monthly: t('每月一份'), quarterly: t('每季度一份'), yearly: t('每年一份'), this_year: t('今年'), last_12_months: t('近 12 个月'), all: t('全部') };

export function factView(v: FactValue): FactView {
  const f = v.fact;
  const out: FactView = { label: f.label, warn: v.warn };
  switch (f.kind) {
    case 'latest':
      if (!v.document) return { ...out, value: '—', note: t('还没有文档') };
      out.value = v.fieldValue ? (v.fieldType === 'monetary' ? formatAmount(v.fieldValue) : v.fieldValue) : v.document.date;
      out.note = v.fieldValue ? `${v.document.title} · ${v.document.date}` : v.document.title;
      return out;
    case 'due': {
      if (!v.dueDate) return { ...out, value: '—', note: t('没有到期日') };
      const d = v.daysLeft ?? 0;
      out.value = v.dueDate;
      const left = d > 0 ? t('还剩 {n} 天', { n: d }) : d === 0 ? t('就是今天') : t('已过期 {n} 天', { n: -d });
      out.note = v.document ? `${left} · ${v.document.title}` : left;
      return out;
    }
    case 'sum': {
      const total = v.total ?? '0';
      out.value = v.money ? moneyInt(total) : total;
      out.note = t('{period} · {n} 份', { period: PERIOD[f.config.period ?? 'this_year'] ?? '', n: v.count ?? 0 });
      const months = v.months ?? [];
      const max = Math.max(1, ...months.map((m) => Number(m.total)));
      if (months.some((m) => Number(m.total) > 0)) {
        out.bars = months.map((m) => ({ h: Math.max(2, (Number(m.total) / max) * 24), title: t('{month} 月 · {amount}', { month: monthLabel(Number(m.month.slice(5))), amount: v.money ? formatAmount(m.total) : m.total }) }));
      }
      return out;
    }
    case 'expect':
      out.value = v.present ? t('{period}已存入', { period: v.periodLabel ?? '' }) : t('{period}未存入', { period: v.periodLabel ?? '' });
      out.note = PERIOD[f.config.period ?? 'monthly'];
      return out;
    case 'pinned':
      out.links = (v.documents ?? []).map((d) => ({ id: d.id, title: d.title }));
      if (!out.links.length) out.note = t('还没有固定文档');
      return out;
    case 'metric': {
      const m = v.metric;
      if (!m) return { ...out, value: '—', note: t('还没有核对过的结果') };
      out.value = showValue(m, m.unit);
      const prev = m.previous !== undefined ? t('上次 {value}', { value: showValue({ value: m.previous, text: m.previousText ?? '' }) }) : '';
      out.note = [m.flag ? FLAG_LABEL[m.flag] : '', m.date, prev].filter(Boolean).join(' · ');
      if (m.values.length > 1) out.spark = m.values;
      out.href = `/health?${new URLSearchParams({ person: String(m.personId ?? 0), metric: String(m.metricId) })}`;
      return out;
    }
  }
  return out;
}

import { t } from '../../i18n';

const pad = (n: number) => String(n).padStart(2, '0');

/** “2024-10-08 20:41” */
export function dateTime(iso: string) {
  const d = new Date(iso);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** “刚刚”“3 分钟前”“2 小时前”“昨天 20:41”，更早的写日期（UI 设计规范：添加时间用相对时间）。 */
export function relativeTime(iso: string, now = new Date()) {
  const d = new Date(iso);
  const s = (now.getTime() - d.getTime()) / 1000;
  if (s < 60) return t('刚刚');
  if (s < 3600) return t('{n} 分钟前', { n: Math.floor(s / 60) });
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) return t('{n} 小时前', { n: Math.floor(s / 3600) });
  const y = new Date(now);
  y.setDate(now.getDate() - 1);
  if (d.toDateString() === y.toDateString()) return t('昨天 {time}', { time: `${pad(d.getHours())}:${pad(d.getMinutes())}` });
  return dateTime(iso);
}

/** 文件大小：“1.2 MB” */
export function fileSize(n?: number) {
  if (n === undefined) return '—';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

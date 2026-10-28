// 自定义字段作为列表的列（v1.5.6）：按字段类型显示值，点表头按这个字段排序。文档链接字段不能作为列。
import { Typography } from '@mui/material';
import type { DocumentSummary } from '../api/documents';
import type { ColumnKey } from '../api/filters';
import type { CustomField } from '../api/taxonomy';
import { t } from '../i18n';

/** 能作为列的自定义字段：内置的金额、到期日已有自己的列，文档链接字段没有可显示的值。 */
export const listableField = (f: CustomField) => !f.builtIn && f.dataType !== 'document_link';

export const fieldColumnKey = (f: CustomField): ColumnKey => `field:${f.id}`;

const CURRENCY: Record<string, string> = { CNY: '¥', USD: '$', EUR: '€', GBP: '£', JPY: '¥', HKD: 'HK$' };

/** 一个字段值的文字：单选显示选项名，布尔显示“是 / 否”，金额带币种符号。 */
export function fieldText(f: CustomField, v: unknown): string {
  if (v === null || v === undefined || v === '') return '';
  switch (f.dataType) {
    case 'boolean': return v ? t('是') : t('否');
    case 'select': return f.extra.options?.find((o) => o.id === v)?.label ?? '';
    case 'monetary': {
      const c = f.extra.currency ?? 'CNY';
      return `${CURRENCY[c] ?? `${c} `}${String(v)}`;
    }
    default: {
      // 多行文本在列表里只显示第一行
      const s = String(v), nl = s.indexOf('\n');
      return nl < 0 ? s : `${s.slice(0, nl)} …`;
    }
  }
}

/** 列表里这一列的单元格。 */
export function FieldCell({ f, d }: { f: CustomField; d: DocumentSummary }) {
  const v = d.fieldValues?.find((x) => x.fieldId === f.id)?.value;
  const numeric = ['integer', 'float', 'monetary'].includes(f.dataType);
  return (
    <Typography noWrap title={fieldText(f, v) || undefined}
      sx={{ fontSize: 13, color: f.dataType === 'select' || f.dataType === 'text' ? 'text.primary' : 'text.secondary', fontVariantNumeric: numeric ? 'tabular-nums' : undefined }}>
      {fieldText(f, v)}
    </Typography>
  );
}

/** 按这个字段排序时默认的方向：金额从大到小，其他从小到大。 */
export const fieldSortDir = (f: CustomField | undefined): 'asc' | 'desc' => (f?.dataType === 'monetary' ? 'desc' : 'asc');

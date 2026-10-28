// 平铺条件编辑器（当 / 且 / 或，字段，属于 / 不属于 / 包含 / 不包含，值）：收纳规则与工作流共用。
import { Autocomplete, Box, Button, IconButton, MenuItem, Select, Stack, TextField, Typography } from '@mui/material';
import { Plus, Trash2 } from 'lucide-react';
import { useTaxonomy } from '../api/taxonomy';
import { FIELD_LABEL, splitWords, type RuleField, type RuleRow } from '../utils/drawer-rules';
import { SW } from './icons';
import { correspondentOptions, personOptions, tagOptions, typeOptions, useTaxonomyIndex, type PickerOption } from './pickers';
import { FieldCondEditor, startCond } from './field-cond-editor';
import { fieldCondToCond, fixOps } from '../utils/field-conds';
import { t } from '../i18n';

export const blankRow = (join: RuleRow['join']): RuleRow => ({ join, field: 'type', op: 'in', values: [] });

export function RuleRowsEditor({ rows, onChange, empty, hint }: { rows: RuleRow[]; onChange: (rows: RuleRow[]) => void; empty: string; hint: string }) {
  const ix = useTaxonomyIndex();
  const fields = useTaxonomy('custom-fields');
  // v1.6.1：所有类型的自定义字段都能作条件
  const allFields = fields.data ?? [];
  const selectFields = allFields.filter((f) => f.dataType === 'select');
  const options = (f: RuleField): PickerOption[] => {
    if (f === 'type') return typeOptions(ix);
    if (f === 'correspondent') return correspondentOptions(ix);
    if (f === 'person') return personOptions(ix);
    if (f === 'tag') return tagOptions(ix).map((o) => ({ ...o, label: o.title ?? o.label }));
    const fd = selectFields.find((x) => `cf:${x.id}` === f);
    return (fd?.extra.options ?? []).map((o, i) => ({ id: i, label: o.label, title: o.id }));
  };
  const setRows = (fn: (rs: RuleRow[]) => RuleRow[]) => onChange(fn(rows));
  const setRow = (i: number, p: Partial<RuleRow>) => setRows((rs) => rs.map((r, j) => (j === i ? { ...r, ...p } : r)));
  return (
    <>
              {rows.length === 0 && <Typography sx={{ color: 'text.secondary' }}>{empty}</Typography>}
              {rows.map((r, i) => (
                <Box key={i} sx={{ display: 'grid', gridTemplateColumns: '68px minmax(0,1fr) 104px 32px', gap: 1, alignItems: 'center', pb: 1, borderBottom: 1, borderColor: 'divider' }}>
                  {i === 0 ? <Typography variant="body2" sx={{ color: 'text.secondary', pl: 1.5 }}>{t('当')}</Typography> : (
                    <Select size="small" value={r.join} onChange={(e) => setRow(i, { join: e.target.value as RuleRow['join'] })} inputProps={{ 'aria-label': t('第 {n} 条的连接', { n: i + 1 }) }}>
                      <MenuItem value="and">{t('且')}</MenuItem><MenuItem value="or">{t('或')}</MenuItem>
                    </Select>
                  )}
                  <Select size="small" value={r.field} inputProps={{ 'aria-label': t('第 {n} 条的字段', { n: i + 1 }) }}
                    onChange={(e) => {
                      const field = e.target.value as RuleField;
                      const cf = allFields.find((f) => `cf:${f.id}` === field);
                      setRow(i, { field, op: 'in', values: [], fc: cf ? startCond(cf) : undefined, cond: cf ? null : undefined });
                    }}>
                    {(['type', 'correspondent', ...(ix.personList.length || r.field === 'person' ? ['person' as const] : []), 'tag', 'text'] as const).map((f) => <MenuItem key={f} value={f}>{FIELD_LABEL[f]}</MenuItem>)}
                    {allFields.map((f) => <MenuItem key={f.id} value={`cf:${f.id}`}>{f.name}</MenuItem>)}
                  </Select>
                  {r.fc ? <Box /> : (
                    <Select size="small" value={r.op} onChange={(e) => setRow(i, { op: e.target.value as RuleRow['op'] })} inputProps={{ 'aria-label': t('第 {n} 条的运算', { n: i + 1 }) }}>
                      <MenuItem value="in">{r.field === 'text' ? t('包含') : t('属于')}</MenuItem>
                      <MenuItem value="not_in">{r.field === 'text' ? t('不包含') : t('不属于')}</MenuItem>
                    </Select>
                  )}
                  <IconButton size="small" aria-label={t('删除第 {n} 条', { n: i + 1 })} onClick={() => setRows((rs) => rs.filter((_, j) => j !== i))}><Trash2 size={16} strokeWidth={SW} /></IconButton>
                  <Box sx={{ gridColumn: '2 / 4' }}>
                    {r.fc
                      ? <FieldCondEditor compact fields={allFields} value={fixOps(r.fc, allFields.find((f) => f.id === r.fc!.fieldId))} label={t('第 {n} 条的', { n: i + 1 })}
                          onChange={(fc) => {
                            const f = allFields.find((x) => x.id === fc.fieldId);
                            const list = fc.op === 'in' || fc.op === 'not_in';
                            setRow(i, { fc, cond: fieldCondToCond(fc, f), op: fc.op === 'not_in' ? 'not_in' : 'in', values: list ? ((fc.value as string[]) ?? []) : [] });
                          }} />
                      : r.field === 'text'
                      ? <TextField size="small" fullWidth defaultValue={r.values.join('、')} placeholder={t('用顿号或空格分隔')}
                        slotProps={{ htmlInput: { 'aria-label': t('第 {n} 条的关键词', { n: i + 1 }) } }} onChange={(e) => setRow(i, { values: splitWords(e.target.value) })} />
                      : <ValuesInput row={r} index={i} options={options(r.field)} onChange={(values) => setRow(i, { values })} />}
                  </Box>
                </Box>
              ))}
              <Stack direction="row" spacing={1}>
                <Button size="small" variant="outlined" startIcon={<Plus size={14} strokeWidth={SW} />} onClick={() => setRows((rs) => [...rs, blankRow('and')])}>{rows.length ? t('且') : t('添加条件')}</Button>
                {rows.length > 0 && <Button size="small" variant="outlined" startIcon={<Plus size={14} strokeWidth={SW} />} onClick={() => setRows((rs) => [...rs, blankRow('or')])}>{t('或')}</Button>}
              </Stack>
              <Typography variant="caption" sx={{ color: 'text.secondary' }}>{hint}</Typography>
    </>
  );
}

/** 类型、往来方、标签、单选选项的多选。单选字段的选项用它的 ID（title）作为值。 */
function ValuesInput({ row, index, options, onChange }: { row: RuleRow; index: number; options: PickerOption[]; onChange: (v: (number | string)[]) => void }) {
  const isField = row.field.startsWith('cf:');
  const key = (o: PickerOption) => (isField ? o.title! : o.id);
  const value = options.filter((o) => row.values.includes(key(o)));
  return (
    <Autocomplete multiple size="small" options={options} value={value} disableCloseOnSelect
      getOptionLabel={(o) => o.label} isOptionEqualToValue={(a, b) => key(a) === key(b)}
      onChange={(_, v) => onChange(v.map(key))} noOptionsText={t('没有可选的项')}
      renderInput={(params) => <TextField {...params} placeholder={value.length ? '' : t('选择…')} slotProps={{ ...params.slotProps, htmlInput: { ...params.slotProps.htmlInput, 'aria-label': t('第 {n} 条的值', { n: index + 1 }) } }} />} />
  );
}

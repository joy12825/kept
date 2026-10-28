// 一个自定义字段条件的编辑（v1.6.1）：选字段、选运算、填值。文件列表的“字段”筛选和条件编辑器共用。
import { Autocomplete, Box, InputAdornment, MenuItem, Stack, TextField } from '@mui/material';
import type { CustomField } from '../api/taxonomy';
import { fieldOps, needsValue, type FieldCond, type FieldOp } from '../utils/field-conds';
import { t } from '../i18n';

/** 字段改了：运算回到这个类型的第一个，值清空。 */
export const startCond = (f: CustomField): FieldCond => ({ fieldId: f.id, op: fieldOps(f)[0][0] });

export function FieldCondEditor({ fields, value, onChange, label = '', compact }: {
  fields: CustomField[]; value: FieldCond | null; onChange: (c: FieldCond) => void;
  /** 无障碍名称的前缀，例如“第 2 条的” */
  label?: string;
  /** 条件编辑器里一行放不下字段选择，隐藏它（字段在外面选） */
  compact?: boolean;
}) {
  const f = value ? fields.find((x) => x.id === value.fieldId) : undefined;
  const set = (p: Partial<FieldCond>) => value && onChange({ ...value, ...p });
  return (
    <Stack spacing={1.5}>
      {!compact && (
        <TextField select size="small" label={t('字段')} value={value?.fieldId ?? ''} slotProps={{ htmlInput: { 'aria-label': (label ? t('{prefix}字段', { prefix: label }) : t('字段')) } }}
          onChange={(e) => { const nf = fields.find((x) => x.id === Number(e.target.value)); if (nf) onChange(startCond(nf)); }}>
          {fields.map((x) => <MenuItem key={x.id} value={x.id}>{x.name}</MenuItem>)}
        </TextField>
      )}
      {f && value && (
        <>
          <TextField select size="small" label={t('条件')} value={value.op} slotProps={{ htmlInput: { 'aria-label': (label ? t('{prefix}条件', { prefix: label }) : t('条件')) } }}
            onChange={(e) => {
              const op = e.target.value as FieldOp;
              // 换运算时，形状不同的值清掉（介于是两个值，单选是数组）
              const shape = (o: FieldOp) => (o === 'between' ? 'pair' : o === 'in' || o === 'not_in' ? 'list' : 'one');
              onChange({ ...value, op, value: shape(op) === shape(value.op) ? value.value : undefined });
            }}>
            {fieldOps(f).map(([k, l]) => <MenuItem key={k} value={k}>{l}</MenuItem>)}
          </TextField>
          {needsValue(value.op) && <ValueInput f={f} c={value} label={label} onChange={(v) => set({ value: v })} />}
        </>
      )}
    </Stack>
  );
}

function ValueInput({ f, c, label, onChange }: { f: CustomField; c: FieldCond; label: string; onChange: (v: unknown) => void }) {
  if (c.op === 'in' || c.op === 'not_in') {
    const opts = f.extra.options ?? [];
    const sel = opts.filter((o) => (c.value as string[] | undefined)?.includes(o.id!));
    return (
      <Autocomplete multiple size="small" options={opts} value={sel} disableCloseOnSelect getOptionLabel={(o) => o.label}
        isOptionEqualToValue={(a, b) => a.id === b.id} onChange={(_, v) => onChange(v.map((o) => o.id))} noOptionsText={t('没有选项')}
        renderInput={(p) => <TextField {...p} label={t('选项')} slotProps={{ ...p.slotProps, htmlInput: { ...p.slotProps.htmlInput, 'aria-label': (label ? t('{prefix}选项', { prefix: label }) : t('选项')) } }} />} />
    );
  }
  if (c.op === 'next_days' || c.op === 'last_days') {
    return (
      <TextField size="small" type="number" label={t('天数')} value={c.value ?? ''} onChange={(e) => onChange(e.target.value === '' ? undefined : Number(e.target.value))}
        slotProps={{ htmlInput: { min: 1, max: 36500, 'aria-label': (label ? t('{prefix}天数', { prefix: label }) : t('天数')) }, input: { endAdornment: <InputAdornment position="end">{t('天')}</InputAdornment> } }}
        helperText={c.op === 'next_days' ? t('例如 90：今天到 90 天后（三个月内到期）') : t('例如 30：30 天前到今天')} />
    );
  }
  const type = f.dataType === 'date' ? 'date' : f.dataType === 'integer' || f.dataType === 'float' ? 'number' : 'text';
  const one = (v: unknown, onV: (s: string) => void, l: string) => (
    <TextField size="small" type={type} label={l} value={v ?? ''} onChange={(e) => onV(e.target.value)} fullWidth
      slotProps={{ inputLabel: type === 'date' ? { shrink: true } : undefined, htmlInput: { 'aria-label': label ? t('{prefix}{name}', { prefix: label, name: l }) : l, inputMode: f.dataType === 'monetary' ? 'decimal' : undefined },
        input: f.dataType === 'monetary' ? { startAdornment: <InputAdornment position="start">{(f.extra.currency ?? 'CNY') === 'CNY' ? '¥' : f.extra.currency}</InputAdornment> } : undefined }} />
  );
  if (c.op === 'between') {
    const [a, b] = Array.isArray(c.value) ? c.value : ['', ''];
    return <Box sx={{ display: 'flex', gap: 1 }}>{one(a, (v) => onChange([v, b]), t('从'))}{one(b, (v) => onChange([a, v]), t('到'))}</Box>;
  }
  return one(c.value, (v) => onChange(v), f.dataType === 'date' ? t('日期') : t('值'));
}

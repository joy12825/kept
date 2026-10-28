// 上传框里的更多预设（v1.9.12）：家人、标签、日期、原件编号、自定义字段。接口早就支持（v1.5.8、v1.6.3），这里补上网页的入口。
// 按 MUI 的 Text Field 标准：上浮标签、整行的输入框；值的格式同修改文档（金额是两位小数的字符串，数字是数，选项是选项 ID）。
import { useState } from 'react';
import { Autocomplete, Button, Checkbox, FormControlLabel, IconButton, InputAdornment, Menu, MenuItem, Stack, TextField } from '@mui/material';
import { Plus, X } from 'lucide-react';
import { useTaxonomy } from '../api/taxonomy';
import type { CustomField } from '../api/taxonomy';
import { MONEY_RE } from './doc-fields';
import { SW } from './icons';
import { t, tc } from '../i18n';

export interface MorePresets {
  personId: number;
  tagIds: number[];
  createdDate: string;
  asn: string;
  fields: { field: CustomField; text: string; value: unknown }[];
}

export const EMPTY_PRESETS: MorePresets = { personId: 0, tagIds: [], createdDate: '', asn: '', fields: [] };

/** 还没填好的值：原件编号不是 1–999999 的整数，或自定义字段的格式不对。 */
export function presetsInvalid(p: MorePresets, single: boolean) {
  if (single && p.asn && !asnValid(p.asn)) return true;
  return p.fields.some((f) => f.text !== '' && f.value === undefined);
}

const asnValid = (s: string) => /^\d+$/.test(s) && Number(s) >= 1 && Number(s) <= 999999;

/** 转成上传表单里的预设；原件编号只在上传一个文件时带上。 */
export function presetsForUpload(p: MorePresets, single: boolean) {
  const fields = p.fields.filter((f) => f.value !== undefined && f.value !== null && f.text !== '').map((f) => ({ fieldId: f.field.id, value: f.value }));
  return {
    personId: p.personId || undefined,
    tagIds: p.tagIds.length ? p.tagIds : undefined,
    createdDate: p.createdDate || undefined,
    asn: single && asnValid(p.asn) ? Number(p.asn) : undefined,
    customFields: fields.length ? fields : undefined,
  };
}

/** 按字段类型把输入的文字转成值；格式不对时为 undefined。 */
function parse(field: CustomField, text: string): unknown {
  if (text === '') return null;
  switch (field.dataType) {
    case 'integer': return /^-?\d+$/.test(text) ? Number(text) : undefined;
    case 'float': return /^-?\d+(\.\d+)?$/.test(text) ? Number(text) : undefined;
    case 'monetary': {
      const v = text.replace(/[¥,\s]/g, '');
      return MONEY_RE.test(v) ? Number(v).toFixed(2) : undefined;
    }
    case 'boolean': return text === 'true';
    default: return text; // text, url, date, select（选项 ID）
  }
}

export function UploadPresets({ value, onChange, single }: { value: MorePresets; onChange: (p: MorePresets) => void; single: boolean }) {
  const persons = useTaxonomy('persons');
  const tags = useTaxonomy('tags');
  const fields = useTaxonomy('custom-fields');
  const [addEl, setAddEl] = useState<HTMLElement | null>(null);
  const set = (patch: Partial<MorePresets>) => onChange({ ...value, ...patch });
  // 文档链接要先有文档，上传时预设不了
  const addable = (fields.data ?? []).filter((f) => f.dataType !== 'document_link' && !value.fields.some((x) => x.field.id === f.id));
  const setField = (id: number, text: string) =>
    set({ fields: value.fields.map((f) => (f.field.id === id ? { ...f, text, value: parse(f.field, text) } : f)) });
  const tagOptions = tags.data ?? [];

  return (
    <Stack spacing={1.5}>
      <TextField select label={t('家人')} value={persons.data?.some((p) => p.id === value.personId) ? value.personId : 0} onChange={(e) => set({ personId: Number(e.target.value) })}>
        <MenuItem value={0}>{t('不预设')}</MenuItem>
        {persons.data?.map((p) => <MenuItem key={p.id} value={p.id}>{p.name}</MenuItem>)}
      </TextField>
      <Autocomplete multiple options={tagOptions} getOptionLabel={(tag) => tag.name} value={tagOptions.filter((tag) => value.tagIds.includes(tag.id))}
        onChange={(_, picked) => set({ tagIds: picked.map((tag) => tag.id) })} isOptionEqualToValue={(a, b) => a.id === b.id}
        renderInput={(params) => <TextField {...params} label={tc('复数', '标签')} />} />
      <TextField type="date" label={t('日期')} value={value.createdDate} onChange={(e) => set({ createdDate: e.target.value })}
        slotProps={{ inputLabel: { shrink: true } }} helperText={t('填了就用这个日期，不再从正文识别')} />
      <TextField label={t('原件编号')} value={single ? value.asn : ''} disabled={!single} onChange={(e) => set({ asn: e.target.value.trim() })}
        error={single && !!value.asn && !asnValid(value.asn)} slotProps={{ htmlInput: { inputMode: 'numeric' } }}
        helperText={!single ? t('上传一个文件时才能填') : value.asn && !asnValid(value.asn) ? t('1 到 999999 的整数') : t('已被别的文档用了时不填，并在任务里提示')} />
      {value.fields.map(({ field, text, value: v }) => (
        <Stack key={field.id} direction="row" spacing={1} sx={{ alignItems: 'flex-start' }}>
          <FieldInput field={field} text={text} invalid={text !== '' && v === undefined} onChange={(txt) => setField(field.id, txt)} />
          <IconButton aria-label={t('不预设{name}', { name: field.name })} onClick={() => set({ fields: value.fields.filter((f) => f.field.id !== field.id) })} sx={{ mt: 1 }}>
            <X size={18} strokeWidth={SW} />
          </IconButton>
        </Stack>
      ))}
      {addable.length > 0 && (
        <>
          <Button size="small" startIcon={<Plus size={16} strokeWidth={SW} />}
            onClick={(e) => setAddEl(e.currentTarget)} sx={{ alignSelf: 'flex-start' }}>{t('添加字段')}</Button>
          <Menu anchorEl={addEl} open={!!addEl} onClose={() => setAddEl(null)}>
            {addable.map((f) => (
              <MenuItem key={f.id} sx={{ minWidth: 160 }} onClick={() => { setAddEl(null); set({ fields: [...value.fields, { field: f, text: '', value: null }] }); }}>{f.name}</MenuItem>
            ))}
          </Menu>
        </>
      )}
    </Stack>
  );
}

function FieldInput({ field, text, invalid, onChange }: { field: CustomField; text: string; invalid: boolean; onChange: (text: string) => void }) {
  switch (field.dataType) {
    case 'boolean':
      return <FormControlLabel sx={{ flex: 1, ml: 0 }} control={<Checkbox checked={text === 'true'} onChange={(e) => onChange(e.target.checked ? 'true' : '')} />} label={field.name} />;
    case 'select':
      return (
        <TextField select fullWidth label={field.name} value={text} onChange={(e) => onChange(e.target.value)}>
          <MenuItem value="">{t('未选择')}</MenuItem>
          {(field.extra.options ?? []).map((o) => <MenuItem key={o.id} value={o.id}>{o.label}</MenuItem>)}
        </TextField>
      );
    case 'date':
      return <TextField fullWidth type="date" label={field.name} value={text} onChange={(e) => onChange(e.target.value)} slotProps={{ inputLabel: { shrink: true } }} />;
    case 'monetary': {
      const prefix = field.extra.currency === 'CNY' || !field.extra.currency ? '¥' : field.extra.currency;
      return (
        <TextField fullWidth label={field.name} value={text} onChange={(e) => onChange(e.target.value)} error={invalid} helperText={invalid ? t('金额，最多两位小数') : undefined}
          slotProps={{ htmlInput: { inputMode: 'decimal' }, input: { startAdornment: <InputAdornment position="start">{prefix}</InputAdornment> } }} />
      );
    }
    case 'integer':
    case 'float':
      return (
        <TextField fullWidth label={field.name} value={text} onChange={(e) => onChange(e.target.value)} error={invalid}
          helperText={invalid ? (field.dataType === 'integer' ? t('整数') : t('数字')) : undefined} slotProps={{ htmlInput: { inputMode: field.dataType === 'integer' ? 'numeric' : 'decimal' } }} />
      );
    default: // text, url
      return <TextField fullWidth label={field.name} value={text} onChange={(e) => onChange(e.target.value)} placeholder={field.dataType === 'url' ? 'https://' : undefined} />;
  }
}

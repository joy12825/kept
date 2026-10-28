// 文档字段的就地编辑控件：类型、往来方、标签、日期、原件编号、自定义字段（按类型）。
// 详情页先攒着修改再保存；待整理的确认面板改一项存一项。只看的成员只显示值。
import React, { forwardRef, useImperativeHandle, useMemo, useRef, useState } from 'react';
import { Autocomplete, Box, Button, Checkbox, IconButton, InputBase, MenuItem, Select, TextField, Tooltip, Typography } from '@mui/material';
import { ArrowUpRight, Check, ChevronDown, Plus, X } from 'lucide-react';
import { describeSource, isAuto, type FieldSource } from '../api/classifier';
import { api, unwrap } from '../api/client';
import { formatAsn, useDocument, type DocumentSummary } from '../api/documents';
import type { CustomField } from '../api/taxonomy';
import { DrawerIcon, SW, TagDot } from './icons';
import { t, currentLang } from '../i18n';
import { correspondentOptions, drawerOptions, locationOptions, personOptions, Picker, tagOptions, typeOptions, usePickerCreate, usePickerCreateLocation, type TaxonomyIndex } from './pickers';

/** 自动填入的对勾，悬停显示依据。 */
function AutoCheck({ source, top }: { source?: FieldSource; top?: boolean }) {
  if (!source || !isAuto(source)) return <Box />;
  const text = describeSource(source);
  return (
    <Tooltip title={text}>
      <Box role="img" aria-label={t('自动填入：{text}', { text })} sx={{ color: 'success.main', display: 'flex', pt: top ? 1.25 : 0 }}><Check size={16} strokeWidth={2} /></Box>
    </Tooltip>
  );
}

/** 左边标签的宽度：英文的字段名长（Correspondent），要宽一些（v1.10.0）。 */
export const LABEL_W = currentLang() === 'en' ? 108 : 64;

/** 一行：左边标签，右边控件。 */
export function FieldRow({ label, children, top, hint, checks, source }: {
  label: React.ReactNode; children: React.ReactNode; top?: boolean; hint?: React.ReactNode;
  /** 右边留一列给“自动填入”的对勾（待整理的确认面板）；source 是自动填入时的来源 */
  checks?: boolean; source?: FieldSource;
}) {
  return (
    <>
      <Box sx={{ display: 'grid', gridTemplateColumns: checks ? `${LABEL_W}px minmax(0,1fr) 18px` : `${LABEL_W}px minmax(0,1fr)`, alignItems: top ? 'start' : 'center', columnGap: 1, minHeight: 40 }}>
        <Typography variant="body2" sx={{ color: 'text.secondary', pt: top ? 1.25 : 0, display: 'flex', alignItems: 'center', gap: 0.5 }}>{label}</Typography>
        <Box sx={{ minWidth: 0 }}>{children}</Box>
        {checks && <AutoCheck source={source} top={top} />}
      </Box>
      {hint && <Typography variant="caption" sx={{ display: 'block', pl: `${LABEL_W + 8}px`, mt: -0.25, mb: 0.5 }}>{hint}</Typography>}
    </>
  );
}

const bare = {
  width: '100%', px: 1.25, height: 40, borderRadius: 1, border: '1px solid transparent', fontSize: 14,
  '&:hover': { borderColor: 'divider' }, '&.Mui-focused': { borderColor: 'primary.main' },
};
const valueButton = {
  ...bare, display: 'flex', alignItems: 'center', gap: 0.5, bgcolor: 'transparent', color: 'text.primary', font: 'inherit', fontSize: 14,
  textAlign: 'left', cursor: 'pointer',
};

export interface PickerHandle { open: () => void }

/** 类型、往来方、家人或存放位置：点一下弹出带搜索的选择面板，可以清空。 */
/** onBrowse（v1.6.0）：看这个往来方、类型、标签的全部文档；有值时旁边出现入口。 */
export type ClassificationKind = 'type' | 'correspondent' | 'person' | 'location';
const SELECT_LABEL: Record<ClassificationKind, string> = { type: t('选择类型'), correspondent: t('选择往来方'), person: t('选择家人'), location: t('选择存放位置') };
const SEARCH_LABEL: Record<ClassificationKind, string> = { type: t('搜索类型'), correspondent: t('搜索往来方'), person: t('搜索家人'), location: t('搜索存放位置') };
const CLEAR_LABEL: Record<ClassificationKind, string> = { type: t('清空类型'), correspondent: t('清空往来方'), person: t('清空家人'), location: t('清空存放位置') };
const CLASS_LABEL: Record<ClassificationKind, string> = { type: t('类型'), correspondent: t('往来方'), person: t('家人'), location: t('存放位置') };

export const ClassificationField = forwardRef<PickerHandle, { kind: ClassificationKind; value?: number; onChange: (id: number) => void; ix: TaxonomyIndex; readOnly?: boolean; warn?: boolean; onBrowse?: (id: number) => void }>(
  function ClassificationField({ kind, value, onChange, ix, readOnly, warn, onBrowse }, ref) {
    const btn = useRef<HTMLButtonElement>(null);
    const [anchor, setAnchor] = useState<HTMLElement | null>(null);
    useImperativeHandle(ref, () => ({ open: () => !readOnly && setAnchor(btn.current) }), [readOnly]);
    const label = CLASS_LABEL[kind];
    const createItem = usePickerCreate(kind === 'type' ? 'document-types' : kind === 'person' ? 'persons' : 'correspondents');
    const createLocation = usePickerCreateLocation();
    const create = kind === 'location' ? createLocation : createItem;
    const name = !value ? undefined
      : kind === 'location' ? ix.locations.get(value)?.name
        : (kind === 'type' ? ix.types : kind === 'person' ? ix.persons : ix.correspondents).get(value)?.name;
    const options = kind === 'type' ? typeOptions(ix) : kind === 'person' ? personOptions(ix) : kind === 'location' ? locationOptions(ix) : correspondentOptions(ix);
    const browse = value && name && onBrowse
      ? <Tooltip title={t('查看“{name}”的全部文档', { name })}><IconButton size="small" aria-label={t('查看“{name}”的全部文档', { name })} onClick={() => onBrowse(value)} sx={{ flex: 'none' }}><ArrowUpRight size={16} strokeWidth={SW} /></IconButton></Tooltip>
      : null;
    if (readOnly) return <Box sx={{ display: 'flex', alignItems: 'center' }}><Typography sx={{ px: 1.25, fontSize: 14, flex: 1 }}>{name ?? '—'}</Typography>{browse}</Box>;
    return (
      <Box sx={{ display: 'flex', alignItems: 'center', minWidth: 0 }}>
        <Box component="button" ref={btn} type="button" aria-label={t('{label}：{value}', { label, value: name ?? t('未设置') })} onClick={(e) => setAnchor(e.currentTarget)}
          sx={{ ...valueButton, flex: 1, minWidth: 0, borderColor: warn ? 'warning.main' : 'transparent' }}>
          <Box component="span" sx={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: name ? 'text.primary' : 'text.secondary' }}>{name ?? SELECT_LABEL[kind]}</Box>
          <ChevronDown size={16} strokeWidth={SW} />
        </Box>
        {browse}
        <Picker anchor={anchor} onClose={() => setAnchor(null)} options={options}
          selected={value ? [value] : []} onPick={onChange} placeholder={SEARCH_LABEL[kind]} clearLabel={value ? CLEAR_LABEL[kind] : undefined} onCreate={create} />
      </Box>
    );
  });

/**
 * 所在抽屉：选了就算手动放入（收纳规则不再移动它）；手动放入的可以“交还给规则”，并提示交还后会去哪个抽屉。
 * 改动立即保存（不随详情页的“保存”）。
 */
export function DrawerField({ value, manual, ruleDrawerId, ix, readOnly, onPick, onAuto }: {
  value?: number; manual: boolean; ruleDrawerId?: number; ix: TaxonomyIndex; readOnly?: boolean; onPick: (id: number) => void; onAuto: () => void;
}) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const drawer = value ? ix.drawers.get(value) : undefined;
  const name = drawer?.name ?? t('未入抽屉');
  const back = ruleDrawerId ? ix.drawers.get(ruleDrawerId)?.name : undefined;
  const body = <>{drawer ? <DrawerIcon drawer={drawer} size={20} /> : <DrawerIcon size={20} />}<Box component="span" sx={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: drawer ? 'text.primary' : 'text.secondary' }}>{name}</Box></>;
  return (
    <Box>
      {readOnly ? <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 1.25, fontSize: 14 }}>{body}</Box> : (
        <>
          <Box component="button" type="button" aria-label={t('抽屉：{name}', { name })} onClick={(e) => setAnchor(e.currentTarget)} sx={{ ...valueButton, gap: 1 }}>
            {body}<ChevronDown size={16} strokeWidth={SW} />
          </Box>
          <Picker anchor={anchor} onClose={() => setAnchor(null)} options={drawerOptions(ix)} selected={value ? [value] : []} onPick={onPick}
            placeholder={t('搜索抽屉')} clearLabel={value || !manual ? t('未入抽屉') : undefined} />
        </>
      )}
      {manual && (
        <Typography variant="caption" sx={{ display: 'block', px: 1.25, color: 'text.secondary' }}>
          {t('手动放入，收纳规则不会移动它。')}{!readOnly && <Box component="span" role="button" tabIndex={0} onClick={onAuto} onKeyDown={(e) => e.key === 'Enter' && onAuto()}
            sx={{ color: 'primary.main', cursor: 'pointer' }}>{t('交还给规则')}</Box>}{!readOnly && (back ? t('（将放入“{name}”）', { name: back }) : t('（将放入未入抽屉）'))}
        </Typography>
      )}
    </Box>
  );
}

/** 标签：圆点加名称，悬停显示完整路径；“+”打开多选面板，× 去掉一个。 */
export const TagsField = forwardRef<PickerHandle, { value: number[]; onChange: (ids: number[]) => void; ix: TaxonomyIndex; readOnly?: boolean; onBrowse?: (id: number) => void }>(
  function TagsField({ value, onChange, ix, readOnly, onBrowse }, ref) {
    const btn = useRef<HTMLButtonElement>(null);
    const [anchor, setAnchor] = useState<HTMLElement | null>(null);
    useImperativeHandle(ref, () => ({ open: () => !readOnly && setAnchor(btn.current) }), [readOnly]);
    const path = (id: number) => ix.tagTree.find((node) => node.tag.id === id)?.path;
    const create = usePickerCreate('tags');
    return (
      <Box sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 1.25, minHeight: 40, px: 1.25 }}>
        {value.map((id) => {
          const tag = ix.tags.get(id);
          if (!tag) return null;
          return (
            <Box key={id} title={path(id)} sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.25 }}>
              {onBrowse
                ? <Box component="button" type="button" aria-label={t('查看带“{name}”标签的文档', { name: tag.name })} onClick={() => onBrowse(id)}
                    sx={{ border: 0, p: 0, bgcolor: 'transparent', font: 'inherit', color: 'inherit', cursor: 'pointer', borderRadius: 0.5, '&:hover': { textDecoration: 'underline' } }}>
                    <TagDot name={tag.name} color={tag.color} />
                  </Box>
                : <TagDot name={tag.name} color={tag.color} />}
              {!readOnly && <IconButton size="small" aria-label={t('去掉标签“{name}”', { name: tag.name })} onClick={() => onChange(value.filter((x) => x !== id))} sx={{ p: 0.25 }}><X size={12} strokeWidth={SW} /></IconButton>}
            </Box>
          );
        })}
        {readOnly && !value.length && <Typography sx={{ fontSize: 14 }}>—</Typography>}
        {!readOnly && (
          <IconButton ref={btn} size="small" aria-label={t('添加标签')} onClick={(e) => setAnchor(e.currentTarget)} sx={{ border: '1px dashed', borderColor: 'divider', width: 24, height: 24 }}>
            <Plus size={14} strokeWidth={SW} />
          </IconButton>
        )}
        <Picker anchor={anchor} onClose={() => setAnchor(null)} options={tagOptions(ix)} selected={value} multi placeholder={t('搜索标签')} onCreate={create}
          onPick={(id) => onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id])} />
      </Box>
    );
  });

/** 单行文字（标题以外）、日期、整数等：无边框的输入框，悬停时出现边框。 */
export function BareInput({ value, onChange, readOnly, type = 'text', placeholder, label, invalid, onBlur, multiline }: {
  value: string; onChange: (v: string) => void; readOnly?: boolean; type?: string; placeholder?: string; label: string; invalid?: boolean; onBlur?: () => void;
  /** 多行文本字段（v1.5.8） */
  multiline?: boolean;
}) {
  if (readOnly) return <Typography sx={{ px: 1.25, fontSize: 14, overflowWrap: 'anywhere', whiteSpace: multiline ? 'pre-wrap' : undefined }}>{value || '—'}</Typography>;
  return (
    <InputBase value={value} type={type} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} onBlur={onBlur}
      multiline={multiline} minRows={multiline ? 2 : undefined} maxRows={multiline ? 10 : undefined}
      inputProps={{ 'aria-label': label, 'aria-invalid': invalid || undefined, maxLength: multiline ? 2000 : undefined }}
      sx={{ ...bare, ...(multiline ? { height: 'auto', py: 1, lineHeight: '22px' } : {}), ...(invalid ? { borderColor: 'error.main', '&:hover': { borderColor: 'error.main' } } : {}) }} />
  );
}

/** 原件编号：显示 #000123，输入时只要数字。 */
export function AsnField({ value, onChange, readOnly }: { value?: number; onChange: (v: number) => void; readOnly?: boolean }) {
  const [text, setText] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const shown = text ?? (value ? String(value) : '');
  if (readOnly) return <Typography sx={{ px: 1.25, fontSize: 14, fontFamily: 'monospace' }}>{formatAsn(value) || '—'}</Typography>;
  // 下一个（v1.5.3）：填入空间里最大编号加 1，给新收好的纸质原件编号
  const next = async () => {
    setBusy(true);
    try {
      const { asn } = unwrap(await api.GET('/api/v1/documents/next-asn'));
      setText(null);
      onChange(asn);
    } catch { /* 取不到时不填，用户可以手动输入 */ } finally { setBusy(false); }
  };
  return (
    <InputBase value={shown} placeholder={t('未编号')} onChange={(e) => {
      const v = e.target.value.replace(/\D/g, '').slice(0, 6);
      setText(v);
      onChange(v ? Number(v) : 0);
    }} onBlur={() => setText(null)} inputProps={{ 'aria-label': t('原件编号'), inputMode: 'numeric' }}
      startAdornment={<Box component="span" sx={{ color: 'text.secondary', mr: 0.25 }}>#</Box>}
      endAdornment={!shown ? <Tooltip describeChild title={t('填入这个空间里最大的编号加 1')}><Button size="small" disabled={busy} onClick={() => void next()} sx={{ minWidth: 0, flexShrink: 0 }}>{t('下一个')}</Button></Tooltip> : undefined}
      sx={{ ...bare, fontFamily: 'monospace' }} />
  );
}

export const MONEY_RE = /^-?\d+(\.\d{0,2})?$/;
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;
const validDate = (v: string) => DATE_RE.test(v) && !Number.isNaN(Date.parse(v));

/** 日期：统一写成 2024-03-05（不用浏览器的日期控件，它按系统语言显示成 03/05/2024）。
 *  输入完整且有效时才交出去；clearable 时清空交出 null。 */
export function DateInput({ value, onChange, readOnly, label, clearable }: { value: string; onChange: (v: string | null) => void; readOnly?: boolean; label: string; clearable?: boolean }) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? value;
  const invalid = !!shown && !validDate(shown);
  return (
    <>
      <BareInput value={shown} label={label} readOnly={readOnly} placeholder={clearable ? t('未填写') : '2024-03-05'} invalid={invalid || (!clearable && !shown)} onBlur={() => setText(null)}
        onChange={(v) => {
          setText(v);
          if (!v && clearable) onChange(null);
          else if (validDate(v)) onChange(v);
        }} />
      {invalid && <Typography variant="caption" sx={{ color: 'error.main', px: 1.25 }}>{t('日期的格式应为 2024-03-05')}</Typography>}
    </>
  );
}

/** 一个自定义字段的输入，按类型渲染。value 为接口里的形状（见 FieldValue），null 表示清空。 */
export function CustomFieldInput({ field, value, onChange, readOnly, selfId }: {
  field: CustomField; value: unknown; onChange: (v: unknown) => void; readOnly?: boolean; selfId: number;
}) {
  const label = field.name;
  const str = value === null || value === undefined ? '' : String(value);
  switch (field.dataType) {
    case 'boolean':
      return <Checkbox checked={value === true} disabled={readOnly} onChange={(e) => onChange(e.target.checked)} slotProps={{ input: { 'aria-label': label } }} sx={{ ml: 0.25 }} />;
    case 'select': {
      const options = field.extra.options ?? [];
      if (readOnly) return <Typography sx={{ px: 1.25, fontSize: 14 }}>{options.find((o) => o.id === value)?.label ?? '—'}</Typography>;
      return (
        <Select fullWidth size="small" displayEmpty value={str} onChange={(e) => onChange(e.target.value || null)} inputProps={{ 'aria-label': label }}
          sx={{ fontSize: 14, '& .MuiOutlinedInput-notchedOutline': { borderColor: 'transparent' }, '&:hover .MuiOutlinedInput-notchedOutline': { borderColor: 'divider' } }}>
          <MenuItem value=""><Typography sx={{ color: 'text.secondary', fontSize: 14 }}>{t('未选择')}</Typography></MenuItem>
          {options.map((o) => <MenuItem key={o.id} value={o.id}>{o.label}</MenuItem>)}
        </Select>
      );
    }
    case 'document_link':
      return <DocumentLinkInput value={typeof value === 'number' ? value : undefined} onChange={onChange} readOnly={readOnly} selfId={selfId} label={label} />;
    case 'monetary': {
      const currency = field.extra.currency === 'CNY' || !field.extra.currency ? '¥' : `${field.extra.currency} `;
      return <MoneyInput value={str} onChange={onChange} readOnly={readOnly} label={label} prefix={currency} />;
    }
    case 'integer':
    case 'float':
      return <NumberInput value={str} integer={field.dataType === 'integer'} onChange={onChange} readOnly={readOnly} label={label} />;
    case 'date':
      return <DateInput value={str} label={label} readOnly={readOnly} clearable onChange={onChange} />;
    default: // text, url
      if (readOnly && field.dataType === 'url' && str) {
        return <Typography component="a" href={str} target="_blank" rel="noopener noreferrer" sx={{ px: 1.25, fontSize: 14, color: 'primary.main', overflowWrap: 'anywhere' }}>{str}</Typography>;
      }
      return <BareInput value={str} label={label} readOnly={readOnly} placeholder={field.dataType === 'url' ? 'https://' : undefined} multiline={field.dataType === 'text' && !!field.extra.multiline} onChange={(v) => onChange(v || null)} />;
  }
}

function MoneyInput({ value, onChange, readOnly, label, prefix }: { value: string; onChange: (v: unknown) => void; readOnly?: boolean; label: string; prefix: string }) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? value;
  const invalid = !!shown && !MONEY_RE.test(shown);
  if (readOnly) return <Typography sx={{ px: 1.25, fontSize: 14, fontVariantNumeric: 'tabular-nums' }}>{value ? `${prefix}${value}` : '—'}</Typography>;
  return (
    <InputBase value={shown} placeholder="0.00" inputProps={{ 'aria-label': label, inputMode: 'decimal', 'aria-invalid': invalid || undefined }}
      onChange={(e) => {
        const v = e.target.value.replace(/[¥,\s]/g, '');
        setText(v);
        if (!v) onChange(null);
        else if (MONEY_RE.test(v)) onChange(Number(v).toFixed(2));
      }}
      onBlur={() => setText(null)}
      startAdornment={<Box component="span" sx={{ color: 'text.secondary', mr: 0.5 }}>{prefix}</Box>}
      sx={{ ...bare, fontVariantNumeric: 'tabular-nums', ...(invalid ? { borderColor: 'error.main' } : {}) }} />
  );
}

function NumberInput({ value, integer, onChange, readOnly, label }: { value: string; integer: boolean; onChange: (v: unknown) => void; readOnly?: boolean; label: string }) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? value;
  const re = integer ? /^-?\d+$/ : /^-?\d+(\.\d+)?$/;
  return (
    <BareInput value={shown} label={label} readOnly={readOnly} invalid={!!shown && !re.test(shown)} onBlur={() => setText(null)}
      onChange={(v) => { setText(v); if (!v) onChange(null); else if (re.test(v)) onChange(Number(v)); }} />
  );
}

/** 文档链接：按标题搜索本空间的文档。 */
/** 搜索并选一份文档（文档链接字段；v1.5.8 起也用于“从已有文档添加版本”）。fieldLabel 时显示为带标签的输入框。 */
export function DocumentLinkInput({ value, onChange, readOnly, selfId, label, fieldLabel }: { value?: number; onChange: (v: unknown) => void; readOnly?: boolean; selfId: number; label: string; fieldLabel?: string }) {
  const linked = useDocument(value);
  const [options, setOptions] = useState<Pick<DocumentSummary, 'id' | 'title'>[]>([]);
  const search = useMemo(() => {
    let t = 0;
    return (q: string) => {
      window.clearTimeout(t);
      t = window.setTimeout(async () => {
        const filter = q.trim() ? JSON.stringify({ text: q.trim() }) : undefined;
        const page = unwrap(await api.GET('/api/v1/documents', { params: { query: { filter, sort: 'added', dir: 'desc', limit: 20 } } }));
        setOptions(page.items.filter((d) => d.id !== selfId).map((d) => ({ id: d.id, title: d.title })));
      }, 250);
    };
  }, [selfId]);
  const current = value ? { id: value, title: linked.data?.title ?? t('（已删除的文档）') } : null;
  if (readOnly) return <Typography sx={{ px: 1.25, fontSize: 14 }}>{current ? t('《{title}》', { title: current.title }) : '—'}</Typography>;
  return (
    <Autocomplete size="small" options={options} value={current} getOptionLabel={(o) => o.title} isOptionEqualToValue={(a, b) => a.id === b.id}
      filterOptions={(x) => x} onOpen={() => search('')} onInputChange={(_, q, reason) => reason === 'input' && search(q)}
      onChange={(_, o) => onChange(o ? o.id : null)} noOptionsText={t('没有找到文档')}
      renderInput={(params) => <TextField {...params} label={fieldLabel} placeholder={t('搜索文档标题')} slotProps={{ ...params.slotProps, htmlInput: { ...params.slotProps.htmlInput, 'aria-label': label } }} />} />
  );
}

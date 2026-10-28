// 目录的编辑面板：从右侧滑出（480px），新建和编辑共用。匹配规则可以粘贴一段文字“试一试”。
import { useEffect, useMemo, useState, type FormEvent } from 'react';
import {
  Alert, Box, Button, ButtonBase, Checkbox, Dialog, DialogActions, DialogContent, DialogTitle, Divider, Drawer, FormControlLabel,
  IconButton, MenuItem, Stack, Switch, TextField, Typography,
} from '@mui/material';
import { ArrowDown, ArrowUp, Check, Plus, Trash2, X } from 'lucide-react';
import {
  ALGORITHMS, CURRENCIES, FIELD_TYPES, KIND_LABEL, RETENTIONS, TAG_COLORS, tagPath, tagSubtree,
  useCreateTaxonomy, useDeleteTaxonomy, useUpdateTaxonomy,
  type CustomField, type CustomFieldType, type MatchRule, type SelectOption, type Tag, type TaxonomyItem, type TaxonomyKind,
} from '../../api/taxonomy';
import { ApiError } from '../../api/client';
import { useMatchTest } from '../../api/classifier';
import { SW } from '../../components/icons';
import { useApp } from '../../store/app-store';
import { PersonPrivacySection } from '@ee';
import { t } from '../../i18n';

type AnyItem = TaxonomyItem | Tag | CustomField;

const DEFAULT_RULE: MatchRule = { algorithm: 'auto', match: '', caseInsensitive: true };
const needsKeywords = (r: MatchRule) => r.algorithm !== 'auto' && r.algorithm !== 'none';

function RuleEditor({ rule, onChange, disabled }: { rule: MatchRule; onChange: (r: MatchRule) => void; disabled: boolean }) {
  const help = ALGORITHMS.find((a) => a.value === rule.algorithm)?.help;
  return (
    <Stack spacing={2}>
      <Typography variant="h6">{t('匹配规则')}</Typography>
      <TextField select label={t('匹配方式')} value={rule.algorithm} disabled={disabled} fullWidth
        onChange={(e) => onChange({ ...rule, algorithm: e.target.value as MatchRule['algorithm'] })}
        helperText={t('{help}。规则会在文档入库时生效。', { help: help ?? '' })}>
        {ALGORITHMS.map((a) => <MenuItem key={a.value} value={a.value}>{a.label}</MenuItem>)}
      </TextField>
      {needsKeywords(rule) && (
        <>
          <TextField label={rule.algorithm === 'regex' ? t('正则表达式') : t('关键词')} value={rule.match} disabled={disabled} fullWidth required
            onChange={(e) => onChange({ ...rule, match: e.target.value })} />
          <FormControlLabel disabled={disabled} label={t('区分大小写')}
            control={<Checkbox checked={!rule.caseInsensitive} onChange={(e) => onChange({ ...rule, caseInsensitive: !e.target.checked })} />} />
          <TryRule rule={rule} />
        </>
      )}
      {rule.algorithm === 'auto' && (
        <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('自动学习由模型根据已整理的文档判断，无法单独试；样本数可以在“设置 → 自动分类”里查看。')}</Typography>
      )}
    </Stack>
  );
}

/** 试一试：粘贴一段文字，看当前（还没保存的）规则能不能匹配上。 */
function TryRule({ rule }: { rule: MatchRule }) {
  const [text, setText] = useState('');
  const test = useMatchTest();
  const { mutate, reset } = test;
  const ready = text.trim() !== '' && rule.match.trim() !== '';
  useEffect(() => {
    if (!ready) { reset(); return; }
    const timer = window.setTimeout(() => mutate({ rule, text }), 300);
    return () => window.clearTimeout(timer);
  }, [ready, text, rule.algorithm, rule.match, rule.caseInsensitive]); // eslint-disable-line react-hooks/exhaustive-deps
  const r = test.data;
  return (
    <Box>
      <TextField label={t('试一试')} placeholder={t('粘贴一段文档里的文字')} multiline minRows={2} maxRows={6} fullWidth value={text}
        onChange={(e) => setText(e.target.value)} slotProps={{ htmlInput: { maxLength: 20000 } }} />
      <Typography variant="body2" role="status" sx={{ mt: 0.75, minHeight: 20, display: 'flex', alignItems: 'center', gap: 0.5,
        color: test.error ? 'error.main' : r?.matched ? 'success.main' : 'text.secondary' }}>
        {ready && test.error && test.error.message}
        {ready && !test.error && r && (r.matched ? <><Check size={16} strokeWidth={2} />{t('匹配：{basis}', { basis: r.basis ?? '' })}</> : t('不匹配'))}
      </Typography>
    </Box>
  );
}

function ColorPicker({ value, onChange, disabled }: { value: string; onChange: (c: string) => void; disabled: boolean }) {
  return (
    <Box>
      <Typography id="tag-color-label" variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>{t('颜色')}</Typography>
      <Box role="radiogroup" aria-labelledby="tag-color-label" sx={{ display: 'grid', gridTemplateColumns: 'repeat(6, 32px)', gap: 1 }}>
        {TAG_COLORS.map(([c, label]) => (
          <ButtonBase key={c} role="radio" aria-checked={value === c} aria-label={label} disabled={disabled} onClick={() => onChange(c)}
            sx={{ width: 32, height: 32, borderRadius: '8px', bgcolor: c, color: '#fff', outline: value === c ? `2px solid ${c}` : 'none', outlineOffset: 2 }}>
            {value === c && <Check size={16} strokeWidth={SW + 0.75} />}
          </ButtonBase>
        ))}
      </Box>
    </Box>
  );
}

function OptionsEditor({ options, onChange, disabled }: { options: SelectOption[]; onChange: (o: SelectOption[]) => void; disabled: boolean }) {
  const set = (i: number, o: Partial<SelectOption>) => onChange(options.map((x, j) => (j === i ? { ...x, ...o } : x)));
  const move = (i: number, d: number) => { const next = [...options]; [next[i], next[i + d]] = [next[i + d], next[i]]; onChange(next); };
  return (
    <Stack spacing={1}>
      <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('选项')}</Typography>
      {options.map((o, i) => (
        <Stack key={o.id ?? `new-${i}`} direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
          <TextField size="small" value={o.label} disabled={disabled} fullWidth slotProps={{ htmlInput: { 'aria-label': t('选项 {n}', { n: i + 1 }) } }}
            onChange={(e) => set(i, { label: e.target.value })} />
          <IconButton size="small" aria-label={t('上移')} disabled={disabled || i === 0} onClick={() => move(i, -1)}><ArrowUp size={16} strokeWidth={SW} /></IconButton>
          <IconButton size="small" aria-label={t('下移')} disabled={disabled || i === options.length - 1} onClick={() => move(i, 1)}><ArrowDown size={16} strokeWidth={SW} /></IconButton>
          <IconButton size="small" aria-label={t('删除选项 {n}', { n: i + 1 })} disabled={disabled || options.length === 1} onClick={() => onChange(options.filter((_, j) => j !== i))}>
            <X size={16} strokeWidth={SW} />
          </IconButton>
        </Stack>
      ))}
      {!disabled && <Box><Button size="small" startIcon={<Plus size={14} strokeWidth={SW} />} onClick={() => onChange([...options, { label: '' }])}>{t('添加选项')}</Button></Box>}
    </Stack>
  );
}

export function EditPanel({ kind, item, readOnly, tags, tagsById, onClose, onDeleted }: {
  kind: TaxonomyKind; item: AnyItem | null; readOnly: boolean; tags: Tag[]; tagsById: Map<number, Tag>; onClose: () => void;
  /** 删除成功后调用（条目页据此回到目录）；不传时与关闭相同 */
  onDeleted?: () => void;
}) {
  const app = useApp();
  const create = useCreateTaxonomy(kind);
  const update = useUpdateTaxonomy(kind);
  const remove = useDeleteTaxonomy(kind);
  const label = KIND_LABEL[kind].toLowerCase(); // 放在句中（英文小写）
  const field = kind === 'custom-fields' ? (item as CustomField | null) : null;
  const tag = kind === 'tags' ? (item as Tag | null) : null;

  const [name, setName] = useState(item?.name ?? '');
  const [rule, setRule] = useState<MatchRule>((item as TaxonomyItem | null)?.rule ?? DEFAULT_RULE);
  const [color, setColor] = useState(tag?.color ?? TAG_COLORS[1][0]);
  const [parentId, setParentId] = useState<number>(tag?.parentId ?? 0);
  const [showInYearbook, setShowInYearbook] = useState(tag?.showInYearbook ?? false);
  const [dataType, setDataType] = useState<CustomFieldType>(field?.dataType ?? 'text');
  const [currency, setCurrency] = useState(field?.extra.currency ?? 'CNY');
  const [options, setOptions] = useState<SelectOption[]>(field?.extra.options ?? [{ label: '' }]);
  const [multiline, setMultiline] = useState(field?.extra.multiline ?? false); // 多行文本（v1.5.8）
  // 类型的保留期限（v1.6.5，月；0 为永久）
  const [retention, setRetention] = useState((item as TaxonomyItem | null)?.retentionMonths ?? 0);
  // 这类文档选了家人后自动设为私密（v1.8.0）
  const [privateByDefault, setPrivateByDefault] = useState((item as TaxonomyItem | null)?.privateByDefault ?? false);
  const [confirming, setConfirming] = useState(false);
  // 服务端的出错信息；field 为 name 时显示在名称下（v1.10.1：看 details.field，不看文字）
  const [failure, setFailure] = useState<{ message: string; field?: unknown } | null>(null);
  const error = failure?.message ?? null;

  // 上级标签的候选：不能是自己和自己的后代
  const parents = useMemo(() => {
    const exclude = tag ? tagSubtree(tag.id, tags) : new Set<number>();
    return tags.filter((tg) => !exclude.has(tg.id)).map((tg) => ({ id: tg.id, path: tagPath(tg, tagsById) })).sort((a, b) => a.path.localeCompare(b.path, 'zh'));
  }, [tag, tags, tagsById]);
  const childCount = tag ? tags.filter((tg) => tg.parentId === tag.id).length : 0;

  const builtIn = !!field?.builtIn;
  const busy = create.isPending || update.isPending || remove.isPending;
  const disabled = readOnly || busy;
  const nameError = failure?.field === 'name' ? failure.message : null;

  const body = () => {
    const extra = dataType === 'monetary' ? { currency } : dataType === 'select' ? { options: options.map((o) => ({ ...o, label: o.label.trim() })) }
      : dataType === 'text' ? { multiline } : undefined;
    switch (kind) {
      case 'tags': return { name, color, parentId, rule, showInYearbook };
      case 'custom-fields': return item ? (builtIn ? { name } : { name, ...(extra ? { extra } : {}) }) : { name, dataType, ...(extra ? { extra } : {}) };
      case 'document-types': return item ? { name, rule, retentionMonths: retention, privateByDefault } : { name, rule };
      default: return { name, rule };
    }
  };

  const fail = (e: ApiError) => setFailure({ message: e.message, field: e.details?.field });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (disabled || !name.trim()) return;
    setFailure(null);
    if (item) {
      update.mutate({ id: item.id, patch: body() as never }, { onSuccess: () => { app.toast(t('已保存')); onClose(); }, onError: fail });
    } else {
      create.mutate(body() as never, { onSuccess: () => { app.toast(t('已新建{kind}“{name}”', { kind: label, name: name.trim() })); onClose(); }, onError: fail });
    }
  };
  const doDelete = () => {
    if (!item) return;
    remove.mutate(item.id, {
      onSuccess: () => { setConfirming(false); app.toast(t('已删除{kind}“{name}”', { kind: label, name: item.name })); (onDeleted ?? onClose)(); },
      onError: (e) => { setConfirming(false); fail(e); },
    });
  };

  return (
    <Drawer anchor="right" open onClose={onClose} slotProps={{ paper: { sx: { width: { xs: '100%', sm: 480 } } } }}>
      <Box component="form" onSubmit={submit} sx={{ display: 'flex', flexDirection: 'column', height: '100%' }} aria-label={item ? t('编辑{kind}', { kind: label }) : t('新建{kind}', { kind: label })}>
        <Stack direction="row" sx={{ alignItems: 'center', px: 3, height: 64, flex: 'none' }}>
          <Typography variant="h3" component="h2" sx={{ flex: 1 }}>{item ? (readOnly ? item.name : t('编辑{kind}', { kind: label })) : t('新建{kind}', { kind: label })}</Typography>
          <IconButton aria-label={t('关闭')} onClick={onClose}><X size={20} strokeWidth={SW} /></IconButton>
        </Stack>
        <Divider />
        <Stack spacing={2.5} sx={{ p: 3, flex: 1, overflowY: 'auto' }}>
          {error && !nameError && <Alert severity="error">{error}</Alert>}
          <TextField label={t('名称')} value={name} disabled={disabled} required fullWidth autoFocus={!item}
            error={!!nameError} helperText={nameError ?? (builtIn ? t('内置字段，用于金额汇总和到期提醒；只能改名，不能删除。') : ' ')}
            onChange={(e) => { setName(e.target.value); setFailure(null); }} slotProps={{ htmlInput: { maxLength: 64 } }} />

          {kind === 'tags' && (
            <>
              <TextField select label={t('上级标签')} value={parentId} disabled={disabled} fullWidth onChange={(e) => setParentId(Number(e.target.value))}
                helperText={t('最多 5 层')}>
                <MenuItem value={0}>{t('无（顶层标签）')}</MenuItem>
                {parents.map((p) => <MenuItem key={p.id} value={p.id}>{p.path}</MenuItem>)}
              </TextField>
              <ColorPicker value={color} onChange={setColor} disabled={disabled} />
              <FormControlLabel disabled={disabled} label={t('在年度回顾中显示（用途标签，例如税务、报销）')}
                control={<Checkbox checked={showInYearbook} onChange={(e) => setShowInYearbook(e.target.checked)} />} />
            </>
          )}

          {kind === 'custom-fields' && (
            <>
              <TextField select label={t('类型')} value={dataType} disabled={disabled || !!item} fullWidth onChange={(e) => setDataType(e.target.value as CustomFieldType)}
                helperText={item ? t('字段类型创建后不能修改') : ' '}>
                {FIELD_TYPES.map((ft) => <MenuItem key={ft.value} value={ft.value}>{ft.label}</MenuItem>)}
              </TextField>
              {dataType === 'monetary' && (
                <TextField select label={t('币种')} value={currency} disabled={disabled || builtIn} fullWidth onChange={(e) => setCurrency(e.target.value)}>
                  {CURRENCIES.map(([c, l]) => <MenuItem key={c} value={c}>{t('{label}（{code}）', { label: l, code: c })}</MenuItem>)}
                </TextField>
              )}
              {dataType === 'select' && <OptionsEditor options={options} onChange={setOptions} disabled={disabled} />}
              {dataType === 'text' && (
                <FormControlLabel disabled={disabled} control={<Switch checked={multiline} onChange={(e) => setMultiline(e.target.checked)} />}
                  label={<Box><Typography>{t('多行')}</Typography><Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('适合保修条款、用药说明这类较长的内容，最多 2000 字')}</Typography></Box>} />
              )}
            </>
          )}

          {kind === 'document-types' && item && (
            <TextField select label={t('保留期限')} value={retention} disabled={disabled} fullWidth onChange={(e) => setRetention(Number(e.target.value))}
              helperText={t('超过期限的文档会出现在“可以清理”里，由你确认后再移到回收站，不会自动删除')}>
              {RETENTIONS.map(([m, l]) => <MenuItem key={m} value={m}>{l}</MenuItem>)}
            </TextField>
          )}
          {kind === 'document-types' && item && (
            <FormControlLabel disabled={disabled} control={<Switch checked={privateByDefault} onChange={(e) => setPrivateByDefault(e.target.checked)} />}
              label={<Box><Typography>{t('选了家人后自动设为私密')}</Typography><Typography variant="caption" sx={{ color: 'text.secondary' }}>
                {t('适合体检、化验、病历这类文档：只有这位家人和他允许的人看得到。这位家人要先关联账号或有照看人')}</Typography></Box>} />
          )}
          {kind === 'persons' && item && (item as TaxonomyItem).privacy && (
            <>
              <Divider />
              <PersonPrivacySection personId={item.id} name={item.name} privacy={(item as TaxonomyItem).privacy!} canWrite={!readOnly} />
            </>
          )}
          {kind !== 'custom-fields' && (
            <>
              <Divider />
              <RuleEditor rule={rule} onChange={setRule} disabled={disabled} />
            </>
          )}
        </Stack>
        {!readOnly && (
          <>
            <Divider />
            <Stack direction="row" spacing={1} sx={{ px: 3, py: 2, flex: 'none' }}>
              {item && !builtIn && (
                <Button color="error" startIcon={<Trash2 size={16} strokeWidth={SW} />} disabled={busy} onClick={() => setConfirming(true)}>{t('删除')}</Button>
              )}
              <Box sx={{ flex: 1 }} />
              <Button onClick={onClose}>{t('取消')}</Button>
              <Button type="submit" variant="contained" disabled={busy || !name.trim()}>{item ? t('保存') : t('创建')}</Button>
            </Stack>
          </>
        )}
      </Box>

      <Dialog open={confirming} onClose={() => setConfirming(false)} fullWidth maxWidth="xs">
        <DialogTitle>{t('删除{kind}“{name}”？', { kind: label, name: item?.name ?? '' })}</DialogTitle>
        <DialogContent>
          <Typography sx={{ color: 'text.secondary' }}>
            {childCount > 0 ? t('它的 {n} 个子标签会上移一层。删除后无法恢复。', { n: childCount }) : t('删除后无法恢复。')}
          </Typography>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 2 }}>
          <Button onClick={() => setConfirming(false)}>{t('取消')}</Button>
          <Button color="error" variant="contained" disabled={remove.isPending} onClick={doDelete}>{t('删除')}</Button>
        </DialogActions>
      </Dialog>
    </Drawer>
  );
}

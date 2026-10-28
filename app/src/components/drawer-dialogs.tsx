// 抽屉的新建与修改（名称、图标、颜色、说明），以及添加 / 修改要点的对话框：先选种类，再回答两三个问题。
import { useEffect, useMemo, useState } from 'react';
import { Autocomplete, Box, Button, ButtonBase, Card, CardActionArea, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, MenuItem, Stack, Switch, TextField, Typography } from '@mui/material';
import { Check } from 'lucide-react';
import { api, unwrap } from '../api/client';
import {
  DRAWER_COLORS, DRAWER_ICONS, useAddFact, useCreateDrawer, useDrawers, useUpdateDrawer, useUpdateFact,
  type Drawer, type DrawerColorName, type DrawerIconName, type Fact, type FactConfig, type FactKind,
} from '../api/drawers';
import type { Cond } from '../api/filters';
import { useTaxonomy } from '../api/taxonomy';
import { useMetrics } from '../api/health';
import { useApp } from '../store/app-store';
import { useEnableLibrary } from '../api/books';
import { FACT_KINDS } from '../utils/facts';
import { splitWords } from '../utils/drawer-rules';
import { DrawerIcon, SW } from './icons';
import { correspondentOptions, tagOptions, useTaxonomyIndex, type PickerOption } from './pickers';
import { t } from '../i18n';
import { eeBuild } from '@ee';

const COLOR_LABEL: Record<DrawerColorName, string> = { blue: t('蓝'), cyan: t('青'), green: t('绿'), orange: t('橙'), red: t('红'), purple: t('紫'), pink: t('粉'), gray: t('灰') };

/** 新建（drawer 为空）或修改抽屉的名称、图标、颜色和说明。 */
export function DrawerDialog({ drawer, open, onClose, onCreated }: { drawer?: Drawer; open: boolean; onClose: () => void; onCreated?: (d: Drawer) => void }) {
  const app = useApp();
  const create = useCreateDrawer();
  const update = useUpdateDrawer();
  const [name, setName] = useState('');
  const [icon, setIcon] = useState<DrawerIconName>('file');
  const [color, setColor] = useState<DrawerColorName>('blue');
  const [description, setDescription] = useState('');
  const [sensitive, setSensitive] = useState(false);
  const [sameName, setSameName] = useState(false);
  const [error, setError] = useState('');
  const hasLibrary = useDrawers().data?.items.some((x) => x.kind === 'library') ?? true;
  const enable = useEnableLibrary();
  const openLibrary = () => enable.mutate(undefined, {
    onSuccess: (r) => { app.toast(t('已开启书房，书籍会自动放进“{name}”', { name: r.drawer.name })); onClose(); onCreated?.(r.drawer); },
    onError: (e) => setError(e.message),
  });
  useEffect(() => {
    if (!open) return;
    setName(drawer?.name ?? ''); setIcon(drawer?.icon ?? 'file'); setColor(drawer?.color ?? 'blue'); setDescription(drawer?.description ?? ''); setSensitive(!!drawer?.sensitive); setSameName(!!drawer?.sameNameVersions); setError('');
  }, [open, drawer]);
  const busy = create.isPending || update.isPending;
  const submit = () => {
    const onError = (e: { message: string }) => setError(e.message);
    if (drawer) {
      update.mutate({ id: drawer.id, patch: { name, icon, color, description, ...(sensitive !== !!drawer.sensitive ? { sensitive } : {}),
        ...(sameName !== !!drawer.sameNameVersions ? { sameNameVersions: sameName } : {}) } }, { onSuccess: () => { app.toast(t('已保存')); onClose(); }, onError });
    } else {
      create.mutate({ name, icon, color, description }, { onSuccess: (r) => { onClose(); onCreated?.(r.drawer); }, onError });
    }
  };
  return (
    <Dialog open={open} onClose={onClose} maxWidth="xs" fullWidth component="form" onSubmit={(e: React.FormEvent) => { e.preventDefault(); submit(); }}
      aria-label={drawer ? t('修改抽屉') : t('新建抽屉')}>
      <DialogTitle>{drawer ? t('修改抽屉') : t('新建抽屉')}</DialogTitle>
      <DialogContent>
        <Stack spacing={2.5} sx={{ pt: 1 }}>
          {!drawer && !hasLibrary && (
            <Card variant="outlined" sx={{ display: 'flex', alignItems: 'center', gap: 1.5, p: 1.5 }}>
              <DrawerIcon drawer={{ icon: 'book', color: 'orange' }} size={32} />
              <Box sx={{ flex: 1 }}>
                <Typography variant="body2" sx={{ fontWeight: 500 }}>{t('书房')}</Typography>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('收纳所有书籍，按封面排列')}</Typography>
              </Box>
              <Button size="small" variant="outlined" disabled={enable.isPending} onClick={openLibrary}>{t('开启书房')}</Button>
            </Card>
          )}
          <Stack direction="row" spacing={2} sx={{ alignItems: 'center' }}>
            <DrawerIcon drawer={{ icon, color }} size={48} />
            <TextField autoFocus fullWidth required label={t('名称')} value={name} onChange={(e) => { setName(e.target.value); setError(''); }}
              error={!!error} helperText={error || t('例如房子、车、孩子')} slotProps={{ htmlInput: { maxLength: 20 } }} />
          </Stack>
          <Box>
            <Typography id="drawer-icon-label" variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>{t('图标')}</Typography>
            <Box role="radiogroup" aria-labelledby="drawer-icon-label" sx={{ display: 'grid', gridTemplateColumns: 'repeat(7, 40px)', gap: 1 }}>
              {DRAWER_ICONS.map((i) => (
                <ButtonBase key={i} role="radio" aria-checked={icon === i} aria-label={i} onClick={() => setIcon(i)}
                  sx={{ width: 40, height: 40, borderRadius: 2, outline: icon === i ? '2px solid' : 'none', outlineColor: 'primary.main' }}>
                  <DrawerIcon drawer={{ icon: i, color }} size={32} />
                </ButtonBase>
              ))}
            </Box>
          </Box>
          <Box>
            <Typography id="drawer-color-label" variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>{t('颜色')}</Typography>
            <Box role="radiogroup" aria-labelledby="drawer-color-label" sx={{ display: 'flex', gap: 1 }}>
              {DRAWER_COLORS.map((c) => (
                <ButtonBase key={c} role="radio" aria-checked={color === c} aria-label={COLOR_LABEL[c]} onClick={() => setColor(c)}
                  sx={{ width: 32, height: 32, borderRadius: '50%', bgcolor: `drawer.${c}`, color: '#fff', outline: color === c ? '2px solid' : 'none', outlineColor: `drawer.${c}`, outlineOffset: 2 }}>
                  {color === c && <Check size={16} strokeWidth={SW + 0.75} />}
                </ButtonBase>
              ))}
            </Box>
          </Box>
          <TextField label={t('说明')} value={description} onChange={(e) => setDescription(e.target.value)} multiline minRows={2} helperText={t('可选，写这个抽屉放什么')} />
          {drawer && (
            <FormControlLabel control={<Switch checked={sameName} onChange={(e) => setSameName(e.target.checked)} />}
              label={<Box><Typography variant="body2">{t('同名文件直接作为新版本')}</Typography><Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('往这个抽屉上传和已有文档同名的文件时，不再询问，直接作为那份文档的新版本；旧版本可以在文档的“版本”里查看和恢复。')}</Typography></Box>} />
          )}
          {drawer && drawer.kind !== 'library' && (
            <FormControlLabel control={<Switch checked={sensitive} onChange={(e) => setSensitive(e.target.checked)} />}
              label={<Box><Typography variant="body2">{t('敏感抽屉')}</Typography><Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('里面的文档打开前要确认密码，不能分享，也不交给 AI。适合身份证、银行卡这类证件。')}</Typography></Box>} />
          )}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button type="submit" variant="contained" disabled={busy || !name.trim()}>{drawer ? t('保存') : t('新建')}</Button>
      </DialogActions>
    </Dialog>
  );
}

const DEFAULT_LABEL: Record<FactKind, string> = { latest: t('最近一份'), due: t('下次到期'), sum: t('今年合计'), expect: t('每月一份'), pinned: t('固定文档'), metric: '' };

/** 筛选条件里能在对话框里编辑的部分：往来方和关键词。其余的保留原样。 */
function readFilter(f?: Cond): { corr: number[]; words: string[]; other?: Cond } {
  if (!f) return { corr: [], words: [] };
  const parts = Array.isArray(f.all) ? (f.all as Cond[]) : [f];
  const out: { corr: number[]; words: string[]; other?: Cond } = { corr: [], words: [] };
  const rest: Cond[] = [];
  for (const p of parts) {
    if (p.field === 'correspondent' && p.op === 'in' && Array.isArray(p.value)) out.corr = p.value as number[];
    else if (typeof p.text === 'string') out.words = [p.text];
    else if (Array.isArray(p.any) && (p.any as Cond[]).every((x) => typeof x.text === 'string')) out.words = (p.any as Cond[]).map((x) => x.text as string);
    else rest.push(p);
  }
  if (rest.length) out.other = rest.length === 1 ? rest[0] : { all: rest };
  return out;
}

function writeFilter(corr: number[], words: string[], other?: Cond): Cond | undefined {
  const all: Cond[] = [];
  if (corr.length) all.push({ field: 'correspondent', op: 'in', value: corr });
  if (words.length) all.push(words.length === 1 ? { text: words[0] } : { any: words.map((w) => ({ text: w })) });
  if (other) all.push(other);
  if (!all.length) return undefined;
  return all.length === 1 ? all[0] : { all };
}

/** 添加（fact 为空）或修改一个要点。 */
export function FactDialog({ drawer, fact, open, onClose }: { drawer: Drawer; fact?: Fact; open: boolean; onClose: () => void }) {
  const app = useApp();
  const ix = useTaxonomyIndex();
  const fields = useTaxonomy('custom-fields').data ?? [];
  const metrics = useMetrics(open && eeBuild).data ?? [];
  const persons = useTaxonomy('persons').data ?? [];
  const add = useAddFact(drawer.id);
  const update = useUpdateFact();
  const [kind, setKind] = useState<FactKind | null>(null);
  const [label, setLabel] = useState('');
  const [cfg, setCfg] = useState<FactConfig>({});
  const [corr, setCorr] = useState<number[]>([]);
  const [words, setWords] = useState('');
  const [other, setOther] = useState<Cond | undefined>();
  const [pinned, setPinned] = useState<PickerOption[]>([]);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!open) return;
    setKind(fact?.kind ?? null); setLabel(fact?.label ?? ''); setCfg(fact?.config ?? {}); setError('');
    const f = readFilter(fact?.config.filter as Cond | undefined);
    setCorr(f.corr); setWords(f.words.join('、')); setOther(f.other);
    setPinned([]);
  }, [open, fact]);

  // 固定文档：已选的标题
  useEffect(() => {
    if (!open || fact?.kind !== 'pinned' || !fact.config.documentIds?.length) return;
    void (async () => {
      const page = unwrap(await api.GET('/api/v1/documents', { params: { query: { filter: JSON.stringify({ field: 'drawer', op: 'in', value: [drawer.id] }), limit: 200 } } }));
      const byId = new Map(page.items.map((d) => [d.id, d.title]));
      setPinned((fact.config.documentIds ?? []).map((id) => ({ id, label: byId.get(id) ?? t('（不在这个抽屉里的文档）') })));
    })();
  }, [open, fact, drawer.id]);

  const dateFields = fields.filter((f) => f.dataType === 'date');
  const numberFields = fields.filter((f) => f.dataType === 'monetary' || f.dataType === 'integer' || f.dataType === 'float');
  const pick = (k: FactKind) => {
    setKind(k);
    setLabel((l) => l || DEFAULT_LABEL[k]);
    const builtin = (key: string) => fields.find((f) => f.key === key)?.id;
    if (k === 'due') setCfg({ fieldId: builtin('due_date') ?? dateFields[0]?.id, advanceDays: 30 });
    else if (k === 'sum') setCfg({ fieldId: builtin('amount') ?? numberFields[0]?.id, period: 'this_year' });
    else if (k === 'expect') setCfg({ period: 'monthly', graceDays: 7 });
    else setCfg({});
  };

  const submit = () => {
    if (!kind) return;
    const config: FactConfig = { ...cfg };
    if (kind !== 'pinned' && kind !== 'metric') config.filter = writeFilter(corr, splitWords(words), other);
    if (kind === 'pinned') config.documentIds = pinned.map((p) => p.id);
    const onError = (e: { message: string }) => setError(e.message);
    if (fact) update.mutate({ id: fact.id, label, config }, { onSuccess: () => { app.toast(t('已保存要点')); onClose(); }, onError });
    else add.mutate({ kind, label, config }, { onSuccess: () => { app.toast(t('已添加要点“{label}”', { label })); onClose(); }, onError });
  };

  const tags = tagOptions(ix).map((o) => ({ ...o, label: o.title ?? o.label }));
  const corrs = correspondentOptions(ix);
  const multi = (opts: PickerOption[], value: number[], onChange: (v: number[]) => void, lbl: string, help: string) => (
    <Autocomplete multiple size="small" options={opts} value={opts.filter((o) => value.includes(o.id))} getOptionLabel={(o) => o.label}
      isOptionEqualToValue={(a, b) => a.id === b.id} onChange={(_, v) => onChange(v.map((o) => o.id))} disableCloseOnSelect
      renderInput={(params) => <TextField {...params} label={lbl} helperText={help} />} />
  );
  const fieldSelect = (list: typeof fields, lbl: string, allowNone?: string) => (
    <TextField select label={lbl} value={cfg.fieldId ?? 0} onChange={(e) => setCfg((c) => ({ ...c, fieldId: Number(e.target.value) || undefined }))}>
      {allowNone && <MenuItem value={0}>{allowNone}</MenuItem>}
      {list.map((f) => <MenuItem key={f.id} value={f.id}>{f.name}</MenuItem>)}
    </TextField>
  );

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth aria-label={fact ? t('修改要点') : t('添加要点')}>
      <DialogTitle>{fact ? t('修改要点') : kind ? t('添加要点 · {kind}', { kind: FACT_KINDS.find((k) => k.kind === kind)?.label ?? '' }) : t('添加要点')}</DialogTitle>
      <DialogContent>
        {!kind ? (
          <Stack spacing={1} sx={{ pt: 0.5 }}>
            {FACT_KINDS.filter((k) => k.kind !== 'metric' || metrics.length > 0).map((k) => (
              <Card key={k.kind}>
                <CardActionArea onClick={() => pick(k.kind)} sx={{ p: 2 }}>
                  <Typography variant="h6">{k.label}</Typography>
                  <Typography variant="body2" sx={{ color: 'text.secondary' }}>{k.help}</Typography>
                </CardActionArea>
              </Card>
            ))}
          </Stack>
        ) : (
          <Stack spacing={2.5} sx={{ pt: 1 }}>
            <TextField autoFocus required label={t('名称')} value={label} onChange={(e) => setLabel(e.target.value)} helperText={t('显示在卡片上，例如“租约到期”“今年水电费”')} slotProps={{ htmlInput: { maxLength: 20 } }} />
            {kind === 'latest' && fieldSelect(fields, t('显示的字段'), t('只显示日期'))}
            {kind === 'metric' && (
              <>
                <Autocomplete size="small" options={metrics} value={metrics.find((m) => m.id === cfg.metricId) ?? null}
                  groupBy={(m) => m.category || t('其他')} getOptionLabel={(m) => m.name} isOptionEqualToValue={(a, b) => a.id === b.id}
                  onChange={(_, m) => { setCfg((c) => ({ ...c, metricId: m?.id })); if (m) setLabel((l) => l || m.name.slice(0, 20)); }}
                  renderInput={(params) => <TextField {...params} required label={t('指标')} helperText={t('只算核对过的结果，也只算你看得到的单子')} />} />
                <TextField select label={t('家人')} value={cfg.personId ?? 0} onChange={(e) => setCfg((c) => ({ ...c, personId: Number(e.target.value) || undefined }))}
                  helperText={t('不指定时显示最近测过的那位')}>
                  <MenuItem value={0}>{t('不指定')}</MenuItem>
                  {persons.map((p) => <MenuItem key={p.id} value={p.id}>{p.name}</MenuItem>)}
                </TextField>
              </>
            )}
            {kind === 'due' && fieldSelect(dateFields, t('日期字段'))}
            {kind === 'due' && (
              <TextField type="number" label={t('提前提醒（天）')} value={cfg.advanceDays ?? 30} onChange={(e) => setCfg((c) => ({ ...c, advanceDays: Number(e.target.value) }))}
                helperText={t('到期前这么多天起，首页会出现提醒')} slotProps={{ htmlInput: { min: 1, max: 366 } }} />
            )}
            {kind === 'sum' && fieldSelect(numberFields, t('加总的字段'))}
            {kind === 'sum' && (
              <TextField select label={t('周期')} value={cfg.period ?? 'this_year'} onChange={(e) => setCfg((c) => ({ ...c, period: e.target.value }))}>
                <MenuItem value="this_year">{t('今年')}</MenuItem><MenuItem value="last_12_months">{t('近 12 个月')}</MenuItem><MenuItem value="all">{t('全部')}</MenuItem>
              </TextField>
            )}
            {kind === 'expect' && (
              <Stack direction="row" spacing={2}>
                <TextField select fullWidth label={t('周期')} value={cfg.period ?? 'monthly'} onChange={(e) => setCfg((c) => ({ ...c, period: e.target.value }))}>
                  <MenuItem value="monthly">{t('每月一份')}</MenuItem><MenuItem value="quarterly">{t('每季度一份')}</MenuItem><MenuItem value="yearly">{t('每年一份')}</MenuItem>
                </TextField>
                <TextField fullWidth type="number" label={t('宽限（天）')} value={cfg.graceDays ?? 0} onChange={(e) => setCfg((c) => ({ ...c, graceDays: Number(e.target.value) }))}
                  helperText={t('周期开始后多少天还没有才算缺')} slotProps={{ htmlInput: { min: 0, max: 60 } }} />
              </Stack>
            )}
            {kind !== 'pinned' && kind !== 'metric' && (
              <>
                {multi(corrs, corr, setCorr, t('往来方'), t('只看这些往来方的文档，可以不选'))}
                <TextField label={t('关键词')} value={words} onChange={(e) => setWords(e.target.value)} helperText={t('标题或正文包含任意一个，用顿号分隔，可以不填')} />
                {other && <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('另有通过接口设置的条件，会保留。')}</Typography>}
              </>
            )}
            {kind === 'pinned' && <PinnedPicker drawerId={drawer.id} value={pinned} onChange={setPinned} />}
            {kind !== 'pinned' && kind !== 'metric' && multi(tags, cfg.tagIds ?? [], (v) => setCfg((c) => ({ ...c, tagIds: v.length ? v : undefined })), t('标签'), t('只统计带这些标签（含子标签）的文档，可以不选'))}
            {error && <Typography variant="body2" sx={{ color: 'error.main' }}>{error}</Typography>}
          </Stack>
        )}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        {kind && !fact && <Button onClick={() => setKind(null)} sx={{ mr: 'auto' }}>{t('换一种')}</Button>}
        <Button onClick={onClose}>{t('取消')}</Button>
        {kind && <Button variant="contained" disabled={!label.trim() || (kind === 'metric' && !cfg.metricId) || add.isPending || update.isPending} onClick={submit}>{fact ? t('保存') : t('添加')}</Button>}
      </DialogActions>
    </Dialog>
  );
}

/** 固定文档：在这个抽屉里按标题搜索，最多 10 份。 */
function PinnedPicker({ drawerId, value, onChange }: { drawerId: number; value: PickerOption[]; onChange: (v: PickerOption[]) => void }) {
  const [options, setOptions] = useState<PickerOption[]>([]);
  const search = useMemo(() => {
    let t = 0;
    return (q: string) => {
      window.clearTimeout(t);
      t = window.setTimeout(async () => {
        const conds: Cond[] = [{ field: 'drawer', op: 'in', value: [drawerId] }];
        if (q.trim()) conds.push({ text: q.trim() });
        const filter = JSON.stringify(conds.length === 1 ? conds[0] : { all: conds });
        const page = unwrap(await api.GET('/api/v1/documents', { params: { query: { filter, sort: 'created', dir: 'desc', limit: 20 } } }));
        setOptions(page.items.map((d) => ({ id: d.id, label: d.title })));
      }, 250);
    };
  }, [drawerId]);
  return (
    <Autocomplete multiple size="small" options={options} value={value} getOptionLabel={(o) => o.label} isOptionEqualToValue={(a, b) => a.id === b.id}
      filterOptions={(x) => x} onOpen={() => search('')} onInputChange={(_, q, reason) => reason === 'input' && search(q)}
      onChange={(_, v) => onChange(v.slice(0, 10))} noOptionsText={t('这个抽屉里没有找到文档')}
      renderInput={(params) => <TextField {...params} label={t('文档')} helperText={t('从这个抽屉里选，最多 {n} 份', { n: 10 })} />} />
  );
}

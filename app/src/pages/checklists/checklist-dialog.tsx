// 编辑办事材料清单（v1.6.2）：名称、说明，以及每一项的名称、要几份、备注和找文档的条件。
// 条件用收纳规则同一个条件编辑器；编辑器表示不了的手写条件保留不动，显示为“自定义条件”。
import { useEffect, useState } from 'react';
import { Alert, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, Stack, TextField, Tooltip, Typography } from '@mui/material';
import { ArrowDown, ArrowUp, ChevronDown, ChevronUp, Plus, Trash2 } from 'lucide-react';
import type { Cond } from '../../api/filters';
import { useSaveChecklist, type Checklist, type ChecklistInput } from '../../api/checklists';
import { useApp } from '../../store/app-store';
import { RuleRowsEditor } from '../../components/rule-rows';
import { SW } from '../../components/icons';
import { describeRule, ruleToRows, rowsToRule, type RuleRow } from '../../utils/drawer-rules';
import { useRuleNames } from '../drawer/drawer-page';
import { useTaxonomyIndex } from '../../components/pickers';
import { t } from '../../i18n';

/** 编辑中的一项：rows 是条件编辑器的行；custom 是编辑器表示不了的原条件。 */
interface Draft { id?: string; label: string; count: number; note: string; rows: RuleRow[]; custom?: Cond }

const toDraft = (it: ChecklistInput['items'][number]): Draft => {
  const rows = ruleToRows(it.filter as Cond);
  return { id: it.id, label: it.label, count: it.count ?? 1, note: it.note ?? '', rows: rows ?? [], custom: rows ? undefined : (it.filter as Cond) };
};

/** 新的一项：按名称当关键词找，用户可以改。 */
const blankItem = (label = ''): Draft => ({ label, count: 1, note: '', rows: label ? [{ join: 'and', field: 'text', op: 'in', values: [label] }] : [] });

/** 改名称时，条件还是“按名称找”（或还没有条件）就跟着改。 */
function relabel(d: Draft, label: string): Partial<Draft> {
  const auto = !d.custom && (d.rows.length === 0 || (d.rows.length === 1 && d.rows[0].field === 'text' && d.rows[0].op === 'in' && d.rows[0].values.length === 1 && d.rows[0].values[0] === d.label.trim()));
  const word = label.trim();
  return auto ? { label, rows: word ? [{ join: 'and', field: 'text', op: 'in', values: [word] }] : [] } : { label };
}

export function ChecklistDialog({ open, onClose, checklist, template, onSaved }: {
  open: boolean; onClose: () => void;
  /** 编辑已有的清单；新建时为空 */
  checklist?: Checklist;
  /** 新建时从模板开始 */
  template?: ChecklistInput;
  onSaved?: (c: Checklist) => void;
}) {
  const app = useApp();
  const save = useSaveChecklist();
  const names = useRuleNames(useTaxonomyIndex());
  const [name, setName] = useState('');
  const [note, setNote] = useState('');
  const [items, setItems] = useState<Draft[]>([]);
  const [editing, setEditing] = useState<number | null>(null);
  useEffect(() => {
    if (!open) return;
    const src = checklist ?? template;
    setName(src?.name ?? '');
    setNote(src?.note ?? '');
    setItems(src ? src.items.map(toDraft) : [blankItem()]);
    setEditing(src ? null : 0);
  }, [open, checklist, template]);

  const set = (i: number, p: Partial<Draft>) => setItems((xs) => xs.map((x, j) => (j === i ? { ...x, ...p } : x)));
  const move = (i: number, d: -1 | 1) => setItems((xs) => { const ys = [...xs]; [ys[i], ys[i + d]] = [ys[i + d], ys[i]]; return ys; });
  const filterOf = (d: Draft) => d.custom ?? rowsToRule(d.rows);
  const problem = !name.trim() ? t('请填写清单名称')
    : items.some((d) => !d.label.trim()) ? t('每一项都需要名称')
      : items.some((d) => !filterOf(d)) ? t('每一项都需要找文档的条件') : null;
  const submit = () => {
    const body: ChecklistInput = {
      name: name.trim(), note: note.trim(),
      items: items.map((d) => ({ ...(d.id ? { id: d.id } : {}), label: d.label.trim(), count: d.count, note: d.note.trim() || undefined, filter: filterOf(d)! })),
    };
    save.mutate({ id: checklist?.id, body }, {
      onSuccess: (c) => { app.toast(checklist ? t('已保存清单') : t('已新建清单“{name}”', { name: c.name })); onSaved?.(c); onClose(); },
    });
  };

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="md" aria-labelledby="checklist-title">
      <DialogTitle id="checklist-title">{checklist ? t('编辑清单') : t('新建清单')}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          <TextField label={t('名称')} value={name} onChange={(e) => setName(e.target.value)} placeholder={t('例如：办签证')} slotProps={{ htmlInput: { maxLength: 64 } }} />
          <TextField label={t('说明')} value={note} onChange={(e) => setNote(e.target.value)} multiline minRows={1} maxRows={4} slotProps={{ htmlInput: { maxLength: 1000 } }} />
          <Typography variant="h6" sx={{ pt: 1 }}>{t('要哪些材料')}</Typography>
          {items.map((d, i) => (
            <Box key={i} sx={{ border: 1, borderColor: 'divider', borderRadius: 2, p: 1.5 }}>
              <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5} sx={{ alignItems: { sm: 'flex-start' } }}>
                <TextField size="small" label={t('材料')} value={d.label} onChange={(e) => set(i, relabel(d, e.target.value))} sx={{ flex: 2 }}
                  slotProps={{ htmlInput: { maxLength: 64, 'aria-label': t('第 {n} 项的名称', { n: i + 1 }) } }} />
                <TextField size="small" type="number" label={t('要几份')} value={d.count} sx={{ width: { sm: 96 } }}
                  onChange={(e) => set(i, { count: Math.max(1, Math.min(24, Math.floor(Number(e.target.value) || 1))) })}
                  slotProps={{ htmlInput: { min: 1, max: 24, 'aria-label': t('第 {n} 项要几份', { n: i + 1 }) } }} />
                <TextField size="small" label={t('备注')} value={d.note} onChange={(e) => set(i, { note: e.target.value })} sx={{ flex: 2 }} placeholder={t('如：需要复印件')}
                  slotProps={{ htmlInput: { maxLength: 1000, 'aria-label': t('第 {n} 项的备注', { n: i + 1 }) } }} />
                <Stack direction="row" sx={{ flex: 'none' }}>
                  <Tooltip title={t('上移')}><span><IconButton size="small" aria-label={t('上移第 {n} 项', { n: i + 1 })} disabled={i === 0} onClick={() => move(i, -1)}><ArrowUp size={16} strokeWidth={SW} /></IconButton></span></Tooltip>
                  <Tooltip title={t('下移')}><span><IconButton size="small" aria-label={t('下移第 {n} 项', { n: i + 1 })} disabled={i === items.length - 1} onClick={() => move(i, 1)}><ArrowDown size={16} strokeWidth={SW} /></IconButton></span></Tooltip>
                  <Tooltip title={t('删除这一项')}><IconButton size="small" aria-label={t('删除第 {n} 项', { n: i + 1 })} onClick={() => setItems((xs) => xs.filter((_, j) => j !== i))}><Trash2 size={16} strokeWidth={SW} /></IconButton></Tooltip>
                </Stack>
              </Stack>
              <Button size="small" sx={{ mt: 1 }} endIcon={editing === i ? <ChevronUp size={14} strokeWidth={SW} /> : <ChevronDown size={14} strokeWidth={SW} />}
                onClick={() => setEditing(editing === i ? null : i)}>
                <Box component="span" sx={{ textAlign: 'left', fontWeight: 400, color: filterOf(d) ? 'text.secondary' : 'warning.main' }}>
                  {t('找文档：{rule}', { rule: d.custom ? t('自定义条件') : filterOf(d) ? describeRule(filterOf(d), names) : t('还没有条件') })}
                </Box>
              </Button>
              {editing === i && (
                <Box sx={{ mt: 1 }}>
                  {d.custom
                    ? <Alert severity="info" action={<Button size="small" onClick={() => set(i, { custom: undefined, rows: [] })}>{t('重新设置')}</Button>}>{t('这一项的条件是手写的，这里表示不了。')}</Alert>
                    : <RuleRowsEditor rows={d.rows} onChange={(rows) => set(i, { rows })} empty={t('还没有条件')} hint={t('满足条件的文档里，取最新的几份。')} />}
                </Box>
              )}
            </Box>
          ))}
          <Button size="small" variant="outlined" startIcon={<Plus size={14} strokeWidth={SW} />} sx={{ alignSelf: 'flex-start' }}
            disabled={items.length >= 50} onClick={() => { setItems((xs) => [...xs, blankItem()]); setEditing(items.length); }}>{t('添加一项')}</Button>
          {save.error && <Alert severity="error">{save.error.message}</Alert>}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        {problem && <Typography variant="caption" sx={{ color: 'text.secondary', mr: 'auto' }}>{problem}</Typography>}
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={!!problem || save.isPending} onClick={submit}>{checklist ? t('保存') : t('新建')}</Button>
      </DialogActions>
    </Dialog>
  );
}

// 年度回顾增强（阶段 21）：与上一年的差、近五年、存储占用、大事记规则的编辑。
import { useEffect, useRef, useState } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import { Alert, Box, Button, Card, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, Link, Rating, Stack, TextField, Typography } from '@mui/material';
import { Plus, Trash2 } from 'lucide-react';
import { useQueryClient } from '@tanstack/react-query';
import { useMilestoneRules, useSetMilestoneRules, type Yearbook } from '../api/drawers';
import { useWriteNarrative } from '../api/agents';
import type { Cond } from '../api/filters';
import { RuleRowsEditor } from '../components/rule-rows';
import { AiMark, SW } from '../components/icons';
import { BookCover } from '../components/book-cover';
import { YearsChart } from '../components/charts-lazy';
import { useApp } from '../store/app-store';
import { ruleToRows, rowsToRule, type RuleRow } from '../utils/drawer-rules';
import { moneyInt } from '../utils/facts';
import { bytes } from './settings/system';
import { currentLang, t } from '../i18n';

/** 月份：中文用数字（“9 月最忙”里的 9），英文用月名。 */
export const monthName = (m: number) => (currentLang() === 'en' ? new Intl.DateTimeFormat('en', { month: 'long' }).format(new Date(2000, m - 1, 1)) : String(m));

/** “+3”“−¥120”；没有变化时不显示。 */
export function Delta({ now, before, money, unit = '' }: { now: number; before: number | undefined; money?: boolean; unit?: string }) {
  if (before === undefined) return null;
  const d = now - before;
  if (Math.round(d) === 0) return null;
  const text = money ? moneyInt(String(Math.abs(d))) : `${Math.abs(d)}${unit}`;
  return (
    <Typography component="span" variant="caption" sx={{ color: 'text.secondary', ml: 0.75, whiteSpace: 'nowrap' }} title={t('与上一年相比')}>
      {d > 0 ? '+' : '−'}{text}
    </Typography>
  );
}

/** 开篇的一句对比。 */
export function compareText(y: Yearbook): string {
  const p = y.previous;
  if (!p || (p.documents === 0 && y.documents === 0)) return '';
  if (p.documents === 0) return t('{year} 年没有文档', { year: p.year });
  const d = y.documents - p.documents;
  if (d === 0) return t('和 {year} 年一样多', { year: p.year });
  const vars = { year: p.year, n: Math.abs(d), pct: Math.round((Math.abs(d) / p.documents) * 100) };
  return d > 0 ? t('比 {year} 年多 {n} 份（+{pct}%）', vars) : t('比 {year} 年少 {n} 份（−{pct}%）', vars);
}

export function HistoryBars({ y, onPick }: { y: Yearbook; onPick: (year: number) => void }) {
  const h = y.history ?? [];
  if (h.every((c) => c.documents === 0)) return null;
  return (
    <Box sx={{ mt: 4 }}>
      <Typography variant="h6" sx={{ color: 'text.secondary', mb: 1 }}>{t('近五年')}</Typography>
      <YearsChart years={h} current={y.year} onPick={onPick} />
    </Box>
  );
}

export function StorageSection({ y }: { y: Yearbook }) {
  const st = y.storage;
  if (!st) return null;
  const parts = [[t('原件'), st.originals], [t('可搜索版'), st.searchable], [t('缩略图'), st.thumbnails], [t('历史版本'), st.versions]] as const;
  return (
    <Box component="section" aria-label={t('存储')} sx={{ mt: 5 }}>
      <Typography variant="h5" sx={{ mb: 1 }}>{t('存储')}</Typography>
      <Typography>{withBold(t('这一年的文档占用 {size}，整个空间一共 {total}。', { size: MARK, total: bytes(st.workspace) }), bytes(st.total))}</Typography>
      <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5 }}>{parts.filter(([, n]) => n > 0).map(([l, n]) => `${l} ${bytes(n)}`).join(' · ')}</Typography>
      {st.largest.length > 0 && (
        <Box sx={{ mt: 1.5 }}>
          <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('最大的文档')}</Typography>
          {st.largest.map((d) => (
            <Stack key={d.id} direction="row" sx={{ alignItems: 'baseline', gap: 1, minHeight: 32 }}>
              <Link component={RouterLink} to={`/docs/${d.id}`} underline="hover" sx={{ overflowWrap: 'anywhere' }}>{d.title}</Link>
              <Typography variant="body2" sx={{ color: 'text.secondary', whiteSpace: 'nowrap' }}>{bytes(d.bytes)}</Typography>
            </Stack>
          ))}
        </Box>
      )}
    </Box>
  );
}

const MARK = '\u0001';
/** 句子里的 {size} 用粗体显示（先用 MARK 占位，再拆开）。 */
function withBold(sentence: string, bold: string) {
  const [pre, post = ''] = sentence.split(MARK);
  return <>{pre}<Box component="span" sx={{ fontWeight: 500 }}>{bold}</Box>{post}</>;
}

interface EditRule { verb: string; rows: RuleRow[]; custom?: Cond }

/** 大事记规则：每条是动词加文档条件；可以恢复默认。 */
export function MilestoneRulesDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const app = useApp();
  const rules = useMilestoneRules(open);
  const save = useSetMilestoneRules();
  const [list, setList] = useState<EditRule[]>([]);
  useEffect(() => {
    if (!open || !rules.data) return;
    save.reset();
    setList(rules.data.rules.map((r) => {
      const rows = ruleToRows(r.filter as Cond);
      return rows ? { verb: r.verb, rows } : { verb: r.verb, rows: [], custom: r.filter as Cond };
    }));
  }, [open, rules.data]); // eslint-disable-line react-hooks/exhaustive-deps
  const set = (i: number, p: Partial<EditRule>) => setList((l) => l.map((r, j) => (j === i ? { ...r, ...p } : r)));
  const submit = () => save.mutate({ rules: list.map((r) => ({ verb: r.verb.trim(), filter: (r.custom ?? rowsToRule(r.rows) ?? {}) as Record<string, unknown> })) }, {
    onSuccess: () => { app.toast(t('已保存大事记规则')); onClose(); },
  });
  const reset = () => save.mutate({ reset: true }, { onSuccess: () => { app.toast(t('已恢复默认的大事记规则')); onClose(); } });
  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm" aria-labelledby="rules-title">
      <DialogTitle id="rules-title">{t('大事记规则')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 2 }}>
          {t('符合条件的文档会按“动词 + 标题”列入大事记，例如“签订 房屋租赁合同（链家）”。一份文档按第一条符合的规则。标记为大事的文档和新建的抽屉总会列入。')}
        </Typography>
        {save.error && <Alert severity="error" sx={{ mb: 2 }}>{save.error.message}</Alert>}
        <Stack spacing={2}>
          {list.map((r, i) => (
            <Card key={i} variant="outlined" sx={{ p: 2 }}>
              <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mb: 1.5 }}>
                <TextField size="small" label={t('动词')} value={r.verb} onChange={(e) => set(i, { verb: e.target.value })} slotProps={{ htmlInput: { maxLength: 8 } }} sx={{ width: 140 }} />
                <Box sx={{ flex: 1 }} />
                <IconButton size="small" aria-label={t('删除第 {n} 条规则', { n: i + 1 })} onClick={() => setList((l) => l.filter((_, j) => j !== i))}><Trash2 size={16} strokeWidth={SW} /></IconButton>
              </Stack>
              {r.custom
                ? <Alert severity="info">{t('这条规则的条件是通过接口写的，编辑器表示不了，会原样保留。')}</Alert>
                : <RuleRowsEditor rows={r.rows} onChange={(rows) => set(i, { rows })} empty={t('还没有条件。')} hint={t('“且”比“或”优先。关键词在标题和正文里查找。')} />}
            </Card>
          ))}
          {list.length < 10 && <Box><Button size="small" variant="outlined" startIcon={<Plus size={14} strokeWidth={SW} />} onClick={() => setList((l) => [...l, { verb: '', rows: [] }])}>{t('添加规则')}</Button></Box>}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={reset} disabled={save.isPending || rules.data?.default} sx={{ mr: 'auto' }}>{t('恢复默认')}</Button>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={save.isPending || list.some((r) => !r.verb.trim())} onClick={submit}>{t('保存')}</Button>
      </DialogActions>
    </Dialog>
  );
}

/** 读完的书（阶段 28）：自己这一年读完的书，按读完的日期；只有自己看得到。 */
export function BooksSection({ y }: { y: Yearbook }) {
  const books = y.books ?? [];
  if (!books.length) return null;
  return (
    <Box component="section" aria-label={t('读完的书')} sx={{ mt: 5 }}>
      <Stack direction="row" sx={{ alignItems: 'baseline', gap: 1, mb: 1.5 }}>
        <Typography variant="h5">{t('读完的书')}</Typography>
        <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('{n} 本', { n: books.length })} · {t('只有你自己看得到')}</Typography>
      </Stack>
      <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(96px, 1fr))', gap: 2 }}>
        {books.map((b) => (
          <Box key={b.id} component={RouterLink} to={`/docs/${b.id}`} sx={{ color: 'inherit', textDecoration: 'none', breakInside: 'avoid' }}>
            <BookCover id={b.id} title={b.title} hasCover={b.hasCover} hasThumbnail={b.hasThumbnail} />
            <Typography variant="body2" sx={{ mt: 0.75, display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>{b.title}</Typography>
            <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block' }}>{t('{month} 月读完', { month: monthName(Number(b.finishedOn.slice(5, 7))) })}</Typography>
            {b.rating > 0 && <Rating value={b.rating} readOnly size="small" aria-label={t('{n} 星', { n: b.rating })} />}
          </Box>
        ))}
      </Box>
    </Box>
  );
}

/** 抽屉管家写的年度回顾叙述（阶段 32）：注明由 AI 生成；数据变了提示可以重写。打印时保留叙述，隐藏按钮。 */
export function NarrativeSection({ y, canWrite }: { y: Yearbook; canWrite: boolean }) {
  const app = useApp();
  const qc = useQueryClient();
  const write = useWriteNarrative();
  // 写的时候每 2 秒看一次，写好（时间变了）或一分钟后停
  const [waiting, setWaiting] = useState<string | null>(null);
  const done = waiting !== null && (y.narrative?.createdAt ?? '') !== waiting;
  useEffect(() => { if (done) setWaiting(null); }, [done]);
  const toast = useRef(app.toast);
  toast.current = app.toast;
  const polling = waiting !== null;
  useEffect(() => {
    if (!polling) return;
    const tick = setInterval(() => void qc.invalidateQueries({ queryKey: ['yearbook', y.year] }), 2000);
    const stop = setTimeout(() => { setWaiting(null); toast.current(t('AI 还没有写好，请稍后再看')); }, 60_000);
    return () => { clearInterval(tick); clearTimeout(stop); };
  }, [polling, qc, y.year]);
  const n = y.narrative;
  if (!n && (!y.narrativeAvailable || !canWrite || y.documents === 0)) return null;
  const start = () => write.mutate(y.year, {
    onSuccess: () => setWaiting(n?.createdAt ?? ''),
    onError: (e) => app.toast(e.message),
  });
  const busy = waiting !== null || write.isPending;
  return (
    <Box component="section" aria-label={t('AI 叙述')} sx={{ mt: 3, p: 2, borderRadius: 3, bgcolor: 'surface.containerLow', printColorAdjust: 'exact' }}>
      {n && <Typography sx={{ lineHeight: 1.8 }}>{n.text}</Typography>}
      {!n && <Typography variant="body2" sx={{ color: 'text.secondary' }}>{busy ? t('AI 正在写……') : t('让 AI 根据这一年的文档写一段回顾。')}</Typography>}
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mt: 1 }}>
        {n && <Typography variant="caption" sx={{ color: 'text.secondary', display: 'flex', alignItems: 'center' }}>{t('由 AI 生成')}<AiMark />{n.stale ? ` · ${t('数据有更新')}` : ''}</Typography>}
        <Box sx={{ flex: 1 }} />
        {canWrite && y.narrativeAvailable && (
          <Button className="yearbook-actions" size="small" disabled={busy} onClick={start}>{busy ? t('正在写…') : n ? t('重写') : t('写一段')}</Button>
        )}
      </Stack>
    </Box>
  );
}

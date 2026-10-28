// 抽屉页（UI 设计规范 5.3）：页头（编辑收纳规则、改名换图标、删除）、一句话的收纳规则、要点卡片（点击筛选下方列表、
// 拖动排序、添加 / 修改 / 删除），以及范围限定在本抽屉的文档列表。
import { useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Box, Button, Card, CardActionArea, Chip, IconButton, Menu, MenuItem, Stack, ToggleButton, ToggleButtonGroup, Tooltip, Typography } from '@mui/material';
import { Ellipsis, FileText, LayoutGrid, List as ListIcon, Lock, Plus, TriangleAlert } from 'lucide-react';
import { useDeleteDrawer, useDeleteFact, useDrawer, useReorderFacts, type DrawerDetail, type FactValue } from '../../api/drawers';
import type { Cond } from '../../api/filters';
import { useTaxonomy } from '../../api/taxonomy';
import { useRole } from '../../api/workspaces';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import DocList from '../../components/doc-list';
import { DrawerDialog, FactDialog } from '../../components/drawer-dialogs';
import { ShareDialog, FactSuggestions, eeBuild } from '@ee';
import { RulePanel } from '../../components/drawer-rule-panel';
import { DrawerIcon, SW } from '../../components/icons';
import { useTaxonomyIndex, type TaxonomyIndex } from '../../components/pickers';
import { useApp } from '../../store/app-store';
import { useDisableLibrary } from '../../api/books';
import { LibraryWall } from './library-wall';
import { describeRule, type Names } from '../../utils/drawer-rules';
import { factView } from '../../utils/facts';
import { MetricSpark } from '../../components/charts-lazy';
import { t } from '../../i18n';

/** 规则说明里用到的名称。 */
export function useRuleNames(ix: TaxonomyIndex): Names {
  const fields = useTaxonomy('custom-fields').data ?? [];
  return {
    type: (id) => ix.types.get(id)?.name,
    correspondent: (id) => ix.correspondents.get(id)?.name,
    person: (id) => ix.persons.get(id)?.name,
    tag: (id) => ix.tagTree.find((x) => x.tag.id === id)?.path,
    field: (id) => {
      const f = fields.find((x) => x.id === id);
      return f && { name: f.name, option: (o: string) => f.extra.options?.find((x) => x.id === o)?.label };
    },
    customField: (id) => fields.find((x) => x.id === id),
  };
}

export default function DrawerPage() {
  const id = Number(useParams().id);
  const q = useDrawer(id);
  if (q.isPending) return <LoadingState />;
  if (q.error?.status === 404) return <NotFound />;
  if (q.error || !q.data) return <ErrorState onRetry={() => void q.refetch()} />;
  return <Drawer d={q.data} key={id} />;
}

const VIEW_KEY = 'kept-library-view';
const readView = (): 'covers' | 'list' => { try { return localStorage.getItem(VIEW_KEY) === 'list' ? 'list' : 'covers'; } catch { return 'covers'; } };

function NotFound() {
  const nav = useNavigate();
  return <EmptyState title={t('找不到这个抽屉')} text={t('它可能已被删除，或属于另一个空间。')} action={t('回到首页')} onAction={() => nav('/')} />;
}

function Drawer({ d }: { d: DrawerDetail }) {
  const app = useApp();
  const nav = useNavigate();
  const role = useRole();
  const ix = useTaxonomyIndex();
  const names = useRuleNames(ix);
  const [ruleOpen, setRuleOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [factEdit, setFactEdit] = useState<{ open: boolean; fact?: FactValue['fact'] }>({ open: false });
  const [menuEl, setMenuEl] = useState<HTMLElement | null>(null);
  const [shareOpen, setShareOpen] = useState(false);
  const del = useDeleteDrawer();
  const disable = useDisableLibrary();
  const listKey = `drawer:${d.id}`;
  const library = d.kind === 'library';
  const [view, setView] = useState<'covers' | 'list'>(readView);
  const pickView = (v: 'covers' | 'list') => { setView(v); try { localStorage.setItem(VIEW_KEY, v); } catch { /* 隐私模式 */ } };

  const remove = () => app.confirm({
    title: t('删除抽屉“{name}”？', { name: d.name }),
    body: t('其中的 {n} 份文档不会被删除，会交给其他抽屉的收纳规则重新整理（手动放入的也一样）。抽屉的要点和提醒会一起删除。', { n: d.documentCount }),
    verb: t('删除'),
    run: () => {
      app.confirm(null);
      del.mutate(d.id, { onSuccess: () => { app.toast(t('已删除抽屉“{name}”', { name: d.name })); nav('/'); }, onError: (e) => app.toast(e.message) });
    },
  });
  const close = () => app.confirm({
    title: t('关闭书房？'),
    body: t('“{name}”抽屉会删除，其中的 {n} 本书交给其他抽屉的收纳规则重新整理。书目信息保留，之后可以在新建抽屉时重新开启书房。', { name: d.name, n: d.documentCount }),
    verb: t('关闭'),
    run: () => {
      app.confirm(null);
      disable.mutate(undefined, { onSuccess: () => { app.toast(t('已关闭书房')); nav('/'); }, onError: (e) => app.toast(e.message) });
    },
  });
  const years = d.firstYear ? (d.firstYear === d.lastYear ? ` · ${d.firstYear}` : ` · ${d.firstYear}–${d.lastYear}`) : '';
  const m = d.manualCount;
  const n = d.documentCount;
  const summary = library
    ? (m ? t('{n} 本 · 收纳：所有书籍；另有手动放入 {m} 份', { n, m }) : t('{n} 本 · 收纳：所有书籍', { n }))
    : (m ? t('{n} 份{years} · 收纳：{rule}；另有手动放入 {m} 份', { n, m, years, rule: describeRule(d.rule as Cond | undefined, names) })
      : t('{n} 份{years} · 收纳：{rule}', { n, years, rule: describeRule(d.rule as Cond | undefined, names) }));

  return (
    <Box sx={{ px: 3, pt: 0.5 }}>
      <Stack direction="row" spacing={1.5} useFlexGap sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
        <DrawerIcon drawer={d} size={40} shared={!!d.sharedCount} />
        <Typography variant="h4" component="h1">{d.name}</Typography>
        {d.sensitive && (
          <Tooltip title={t('敏感抽屉：里面的文档打开前要确认密码，不能分享，也不交给 AI')}>
            <Chip size="small" icon={<Lock size={14} strokeWidth={SW} />} label={t('敏感')} />
          </Tooltip>
        )}
        <Box sx={{ flex: 1 }} />
        {library && <Button onClick={() => nav('/book-lists')}>{t('书单')}</Button>}
        {library && <Button onClick={() => nav('/highlights')}>{t('我的批注')}</Button>}
        {library && (
          <ToggleButtonGroup size="small" exclusive value={view} onChange={(_, v) => v && pickView(v)} aria-label={t('显示方式')}>
            <ToggleButton value="covers" aria-label={t('封面')}><LayoutGrid size={16} strokeWidth={SW} /></ToggleButton>
            <ToggleButton value="list" aria-label={t('列表')}><ListIcon size={16} strokeWidth={SW} /></ToggleButton>
          </ToggleButtonGroup>
        )}
        {role.canWrite && !library && <Button variant="outlined" onClick={() => setRuleOpen(true)}>{t('编辑收纳规则')}</Button>}
        {role.canWrite && (
          <>
            <Tooltip title={t('更多')}><IconButton aria-label={t('更多')} onClick={(e) => setMenuEl(e.currentTarget)}><Ellipsis size={20} strokeWidth={SW} /></IconButton></Tooltip>
            <Menu anchorEl={menuEl} open={!!menuEl} onClose={() => setMenuEl(null)}>
              <MenuItem sx={{ minWidth: 160 }} onClick={() => { setMenuEl(null); setEditOpen(true); }}>{t('修改抽屉（名称、图标、敏感）')}</MenuItem>
              {eeBuild && role.isOwner && <MenuItem onClick={() => { setMenuEl(null); setShareOpen(true); }}>{d.sharedCount ? t('共享给其他空间（{n}）', { n: d.sharedCount }) : t('共享给其他空间')}</MenuItem>}
              {library
                ? <MenuItem sx={{ color: 'error.main' }} onClick={() => { setMenuEl(null); close(); }}>{t('关闭书房')}</MenuItem>
                : <MenuItem sx={{ color: 'error.main' }} onClick={() => { setMenuEl(null); remove(); }}>{t('删除抽屉')}</MenuItem>}
            </Menu>
          </>
        )}
      </Stack>
      <Typography variant="body2" sx={{ color: 'text.secondary', mt: 1 }}>
        {summary}
      </Typography>
      {d.description && <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.5 }}>{d.description}</Typography>}

      <Facts d={d} canWrite={role.canWrite} onEdit={(fact) => setFactEdit({ open: true, fact })} onAdd={() => setFactEdit({ open: true })}
        onFilter={(v) => app.setDocList(listKey, (l) => ({ ...l, extra: v.filter as Cond, extraLabel: v.fact.label, sel: [] }))}
        onOpenDoc={(docId) => nav(`/docs/${docId}`)} />
      {role.canWrite && <FactSuggestions drawerId={d.id} />}

      {library && view === 'covers'
        ? <LibraryWall drawerId={d.id} name={d.name} />
        : d.documentCount === 0 && !d.rule
        ? <EmptyState title={t('这个抽屉还没有文档')} text={t('编写收纳规则，符合的文档会自动放进来；也可以在文档列表或详情页里手动放入。')} action={role.canWrite ? t('编辑收纳规则') : undefined} onAction={() => setRuleOpen(true)} />
        : <DocList listKey={listKey} base={[{ field: 'drawer', op: 'in', value: [d.id] }]} hideGroups={['drawer']} placeholder={t('在{name}中搜索…', { name: d.name })} backLabel={d.name}
          emptyTitle={t('这个抽屉还没有文档')} emptyText={t('调整收纳规则，或把文档手动放进来。')} />}
      {role.canWrite && <RulePanel drawer={d} open={ruleOpen} onClose={() => setRuleOpen(false)} />}
      <DrawerDialog drawer={d} open={editOpen} onClose={() => setEditOpen(false)} />
      {role.isOwner && <ShareDialog drawer={d} open={shareOpen} onClose={() => setShareOpen(false)} />}
      <FactDialog drawer={d} fact={factEdit.fact} open={factEdit.open} onClose={() => setFactEdit({ open: false })} />
    </Box>
  );
}

function Facts({ d, canWrite, onEdit, onAdd, onFilter, onOpenDoc }: {
  d: DrawerDetail; canWrite: boolean; onEdit: (f: FactValue['fact']) => void; onAdd: () => void; onFilter: (v: FactValue) => void; onOpenDoc: (id: number) => void;
}) {
  const app = useApp();
  const nav = useNavigate();
  const reorder = useReorderFacts(d.id);
  const delFact = useDeleteFact();
  const [drag, setDrag] = useState<number | null>(null);
  const [menu, setMenu] = useState<{ el: HTMLElement; v: FactValue } | null>(null);
  const drop = (target: number) => {
    if (drag === null || drag === target) return;
    const ids = d.facts.map((f) => f.fact.id).filter((x) => x !== drag);
    ids.splice(ids.indexOf(target), 0, drag);
    reorder.mutate(ids, { onError: (e) => app.toast(e.message) });
    setDrag(null);
  };
  return (
    <Stack direction="row" spacing={1.5} useFlexGap sx={{ flexWrap: 'wrap', mt: 2.5, mb: 3 }}>
      {d.facts.map((v, i) => {
        const fv = factView(v);
        return (
          <Card key={v.fact.id} draggable={canWrite} onDragStart={() => setDrag(v.fact.id)} onDragOver={(e) => e.preventDefault()} onDrop={() => drop(v.fact.id)}
            aria-label={i === 0 ? t('要点：{label}（首要）', { label: fv.label }) : t('要点：{label}', { label: fv.label })}
            sx={{ position: 'relative', width: 208, border: 0, bgcolor: 'surface.containerLow', transition: 'box-shadow 150ms', '&:hover': { boxShadow: v.filter ? 1 : 0 }, '&:hover .fact-more': { opacity: 1 } }}>
            <CardActionArea disabled={!v.filter && !fv.links && !fv.href} onClick={() => (fv.href ? nav(fv.href) : v.filter && onFilter(v))}
              sx={{ p: 2, minHeight: 112, display: 'flex', flexDirection: 'column', alignItems: 'flex-start', justifyContent: 'flex-start' }}>
              <Typography variant="caption" sx={{ color: 'text.secondary', pr: 3 }}>{fv.label}</Typography>
              {fv.value && (
                <Typography sx={{ display: 'flex', alignItems: 'center', gap: 0.75, fontSize: 18, lineHeight: '26px', fontWeight: 500, fontVariantNumeric: 'tabular-nums', color: fv.warn ? 'warning.main' : 'text.primary' }}>
                  {fv.value}{fv.warn && <TriangleAlert size={16} strokeWidth={SW} />}
                </Typography>
              )}
              {fv.note && <Typography variant="caption" sx={{ color: 'text.secondary' }}>{fv.note}</Typography>}
              {fv.spark && <MetricSpark values={fv.spark} width={170} height={24} label={t('{label} 最近 {n} 次', { label: fv.label, n: fv.spark.length })} />}
              {fv.bars && (
                <Box aria-hidden sx={{ display: 'flex', alignItems: 'flex-end', gap: '3px', height: 24, mt: 0.75, width: '100%' }}>
                  {fv.bars.map((b, j) => <Box key={j} title={b.title} sx={{ flex: 1, height: b.h, bgcolor: 'primary.main', opacity: 0.55, borderRadius: 0.5 }} />)}
                </Box>
              )}
              {fv.links && (
                <Stack sx={{ mt: 0.5 }}>
                  {fv.links.map((l) => (
                    <Box key={l.id} component="span" role="link" onClick={(e) => { e.stopPropagation(); onOpenDoc(l.id); }}
                      sx={{ display: 'flex', alignItems: 'center', gap: 0.75, height: 26, fontSize: 13, color: 'primary.main', cursor: 'pointer', '&:hover': { textDecoration: 'underline' } }}>
                      <FileText size={14} strokeWidth={SW} />{l.title}
                    </Box>
                  ))}
                </Stack>
              )}
            </CardActionArea>
            {canWrite && (
              <IconButton className="fact-more" size="small" aria-label={t('要点“{label}”的操作', { label: fv.label })} onClick={(e) => setMenu({ el: e.currentTarget, v })}
                sx={{ position: 'absolute', right: 4, top: 4, opacity: 0, '&:focus-visible': { opacity: 1 } }}>
                <Ellipsis size={16} strokeWidth={SW} />
              </IconButton>
            )}
          </Card>
        );
      })}
      {canWrite && (
        <Tooltip title={t('添加要点')}>
          <Box component="button" aria-label={t('添加要点')} onClick={onAdd}
            sx={{ width: 48, minHeight: 112, border: '1px dashed', borderColor: 'divider', borderRadius: 3, bgcolor: 'transparent', color: 'text.secondary', cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center', '&:hover': { bgcolor: 'action.hover' } }}>
            <Plus size={18} strokeWidth={SW} />
          </Box>
        </Tooltip>
      )}
      {!canWrite && d.facts.length === 0 && <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('这个抽屉还没有要点。')}</Typography>}
      <Menu anchorEl={menu?.el} open={!!menu} onClose={() => setMenu(null)}>
        <MenuItem sx={{ minWidth: 140 }} onClick={() => { const f = menu!.v.fact; setMenu(null); onEdit(f); }}>{t('修改')}</MenuItem>
        <MenuItem sx={{ color: 'error.main' }} onClick={() => {
          const f = menu!.v.fact; setMenu(null);
          app.confirm({ title: t('删除要点“{label}”？', { label: f.label }), body: t('要点产生的提醒也会一起删除，文档不受影响。'), verb: t('删除'),
            run: () => { app.confirm(null); delFact.mutate(f.id, { onError: (e) => app.toast(e.message) }); } });
        }}>{t('删除')}</MenuItem>
      </Menu>
    </Stack>
  );
}

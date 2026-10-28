// 首页（UI 设计规范 5.2）：问候、待整理提示、提醒（忽略可撤销）、我的抽屉（首要要点、提醒圆点）、最近添加、年度回顾入口。
// 还没有抽屉时（v1.3.4）首页照样完整显示，“我的抽屉”里按场景勾选模板一次建好；还没有文档时显示上传区。
// 已有抽屉后（v1.3.5）“按场景添加”照样能打开场景卡片，已经建过的模板标为“已有”。
import { useState } from 'react';
import { Link as RouterLink, useNavigate } from 'react-router-dom';
import { docHref, docLinkClick } from '../components/doc-link';
import { Alert as MuiAlert, Box, Button, Card, CardActionArea, Checkbox, Paper, Skeleton, Stack, Typography } from '@mui/material';
import { ArrowLeft, ArrowRight, CalendarDays, CircleCheck, Circle, FileText, HeartPulse, Inbox, TriangleAlert, Upload } from 'lucide-react';
import { useBranding } from '../api/branding';
import { useInboxDocuments, useRecentDocuments } from '../api/documents';
import { useAlerts, useDrawers, useDrawerTemplates, useFromTemplates, useSetAlert, type Alert, type DrawerList } from '../api/drawers';
import { ALERT_KIND_LABEL, useHealthAlerts, useHealthOverview, useMetrics, useSetHealthAlert, type HealthAlert } from '../api/health';
import { useMe } from '../api/session';
import { useDevices } from '../api/devices';
import { useRole } from '../api/workspaces';
import { ErrorState } from '../components/common';
import { DrawerDialog } from '../components/drawer-dialogs';
import { AiMark, DrawerIcon, NamedIcon, SW } from '../components/icons';
import { quotaShare, useStorage } from '../api/storage';
import { bytes } from './settings/system';
import { useTaxonomyIndex } from '../components/pickers';
import { useApp } from '../store/app-store';
import { factView } from '../utils/facts';
import { currentLang, t } from '../i18n';
import { eeBuild } from '@ee';

export function greeting() {
  const h = new Date().getHours();
  return h < 6 ? t('晚上好') : h < 11 ? t('上午好') : h < 13 ? t('中午好') : h < 18 ? t('下午好') : t('晚上好');
}

export default function HomePage() {
  const drawers = useDrawers();
  if (drawers.isPending) return <Box sx={{ px: 3, pt: 1 }}><Skeleton width={240} height={48} /><Skeleton height={120} /></Box>;
  if (drawers.error) return <ErrorState onRetry={() => void drawers.refetch()} />;
  return <Home />;
}

/** 在“我的抽屉”里按场景建抽屉：还没有抽屉时常驻，已有抽屉时由“按场景添加”打开（onClose 收起）。 */
function DrawerSetup({ existing, onClose }: { existing?: { templateKey?: string; name: string }[]; onClose?: () => void }) {
  const app = useApp();
  const role = useRole();
  const tpls = useDrawerTemplates();
  const create = useFromTemplates();
  const [scene, setScene] = useState<string | null>(null);
  const [pick, setPick] = useState<string[]>([]);
  const [blank, setBlank] = useState(false);
  const sc = tpls.data?.scenes.find((s) => s.key === scene);
  const tplOf = (k: string) => tpls.data?.templates.find((tp) => tp.key === k);
  // 已经建过的模板（按模板或同名判断；服务端遇到同名抽屉也会跳过）
  const has = (k: string) => !!existing?.some((d) => d.templateKey === k || d.name === tplOf(k)?.name);
  const choose = (key: string, templates: string[]) => { setScene(key); setPick(templates.filter((k) => !has(k))); };
  const go = () => create.mutate(pick, {
    onSuccess: (r) => { app.toast(t('已创建 {n} 个抽屉，正在把已有的文档放进去', { n: r.drawers.length })); setScene(null); onClose?.(); },
    onError: (e) => app.toast(e.message),
  });
  return (
    <Stack spacing={2}>
      {!role.canWrite ? (
        <Card sx={{ p: 2 }}><Typography sx={{ color: 'text.secondary' }}>{t('这个空间还没有抽屉。空间的成员或管理员建好抽屉后，这里会显示各个抽屉和它们的要点。')}</Typography></Card>
      ) : !sc ? (
        <>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-start' }}>
            <Typography sx={{ color: 'text.secondary', flex: 1 }}>{onClose
              ? t('选一个场景，补上还没有的抽屉。已经建过的会标为“已有”，不会重复创建。')
              : t('还没有抽屉。一类事放一个抽屉，文档会按规则自动放进去。你主要用它来管理什么？选一个场景，我们推荐一组抽屉，之后随时可以修改。')}</Typography>
            {onClose && <Button size="small" onClick={onClose} sx={{ color: 'text.secondary', flexShrink: 0 }}>{t('收起')}</Button>}
          </Stack>
          <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(240px,1fr))', gap: 1.5 }}>
            {(tpls.data?.scenes ?? []).map((S) => (
              <Card key={S.key} sx={{ transition: 'box-shadow 150ms, border-color 150ms', '&:hover': { boxShadow: 1, borderColor: 'primary.main' } }}>
                <CardActionArea onClick={() => choose(S.key, S.templates)} sx={{ p: 2.25, display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 1.25, height: '100%' }}>
                  <Box sx={{ width: 40, height: 40, borderRadius: 2, bgcolor: 'surface.containerHigh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}><NamedIcon name={S.icon as 'home'} size={20} /></Box>
                  <Typography variant="h5">{S.name}</Typography>
                  <Typography variant="body2" sx={{ color: 'text.secondary' }}>{S.description}</Typography>
                  <Typography variant="caption" sx={{ color: 'text.secondary' }}>{S.templates.slice(0, 4).map((k) => tplOf(k)?.name).join(t('、'))} …</Typography>
                  {existing && S.templates.some(has) && (
                    <Typography variant="caption" sx={{ color: 'primary.main' }}>{t('已有 {done} / {n} 个', { done: S.templates.filter(has).length, n: S.templates.length })}</Typography>
                  )}
                </CardActionArea>
              </Card>
            ))}
            {!onClose && <Card sx={{ transition: 'box-shadow 150ms', '&:hover': { boxShadow: 1, borderColor: 'primary.main' } }}>
              <CardActionArea onClick={() => setBlank(true)} sx={{ p: 2.25, display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 1.25, height: '100%' }}>
                <Box sx={{ width: 40, height: 40, borderRadius: 2, bgcolor: 'surface.containerHigh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}><NamedIcon name="sliders" size={20} /></Box>
                <Typography variant="h5">{t('自定义')}</Typography>
                <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('从空白开始，自己建抽屉和规则')}</Typography>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('不使用模板')}</Typography>
              </CardActionArea>
            </Card>}
          </Box>
        </>
      ) : (
        <>
          <Box>
            <Button startIcon={<ArrowLeft size={16} strokeWidth={SW} />} onClick={() => setScene(null)} sx={{ ml: -1.5, mb: 0.5, color: 'text.secondary' }}>{t('重新选择场景')}</Button>
            <Typography variant="h6">{t('选择要创建的抽屉 · {name}', { name: sc.name })}</Typography>
            <Typography sx={{ color: 'text.secondary', mt: 0.5 }}>{t('每个模板都带好了收纳规则和要点，之后可以改名、换图标和颜色。已有的文档会按规则放进去。')}</Typography>
          </Box>
          <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(220px,1fr))', gap: 1.5 }}>
            {sc.templates.map((k) => {
              const tp = tplOf(k);
              if (!tp) return null;
              const on = pick.includes(k);
              const had = has(k);
              return (
                <Card key={k} sx={{ borderColor: on ? 'primary.main' : 'divider', bgcolor: on ? 'surface.secondaryContainer' : 'background.paper', opacity: had ? 0.6 : 1 }}>
                  <CardActionArea role="checkbox" aria-checked={on} disabled={had} onClick={() => setPick(on ? pick.filter((x) => x !== k) : [...pick, k])} sx={{ p: 2 }}>
                    <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center' }}>
                      <DrawerIcon drawer={tp} size={32} /><Typography variant="h5" sx={{ flex: 1 }}>{tp.name}</Typography>
                      {had ? <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('已有')}</Typography> : <Checkbox checked={on} tabIndex={-1} sx={{ p: 0 }} />}
                    </Stack>
                    <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary', mt: 1 }}>{tp.description}</Typography>
                    {tp.facts.length > 0 && <Typography variant="caption" sx={{ display: 'block', color: 'text.secondary' }}>{t('要点：{list}', { list: tp.facts.join(t('、')) })}</Typography>}
                  </CardActionArea>
                </Card>
              );
            })}
          </Box>
          <Box>{sc.templates.every(has)
            ? <Typography sx={{ color: 'text.secondary' }}>{t('这个场景的抽屉都已经建好了。')}</Typography>
            : <Button variant="contained" disabled={!pick.length || create.isPending} onClick={go}>{t('创建 {n} 个抽屉', { n: pick.length })}</Button>}</Box>
        </>
      )}
      <DrawerDialog open={blank} onClose={() => setBlank(false)} onCreated={() => app.toast(t('已新建抽屉。可以在抽屉页编写收纳规则'))} />
    </Stack>
  );
}

type Row = { kind: 'drawer'; a: Alert } | { kind: 'health'; a: HealthAlert };

const NO_DRAWERS: DrawerList = { items: [], unfiled: 0 };

function Home() {
  const app = useApp();
  const nav = useNavigate();
  const role = useRole();
  const me = useMe().data;
  const ix = useTaxonomyIndex();
  // 切换空间的一瞬间新空间的抽屉还没取到（外层已等过一次），先当作空的，不读 undefined
  const drawers = useDrawers().data ?? NO_DRAWERS;
  const drawerAlerts = useAlerts().data ?? [];
  // 健康提醒和待核对的检查结果（v1.8.3），只在用了“体检与化验”的空间
  const health = (useMetrics(eeBuild).data?.length ?? 0) > 0;
  const healthAlerts = useHealthAlerts(health).data ?? [];
  const pendingLabs = (useHealthOverview(health).data?.persons ?? []).reduce((n, p) => n + p.pending.length, 0);
  const alerts: Row[] = [...drawerAlerts.map((a) => ({ kind: 'drawer' as const, a })), ...healthAlerts.map((a) => ({ kind: 'health' as const, a }))];
  const inbox = useInboxDocuments().data?.total ?? 0;
  const recentQ = useRecentDocuments(8);
  const recent = recentQ.data?.items ?? [];
  const brand = useBranding();
  const noDrawers = drawers.items.length === 0;
  // 还没有任何文档（取到最近添加之后才判断，免得闪一下）
  const noDocs = recentQ.isSuccess && recent.length === 0 && inbox === 0;
  const setAlert = useSetAlert();
  const [newOpen, setNewOpen] = useState(false);
  const [scenesOpen, setScenesOpen] = useState(false);
  const year = new Date().getFullYear();

  const dismiss = (a: Alert) => setAlert.mutate({ id: a.id, dismissed: true }, {
    onSuccess: () => app.toast(t('已忽略这条提醒，下个周期会再次出现'), () => { setAlert.mutate({ id: a.id, dismissed: false }, { onError: (e) => app.toast(t('没能撤销：{msg}', { msg: e.message })) }); app.set({ toast: null }); }),
    onError: (e) => app.toast(e.message),
  });
  const setHealthAlert = useSetHealthAlert();
  const dismissHealth = (a: HealthAlert) => setHealthAlert.mutate({ id: a.id, dismissed: true }, {
    onSuccess: () => app.toast(t('已忽略这条提醒'), () => { setHealthAlert.mutate({ id: a.id, dismissed: false }); app.set({ toast: null }); }),
    onError: (e) => app.toast(e.message),
  });
  const openDoc = (id: number) => {
    app.set({ docNav: { ids: recent.map((d) => d.id), from: '/', label: t('首页') } });
    nav(`/docs/${id}`);
  };

  return (
    <Box sx={{ px: 3, pt: 1, pb: 8 }}>
      <Typography variant="h2">{t('{greeting}，{name}', { greeting: greeting(), name: me?.user.displayName || me?.user.username || '' })}</Typography>
      <Typography sx={{ color: 'text.secondary', mt: 0.5 }}>
        {noDocs ? t('欢迎使用{name}。上传第一份文档，或者先建好抽屉。', { name: brand.name })
          : inbox || alerts.length ? t('你有 {docs}，{alerts}。', { docs: t('{n} 份文档待整理', { n: inbox }), alerts: t('{n} 条提醒', { n: alerts.length }) })
          : noDrawers ? t('先建好抽屉，文档会按规则自动放进去。') : t('一切都整理好了。')}
      </Typography>
      {/* 宽屏（≥1536px）分两栏：左边是需要处理的事和抽屉，右边是最近添加和年度回顾 */}
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0,1fr)', xl: 'minmax(0,1fr) 400px' }, gap: 4, mt: 3, alignItems: 'start' }}>
        <Stack spacing={4} sx={{ minWidth: 0 }}>
          {role.canWrite && <StorageWarning onOpen={() => nav('/settings/storage')} />}
          {noDocs && role.canWrite && (
            <Stack spacing={1} role="button" tabIndex={0} aria-label={t('上传文档')} onClick={() => app.set({ uploadOpen: true })} onKeyDown={(e) => e.key === 'Enter' && app.set({ uploadOpen: true })}
              sx={{ alignItems: 'center', justifyContent: 'center', height: 160, border: '1.5px dashed', borderColor: 'divider', borderRadius: 3, color: 'text.secondary', cursor: 'pointer', '&:hover': { borderColor: 'primary.main', bgcolor: 'action.hover' } }}>
              <Upload size={20} strokeWidth={SW} /><Typography sx={{ color: 'text.primary' }}>{t('拖入文件或点击上传，支持 PDF、图片和文本文件')}</Typography>
            </Stack>
          )}
          {inbox > 0 && (
            <Paper elevation={0} sx={{ display: 'flex', alignItems: 'center', gap: 1.5, p: '12px 16px', borderRadius: 4, bgcolor: 'surface.secondaryContainer', color: 'surface.onSecondaryContainer' }}>
              <Inbox size={20} strokeWidth={SW} /><Typography variant="h6" sx={{ flex: 1 }}>{t('{n} 份文档待整理', { n: inbox })}</Typography>
              <Button variant="contained" endIcon={<ArrowRight size={16} strokeWidth={SW} />} onClick={() => nav('/inbox')}>{t('开始整理')}</Button>
            </Paper>
          )}
          {pendingLabs > 0 && (
            <Paper elevation={0} sx={{ display: 'flex', alignItems: 'center', gap: 1.5, p: '12px 16px', borderRadius: 4, bgcolor: 'surface.secondaryContainer', color: 'surface.onSecondaryContainer' }}>
              <HeartPulse size={20} strokeWidth={SW} /><Typography variant="h6" sx={{ flex: 1 }}>{t('{n} 张单子的检查结果等你核对', { n: pendingLabs })}</Typography>
              <Button variant="contained" endIcon={<ArrowRight size={16} strokeWidth={SW} />} onClick={() => nav('/health')}>{t('去核对')}</Button>
            </Paper>
          )}
          {alerts.length > 0 && (
            <Box component="section" aria-label={t('提醒')}>
              <Typography variant="h5" sx={{ mb: 1.25 }}>{t('提醒')}</Typography>
              <Card>
                {alerts.slice(0, 5).map((row, i) => {
                  if (row.kind === 'health') {
                    const a = row.a;
                    return (
                      <Box key={`h${a.id}`} sx={{ display: 'flex', alignItems: 'center', gap: 1.5, minHeight: 56, pl: 2, pr: 1, borderTop: i ? 1 : 0, borderColor: 'divider', flexWrap: { xs: 'wrap', sm: 'nowrap' } }}>
                        <Box sx={{ color: a.kind === 'recheck' ? 'primary.main' : 'warning.main', display: 'flex' }}><HeartPulse size={18} strokeWidth={SW} /></Box>
                        <Box sx={{ flex: 1, py: 1, minWidth: 160 }}><Typography>{a.text}</Typography></Box>
                        <Box sx={{ display: { xs: 'none', sm: 'flex' }, fontSize: 13, color: 'text.secondary' }}>{t('健康')} · {ALERT_KIND_LABEL[a.kind]}</Box>
                        <Button size="small" onClick={() => nav(`/docs/${a.documentId}`)}>{t('查看')}</Button>
                        {role.canWrite && <Button size="small" sx={{ color: 'text.secondary' }} onClick={() => dismissHealth(a)}>{t('忽略')}</Button>}
                      </Box>
                    );
                  }
                  const a = row.a;
                  const D = ix.drawers.get(a.drawerId);
                  return (
                    <Box key={a.id} sx={{ display: 'flex', alignItems: 'center', gap: 1.5, minHeight: 56, pl: 2, pr: 1, borderTop: i ? 1 : 0, borderColor: 'divider', flexWrap: { xs: 'wrap', sm: 'nowrap' } }}>
                      {a.warn ? <Box sx={{ color: 'warning.main', display: 'flex' }}><TriangleAlert size={18} strokeWidth={SW} /></Box>
                        : <Box sx={{ width: 18, display: 'flex', justifyContent: 'center' }}><Box sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: 'text.secondary' }} /></Box>}
                      <Box sx={{ flex: 1, py: 1, minWidth: 160 }}>
                        <Typography>{a.text}</Typography>
                        {a.note && <Typography variant="body2" sx={{ color: 'text.secondary' }}>{a.note}<AiMark label={t('抽屉管家写的说明（AI 生成）')} /></Typography>}
                      </Box>
                      {D && <Box sx={{ display: { xs: 'none', sm: 'flex' }, alignItems: 'center', gap: 0.75, fontSize: 13, color: 'text.secondary' }}><DrawerIcon drawer={D} size={20} />{D.name}</Box>}
                      <Button size="small" onClick={() => (a.documentId ? nav(`/docs/${a.documentId}`) : nav(`/drawers/${a.drawerId}`))}>{t('查看')}</Button>
                      {role.canWrite && <Button size="small" sx={{ color: 'text.secondary' }} onClick={() => dismiss(a)}>{t('忽略')}</Button>}
                    </Box>
                  );
                })}
                {alerts.length > 5 && <Typography variant="body2" sx={{ color: 'text.secondary', p: 1.5, pl: 2 }}>{t('还有 {n} 条', { n: alerts.length - 5 })}</Typography>}
              </Card>
            </Box>
          )}
          <Box component="section" aria-label={t('我的抽屉')}>
            <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', mb: 1.25 }}>
              <Typography variant="h5">{t('我的抽屉')}</Typography>
              {role.canWrite && !noDrawers && (
                <Stack direction="row" spacing={1}>
                  {!scenesOpen && <Button size="small" onClick={() => setScenesOpen(true)}>{t('按场景添加')}</Button>}
                  <Button variant="tonal" size="small" onClick={() => setNewOpen(true)}>{t('新建抽屉')}</Button>
                </Stack>
              )}
            </Stack>
            {noDrawers && <DrawerSetup />}
            {!noDrawers && scenesOpen && role.canWrite && (
              <Card sx={{ p: 2.5, mb: 2, bgcolor: 'surface.containerLow' }}>
                <DrawerSetup existing={drawers.items} onClose={() => setScenesOpen(false)} />
              </Card>
            )}
            <Box sx={{ display: noDrawers ? 'none' : 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(240px,1fr))', gap: 2 }}>
              {drawers.items.map((D) => {
                const p = D.primary ? factView(D.primary) : null;
                return (
                  <Card key={D.id} sx={{ transition: 'box-shadow 150ms ease-out', '&:hover': { boxShadow: 1 } }}>
                    <CardActionArea onClick={() => nav(`/drawers/${D.id}`)} sx={{ p: 2, position: 'relative', height: '100%' }}>
                      {D.alertCount > 0 && <Box aria-label={t('有提醒')} sx={{ position: 'absolute', right: 16, top: 16, width: 8, height: 8, borderRadius: '50%', bgcolor: 'warning.main' }} />}
                      <DrawerIcon drawer={D} size={36} shared={!!D.sharedCount} />
                      <Stack direction="row" sx={{ alignItems: 'baseline', justifyContent: 'space-between', mt: 1.5 }}>
                        <Typography variant="h5">{D.name}</Typography>
                        <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('{n} 份', { n: D.documentCount })}</Typography>
                      </Stack>
                      <Box sx={{ mt: 1.5, pt: 1.5, borderTop: 1, borderColor: 'divider' }}>
                        <Typography variant="caption" sx={{ color: 'text.secondary' }}>{p?.label ?? t('文档数')}</Typography>
                        <Typography sx={{ fontSize: 18, lineHeight: '26px', fontWeight: 500, fontVariantNumeric: 'tabular-nums', color: p?.warn ? 'warning.main' : 'text.primary' }} noWrap>
                          {p ? p.value ?? (p.links?.length ? t('{n} 份', { n: p.links.length }) : '—') : t('{n} 份', { n: D.documentCount })}
                        </Typography>
                        <Typography variant="caption" noWrap sx={{ color: 'text.secondary', display: 'block', minHeight: 16 }}>{p?.note ?? (p ? '' : t('添加要点后，这里会显示到期、汇总等关键信息'))}</Typography>
                      </Box>
                    </CardActionArea>
                  </Card>
                );
              })}
            </Box>
            {drawers.unfiled > 0 && (
              <Button size="small" sx={{ mt: 1.5, color: 'text.secondary' }} onClick={() => {
                app.setDocList('files', (l) => ({ ...l, f: { ...l.f, drawer: [0] }, sel: [] }));
                nav('/files');
              }}>{noDrawers ? t('查看全部 {n} 份文档', { n: drawers.unfiled }) : t('还有 {n} 份文档未入抽屉', { n: drawers.unfiled })}</Button>
            )}
          </Box>
        </Stack>
        <Stack spacing={4} sx={{ minWidth: 0, position: { xl: 'sticky' }, top: { xl: 80 } }}>
          {(noDrawers || noDocs) && role.canWrite && (
            <GettingStarted drawersDone={!noDrawers} docsDone={recentQ.isSuccess && !noDocs}
              onUpload={() => app.set({ uploadOpen: true })} onPhone={() => nav('/settings/pair')} />
          )}
          {recent.length > 0 && (
            <Box component="section" aria-label={t('最近添加')}>
              <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', mb: 0.5 }}>
                <Typography variant="h5">{t('最近添加')}</Typography>
                <Button size="small" endIcon={<ArrowRight size={16} strokeWidth={SW} />} onClick={() => nav('/files')}>{t('查看全部')}</Button>
              </Stack>
              {recent.map((d) => {
                const D = d.drawerId ? ix.drawers.get(d.drawerId) : undefined;
                const corr = d.correspondentId ? ix.correspondents.get(d.correspondentId)?.name : '';
                return (
                  <Box key={d.id} component={RouterLink} to={docHref(d.id)} onClick={docLinkClick(() => openDoc(d.id))}
                    sx={{ display: 'flex', alignItems: 'center', gap: 1.5, minHeight: 48, px: 1, borderRadius: 2, cursor: 'pointer', color: 'inherit', textDecoration: 'none', '&:hover': { bgcolor: 'action.hover' } }}>
                    <Box sx={{ color: 'text.secondary', display: 'flex' }}><FileText size={18} strokeWidth={SW} /></Box>
                    <Typography noWrap sx={{ flex: 1 }}>{d.title}</Typography>
                    <Typography noWrap variant="body2" sx={{ color: 'text.secondary' }}>{[corr, d.createdDate.startsWith(String(year)) ? d.createdDate.slice(5) : d.createdDate].filter(Boolean).join(' · ')}</Typography>
                    <Box sx={{ width: { xs: 24, sm: 100, xl: 'auto' }, display: 'flex', alignItems: 'center', gap: 0.75, fontSize: 13 }}>
                      {D ? <><DrawerIcon drawer={D} size={20} /><Box component="span" sx={{ display: { xs: 'none', sm: 'inline', xl: 'none' } }}>{D.name}</Box></> : <DrawerIcon size={20} />}
                    </Box>
                  </Box>
                );
              })}
            </Box>
          )}
          <Card sx={{ display: 'flex', alignItems: 'center', gap: 1.5, p: '10px 12px 10px 16px' }}>
            <Box sx={{ color: 'text.secondary', display: 'flex' }}><CalendarDays size={20} strokeWidth={SW} /></Box>
            <Typography sx={{ flex: 1 }}>{t('{year} 年度回顾（截至 {month} 月 {d} 日）', { year, month: currentLang() === 'en' ? new Intl.DateTimeFormat('en', { month: 'long' }).format(new Date()) : new Date().getMonth() + 1, d: new Date().getDate() })}</Typography>
            <Button size="small" endIcon={<ArrowRight size={16} strokeWidth={SW} />} onClick={() => nav('/review')}>{t('查看')}</Button>
          </Card>
        </Stack>
      </Box>
      <DrawerDialog open={newOpen} onClose={() => setNewOpen(false)} onCreated={(d) => nav(`/drawers/${d.id}`)} />
    </Box>
  );
}

/** 新空间的上手清单（v1.3.4）：建抽屉、上传文档、连接手机，做完的打勾。 */
function GettingStarted({ drawersDone, docsDone, onUpload, onPhone }: { drawersDone: boolean; docsDone: boolean; onUpload: () => void; onPhone: () => void }) {
  const phoneDone = (useDevices().data?.length ?? 0) > 0;
  const steps: { title: string; detail: string; done: boolean; action?: [string, () => void] }[] = [
    { title: t('建好抽屉'), detail: t('左边选一个场景，或自定义。一类事放一个抽屉。'), done: drawersDone },
    { title: t('上传第一份文档'), detail: t('PDF、图片、Office 文档都可以，文字会自动识别。'), done: docsDone, action: [t('上传'), onUpload] },
    { title: t('用手机扫描单据'), detail: t('装好收好 App，扫码连接后拍照就能存进来。'), done: phoneDone, action: [t('连接手机'), onPhone] },
  ];
  const left = steps.filter((x) => !x.done).length;
  return (
    <Box component="section" aria-label={t('开始使用')}>
      <Stack direction="row" sx={{ alignItems: 'baseline', justifyContent: 'space-between', mb: 1.25 }}>
        <Typography variant="h5">{t('开始使用')}</Typography>
        <Typography variant="body2" sx={{ color: 'text.secondary' }}>{steps.length - left} / {steps.length}</Typography>
      </Stack>
      <Card>
        {steps.map((x, i) => (
          <Box key={x.title} sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, p: '12px 12px 12px 16px', borderTop: i ? 1 : 0, borderColor: 'divider' }}>
            <Box sx={{ color: x.done ? 'success.main' : 'text.disabled', display: 'flex', pt: '2px' }}>
              {x.done ? <CircleCheck size={18} strokeWidth={SW} aria-label={t('已完成')} /> : <Circle size={18} strokeWidth={SW} aria-label={t('未完成')} />}
            </Box>
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Typography sx={{ color: x.done ? 'text.secondary' : 'text.primary', textDecoration: x.done ? 'line-through' : 'none' }}>{x.title}</Typography>
              {!x.done && <Typography variant="body2" sx={{ color: 'text.secondary', mt: 0.25 }}>{x.detail}</Typography>}
            </Box>
            {!x.done && x.action && <Button size="small" onClick={x.action[1]}>{x.action[0]}</Button>}
          </Box>
        ))}
      </Card>
    </Box>
  );
}

/** 用量到配额的 90% 时提醒（v1.0 阶段 36）。 */
function StorageWarning({ onOpen }: { onOpen: () => void }) {
  const st = useStorage().data;
  const share = st ? quotaShare(st) : null;
  if (!st || share === null || share < 0.9) return null;
  return (
    <MuiAlert severity={share >= 1 ? 'error' : 'warning'} action={<Button color="inherit" size="small" onClick={onOpen}>{t('查看')}</Button>}>
      {t('{status}：已用 {used}，配额 {quota}。清空回收站或删除不需要的文档可以腾出空间。', { status: share >= 1 ? t('空间已满，暂时不能上传新文档') : t('空间快满了'), used: bytes(st.total), quota: bytes(st.quota) })}
    </MuiAlert>
  );
}

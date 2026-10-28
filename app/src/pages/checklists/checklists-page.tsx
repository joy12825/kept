// 办事清单（v1.6.2）：办签证、孩子入学、报销这类事要的一组材料。列出空间的清单，每张卡片显示齐了几项、还缺什么；
// 下面是可以直接开始的模板。铺满内容区，卡片按宽度自动排成几列。
import { useState } from 'react';
import { Link as RouterLink, useNavigate } from 'react-router-dom';
import { Box, Button, Card, CardActionArea, Chip, LinearProgress, Menu, MenuItem, Skeleton, Stack, Typography } from '@mui/material';
import { ClipboardCheck, Plus } from 'lucide-react';
import { useChecklistCheck, useChecklists, type Checklist, type ChecklistInput } from '../../api/checklists';
import { useRole } from '../../api/workspaces';
import { EmptyState, ErrorState } from '../../components/common';
import { SW } from '../../components/icons';
import { CHECKLIST_TEMPLATES } from '../../utils/checklist-templates';
import { ChecklistDialog } from './checklist-dialog';
import { t } from '../../i18n';

const GRID = { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 2 } as const;

/** 一份清单：名称、进度、还缺的材料、过期提醒。 */
function ChecklistCard({ c }: { c: Checklist }) {
  const check = useChecklistCheck(c.id);
  const items = check.data?.items ?? [];
  const ok = items.filter((it) => it.status === 'ok').length;
  const missing = items.filter((it) => it.status !== 'ok').map((it) => it.label);
  const expired = items.reduce((n, it) => n + it.expiredIds.length, 0);
  const done = items.length > 0 && !missing.length;
  return (
    <Card variant="outlined" sx={{ borderColor: done ? 'success.main' : undefined }}>
      <CardActionArea component={RouterLink} to={`/checklists/${c.id}`} sx={{ p: 2, height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'stretch', justifyContent: 'flex-start', gap: 1 }}>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'baseline' }}>
          <Typography variant="h6" noWrap sx={{ flex: 1, minWidth: 0 }}>{c.name}</Typography>
          {check.data && (
            <Typography variant="body2" sx={{ flex: 'none', fontWeight: 500, color: done ? 'success.main' : 'text.secondary', fontVariantNumeric: 'tabular-nums' }}>
              {done ? t('都齐了') : t('齐了 {ok}/{total}', { ok, total: items.length })}
            </Typography>
          )}
        </Stack>
        {check.isPending ? <Skeleton height={6} /> : (
          <LinearProgress variant="determinate" value={items.length ? (ok / items.length) * 100 : 0} color={done ? 'success' : 'primary'}
            aria-label={t('{name}：齐了 {ok} 项，共 {total} 项', { name: c.name, ok, total: items.length })} sx={{ height: 6, borderRadius: 3 }} />
        )}
        {c.note && <Typography variant="body2" sx={{ color: 'text.secondary', display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>{c.note}</Typography>}
        {missing.length > 0 && (
          <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
            <Box component="span" sx={{ color: 'text.secondary' }}>{t('还缺：')}</Box>{missing.slice(0, 4).join(t('、'))}{missing.length > 4 && t(' 等 {n} 项', { n: missing.length })}
          </Typography>
        )}
        {expired > 0 && <Chip size="small" color="warning" variant="outlined" label={t('{n} 份已过期', { n: expired })} sx={{ alignSelf: 'flex-start' }} />}
      </CardActionArea>
    </Card>
  );
}

export default function ChecklistsPage() {
  const list = useChecklists();
  const role = useRole();
  const nav = useNavigate();
  const [menu, setMenu] = useState<HTMLElement | null>(null);
  const [creating, setCreating] = useState<{ template?: ChecklistInput } | null>(null);
  const items = list.data ?? [];
  const start = (template?: ChecklistInput) => { setMenu(null); setCreating({ template }); };

  return (
    <Box sx={{ px: 3, pt: 1, pb: 8 }}>
      <Stack direction="row" spacing={2} sx={{ alignItems: 'center', mb: 2.5 }}>
        <Typography sx={{ color: 'text.secondary', flex: 1 }}>{t('办一件事要的一组材料。打开清单就能看到哪些已经有了、缺哪些，再一起分享或下载。')}</Typography>
        {role.canWrite && <Button variant="contained" sx={{ flex: 'none' }} startIcon={<Plus size={16} strokeWidth={SW} />} onClick={(e) => setMenu(e.currentTarget)}>{t('新建清单')}</Button>}
        <Menu anchorEl={menu} open={!!menu} onClose={() => setMenu(null)}>
          <MenuItem onClick={() => start()}>{t('空白清单')}</MenuItem>
          {CHECKLIST_TEMPLATES.map((tpl) => <MenuItem key={tpl.name} onClick={() => start(tpl)}>{t('从模板：{name}', { name: tpl.name })}</MenuItem>)}
        </Menu>
      </Stack>
      {list.isPending && <Box sx={GRID}>{[0, 1, 2].map((i) => <Skeleton key={i} variant="rounded" height={132} />)}</Box>}
      {list.isError && <ErrorState onRetry={() => void list.refetch()} />}
      {list.isSuccess && !items.length && (
        <EmptyState icon={<ClipboardCheck size={40} strokeWidth={SW} />} title={t('还没有清单')} text={t('例如办签证要护照、身份证、近 6 个月的银行流水。可以从下面的模板开始。')} />
      )}
      {items.length > 0 && <Box sx={GRID}>{items.map((c) => <ChecklistCard key={c.id} c={c} />)}</Box>}

      {list.isSuccess && role.canWrite && (
        <Box component="section" aria-labelledby="templates-title" sx={{ mt: 4 }}>
          <Typography id="templates-title" variant="h6" sx={{ mb: 0.5 }}>{t('从模板开始')}</Typography>
          <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>{t('常见的几件事要哪些材料。新建后每一项都可以改。')}</Typography>
          <Box sx={GRID}>
            {CHECKLIST_TEMPLATES.map((tpl) => (
              <Card key={tpl.name} variant="outlined" sx={{ borderStyle: 'dashed', bgcolor: 'transparent' }}>
                <CardActionArea onClick={() => start(tpl)} aria-label={t('用模板新建：{name}', { name: tpl.name })}
                  sx={{ p: 2, height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'stretch', justifyContent: 'flex-start', gap: 1 }}>
                  <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                    <Box sx={{ color: 'primary.main', display: 'flex' }}><Plus size={18} strokeWidth={SW} /></Box>
                    <Typography variant="h6" sx={{ flex: 1 }}>{tpl.name}</Typography>
                    <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('{n} 项', { n: tpl.items.length })}</Typography>
                  </Stack>
                  <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                    {tpl.items.map((it) => <Chip key={it.label} size="small" variant="outlined" label={it.label} />)}
                  </Box>
                </CardActionArea>
              </Card>
            ))}
          </Box>
        </Box>
      )}
      {creating && <ChecklistDialog open template={creating.template} onClose={() => setCreating(null)} onSaved={(c) => nav(`/checklists/${c.id}`)} />}
    </Box>
  );
}

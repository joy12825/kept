// 编辑收纳规则（UI 设计规范 5.3）：右侧滑出 480px。条件按“当 / 且 / 或”平铺，字段为类型、往来方、标签、
// 单选的自定义字段和关键词；边改边预览将收纳多少份、从哪里移入、移到哪里，手动放入的不受影响。
import { useEffect, useMemo, useState } from 'react';
import { Alert, Box, Button, Drawer as MuiDrawer, IconButton, Paper, Stack, Typography } from '@mui/material';
import { X } from 'lucide-react';
import { blankRow, RuleRowsEditor } from './rule-rows';
import { usePreviewDrawers, useUpdateDrawer, type Drawer } from '../api/drawers';
import type { Cond } from '../api/filters';
import { useApp } from '../store/app-store';
import { ruleToRows, rowsToRule, type RuleRow } from '../utils/drawer-rules';
import { DrawerIcon, SW } from './icons';
import { useTaxonomyIndex } from './pickers';
import { t } from '../i18n';

export function RulePanel({ drawer, open, onClose }: { drawer: Drawer; open: boolean; onClose: () => void }) {
  const app = useApp();
  const ix = useTaxonomyIndex();
  const initial = useMemo(() => ruleToRows(drawer.rule as Cond | undefined), [drawer.rule]);
  const [rows, setRows] = useState<RuleRow[]>(initial ?? []);
  const [custom, setCustom] = useState(initial === null); // 编辑器表示不了的规则
  useEffect(() => { if (open) { setRows(initial ?? []); setCustom(initial === null); } }, [open, initial]);
  const rule = custom ? (drawer.rule as Cond) : rowsToRule(rows);
  const preview = usePreviewDrawers();
  const update = useUpdateDrawer();
  const ruleKey = JSON.stringify(rule);
  useEffect(() => {
    if (!open) return;
    const t = window.setTimeout(() => preview.mutate(rule ? { drawerId: drawer.id, rule } : { drawerId: drawer.id, clearRule: true }), 400);
    return () => window.clearTimeout(t);
  }, [open, ruleKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const save = () => {
    update.mutate({ id: drawer.id, patch: rule ? { rule } : { clearRule: true } }, {
      onSuccess: () => { app.toast(t('已保存收纳规则，正在后台重新整理文档')); onClose(); },
      onError: (e) => app.toast(e.message),
    });
  };
  const p = preview.data;
  const drawerName = (id?: number) => (id ? ix.drawers.get(id)?.name ?? t('其他抽屉') : t('未入抽屉'));

  return (
    <MuiDrawer anchor="right" open={open} onClose={onClose} slotProps={{ paper: { sx: { width: 480, maxWidth: '100vw', bgcolor: 'background.paper' } } }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.25, height: 64, px: 2.5, borderBottom: 1, borderColor: 'divider', flex: 'none' }}>
        <DrawerIcon drawer={drawer} size={24} />
        <Typography variant="h5" sx={{ flex: 1 }}>{t('编辑收纳规则 · {name}', { name: drawer.name })}</Typography>
        <IconButton aria-label={t('关闭')} onClick={onClose}><X size={20} strokeWidth={SW} /></IconButton>
      </Box>
      <Stack spacing={1.5} sx={{ flex: 1, overflow: 'auto', p: 2.5 }}>
        {custom ? (
          <Alert severity="info" action={<Button size="small" onClick={() => { setCustom(false); setRows([blankRow('and')]); }}>{t('重新编写')}</Button>}>
            {t('这条规则是通过接口写的，包含编辑器表示不了的条件，可以保留，也可以重新编写。')}
          </Alert>
        ) : (
          <>
            <RuleRowsEditor rows={rows} onChange={setRows} empty={t('还没有条件。添加条件后，符合条件的文档会自动放进这个抽屉。')}
              hint={t('“且”比“或”优先：“A 且 B 或 C”表示满足 A 和 B，或者满足 C。关键词在标题和正文里查找，写多个时出现任意一个即可。多个抽屉的规则都满足时，放进排在前面的抽屉。')} />
          </>
        )}
        <Paper elevation={0} role="status" sx={{ mt: 1, p: 2, bgcolor: 'surface.containerLow', borderRadius: 3 }}>
          {preview.error ? <Typography variant="body2" sx={{ color: 'error.main' }}>{preview.error.message}</Typography> : p ? (
            <>
              <Typography variant="h6">{t('将收纳 {n} 份文档', { n: p.total })}</Typography>
              <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                {[t('将从其他抽屉移入 {n} 份', { n: p.in }), t('原来未入抽屉的放进来 {n} 份', { n: p.new }), t('将移到其他抽屉 {n} 份', { n: p.out }), t('将变为未入抽屉 {n} 份', { n: p.unfiled })].join(' · ')}
              </Typography>
              <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('手动放入的 {n} 份不受影响', { n: p.manual })}</Typography>
              {p.samples.length > 0 && (
                <Box component="ul" sx={{ m: 0, mt: 1, pl: 2.5, fontSize: 13 }}>
                  {p.samples.map((m) => <li key={m.documentId}>{t('{title}：{from} → {to}', { title: m.title, from: drawerName(m.from), to: drawerName(m.to) })}</li>)}
                </Box>
              )}
            </>
          ) : <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('正在计算…')}</Typography>}
        </Paper>
      </Stack>
      <Stack direction="row" spacing={1} sx={{ justifyContent: 'flex-end', p: 2, borderTop: 1, borderColor: 'divider' }}>
        <Button onClick={onClose}>{t('取消')}</Button>
        <Button variant="contained" disabled={update.isPending || !!preview.error} onClick={save}>{t('保存规则')}</Button>
      </Stack>
    </MuiDrawer>
  );
}

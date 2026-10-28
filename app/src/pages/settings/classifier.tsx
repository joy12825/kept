// 设置 → 自动分类（设计文档 5.6）：模型状态、参与学习的对象和样本数、重新训练、对已有文档重新分类。
import { useState } from 'react';
import { Box, Button, Card, Chip, FormControlLabel, LinearProgress, Radio, RadioGroup, Skeleton, Stack, Table, TableBody, TableCell, TableHead, TableRow, Tooltip, Typography } from '@mui/material';
import { RefreshCw } from 'lucide-react';
import { SKIP_REASONS, useClassifier, useTrainClassifier, type ClassifierStatus, type ClassifyMode } from '../../api/classifier';
import { useBulk } from '../../api/documents';
import { useRole } from '../../api/workspaces';
import { ErrorState } from '../../components/common';
import { SW } from '../../components/icons';
import { useTaxonomyIndex } from '../../components/pickers';
import { useApp } from '../../store/app-store';
import { dateTime } from '../detail/time';
import { t } from '../../i18n';

const KIND: Record<ClassifierStatus['items'][number]['kind'], string> = { type: t('类型'), correspondent: t('往来方'), person: t('家人'), tag: t('标签') };
const MODES: [ClassifyMode, string, string][] = [
  ['fill', t('只补空'), t('只填写还空着的类型、往来方和家人，只增加标签。')],
  ['overwrite', t('覆盖'), t('用新的结果替换以前自动分类的结果。')],
];

/** 已学会；样本不足；或者几乎每份文档都有，没有可以对比的。 */
function ItemState({ samples, learned, total }: { samples: number; learned: boolean; total: number }) {
  if (learned) return <Chip size="small" color="success" variant="outlined" label={t('已学会')} />;
  if (samples >= 2 && total - samples < 2) {
    return <Tooltip title={t('几乎每份已整理的文档都有这一项，学不出什么样的文档没有它')}><Chip size="small" variant="outlined" label={t('无法区分')} /></Tooltip>;
  }
  return <Chip size="small" variant="outlined" label={t('样本不足')} />;
}

export function ClassifierPanel() {
  const st = useClassifier();
  const role = useRole();
  const ix = useTaxonomyIndex();
  const train = useTrainClassifier();
  const bulk = useBulk();
  const app = useApp();
  const [mode, setMode] = useState<ClassifyMode>('fill');

  if (st.error) return <ErrorState onRetry={() => void st.refetch()} />;
  const s = st.data;
  const name = (kind: string, id: number) =>
    (kind === 'type' ? ix.types.get(id)?.name : kind === 'correspondent' ? ix.correspondents.get(id)?.name : kind === 'person' ? ix.persons.get(id)?.name : ix.tagTree.find((tt) => tt.tag.id === id)?.path) ?? t('已删除');
  const applying = s?.applying && (s.applying.status === 'queued' || s.applying.status === 'running') ? s.applying : undefined;

  const reapply = () => app.confirm({
    title: t('对全部文档重新分类？'),
    body: t('{help}手动改过的类型、往来方、家人和标签不会被改动。会在后台进行，可以离开这个页面。', { help: MODES.find(([m]) => m === mode)![2] }),
    verb: t('开始'),
    safe: true,
    run: () => {
      app.confirm(null);
      bulk.mutate({ filter: {}, classify: { mode } }, {
        onSuccess: (r) => { app.toast(t('已开始重新分类，共 {n} 份文档。', { n: r.count })); void st.refetch(); },
        onError: (e) => app.toast(e.message),
      });
    },
  });

  return (
    <>
      <Typography variant="h4">{t('自动分类')}</Typography>
      <Typography sx={{ color: 'text.secondary', mt: 0.5, mb: 2.5, maxWidth: 720 }}>
        {t('新文档入库时，先按往来方、类型和标签的匹配规则归类；匹配方式为“自动学习”的，由根据已整理文档学到的模型判断。手动改过的分类不会被自动分类改动。')}
      </Typography>

      <Card sx={{ p: 2, mb: 3, maxWidth: 720 }}>
        {!s ? <Skeleton width="60%" /> : (
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} sx={{ alignItems: { sm: 'center' } }}>
            <Box sx={{ flex: 1 }}>
              <Typography variant="h6">{s.training ? t('正在训练…') : s.trainedAt && !s.skipped ? t('模型可用') : t('还没有可用的模型')}</Typography>
              <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                {s.skipped ? t('{reason}：至少需要 {n} 份已整理的文档，每个对象至少 2 份。', { reason: SKIP_REASONS[s.skipped], n: s.minDocuments }) : null}
                {s.trainedAt
                  ? (s.skipped
                    ? t('上次检查：{time}，用了 {n} 份已整理的文档。', { time: dateTime(s.trainedAt), n: s.documentCount })
                    : t('上次训练：{time}，用了 {n} 份已整理的文档。', { time: dateTime(s.trainedAt), n: s.documentCount }))
                  : t('每小时检查一次，已整理的文档有变化时自动重新训练。')}
              </Typography>
              {s.training && <LinearProgress sx={{ mt: 1 }} />}
            </Box>
            {role.canWrite && (
              <Button variant="outlined" startIcon={<RefreshCw size={16} strokeWidth={SW} />} disabled={s.training || train.isPending}
                onClick={() => train.mutate(undefined, { onSuccess: () => app.toast(t('已开始重新训练')), onError: (e) => app.toast(e.message) })}>
                {t('重新训练')}
              </Button>
            )}
          </Stack>
        )}
      </Card>

      <Typography variant="h5" sx={{ mb: 1 }}>{t('参与自动学习的对象')}</Typography>
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>{t('在目录里把匹配方式设为“自动学习”的往来方、类型和标签。样本是带有这一项的已整理文档。')}</Typography>
      <Card sx={{ mb: 3, maxWidth: 720, overflowX: 'auto' }}>
        {s && s.items.length === 0
          ? <Typography variant="body2" sx={{ p: 2, color: 'text.secondary' }}>{s.trainedAt ? t('没有设为“自动学习”的对象。') : t('训练之后在这里显示。')}</Typography>
          : (
            <Table size="small" aria-label={t('参与自动学习的对象')}>
              <TableHead><TableRow><TableCell>{t('种类')}</TableCell><TableCell>{t('名称')}</TableCell><TableCell align="right">{t('样本')}</TableCell><TableCell>{t('状态')}</TableCell></TableRow></TableHead>
              <TableBody>
                {(s?.items ?? []).map((it) => (
                  <TableRow key={`${it.kind}:${it.id}`}>
                    <TableCell sx={{ color: 'text.secondary', whiteSpace: 'nowrap' }}>{KIND[it.kind]}</TableCell>
                    <TableCell>{name(it.kind, it.id)}</TableCell>
                    <TableCell align="right">{it.samples}</TableCell>
                    <TableCell><ItemState samples={it.samples} learned={it.learned} total={s?.documentCount ?? 0} /></TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
      </Card>

      <Typography variant="h5" sx={{ mb: 1 }}>{t('对已有文档重新分类')}</Typography>
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5, maxWidth: 720 }}>{t('改了匹配规则或重新训练之后，可以对已有的文档再执行一次自动分类。也可以在文档列表里选中一部分文档再“重新分类”。')}</Typography>
      <Card sx={{ p: 2, maxWidth: 720 }}>
        <RadioGroup value={mode} onChange={(e) => setMode(e.target.value as ClassifyMode)} aria-label={t('重新分类的方式')}>
          {MODES.map(([m, label, help]) => (
            <FormControlLabel key={m} value={m} disabled={!role.canWrite} control={<Radio />}
              label={<Box sx={{ py: 0.5 }}><Typography variant="body2">{label}</Typography><Typography variant="caption" sx={{ color: 'text.secondary' }}>{help}</Typography></Box>} />
          ))}
        </RadioGroup>
        {applying && (
          <Box sx={{ mt: 1.5 }}>
            <Typography variant="body2" sx={{ mb: 0.5 }}>{t('正在重新分类：{done} / {total}', { done: applying.done, total: applying.total })}</Typography>
            <LinearProgress variant={applying.total ? 'determinate' : 'indeterminate'} value={applying.total ? (applying.done / applying.total) * 100 : 0} />
          </Box>
        )}
        {s?.applying && !applying && (
          <Typography variant="body2" sx={{ mt: 1.5, color: s.applying.status === 'failed' ? 'error.main' : 'text.secondary' }}>
            {s.applying.status === 'failed' ? t('上次重新分类没有完成，请重试。') : t('上次重新分类：{total} 份文档，改动了 {changed} 份。', { total: s.applying.total, changed: s.applying.changed })}
          </Typography>
        )}
        {role.canWrite && (
          <Button variant="contained" sx={{ mt: 2 }} disabled={!!applying || bulk.isPending} onClick={reapply}>{t('对全部文档重新分类')}</Button>
        )}
      </Card>
    </>
  );
}

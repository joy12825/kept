// 可以清理（v1.6.5，参照 Mayan EDMS 的保留策略）：超过所属类型保留期限的文档。
// 已整理、不是大事、没有被抽屉要点固定、也没有被其他文档链接的才会列出；只提示，选中后由你移到回收站。
import { useNavigate } from 'react-router-dom';
import { Box, Button, Typography } from '@mui/material';
import { Recycle } from 'lucide-react';
import DocList from '../components/doc-list';
import { EmptyState, LoadingState } from '../components/common';
import { SW } from '../components/icons';
import { retentionLabel, useTaxonomy } from '../api/taxonomy';
import type { Cond } from '../api/filters';
import { t } from '../i18n';

export const EXPIRED: Cond = { field: 'retention', op: 'expired', value: true };
const BASE = [EXPIRED];

export default function CleanupPage() {
  const nav = useNavigate();
  const types = useTaxonomy('document-types');
  if (types.isPending) return <LoadingState />;
  const kept = (types.data ?? []).filter((ty) => ty.retentionMonths);
  if (!kept.length) {
    return (
      <EmptyState icon={<Recycle size={40} strokeWidth={SW} />} title={t('还没有设置保留期限')}
        text={t('水电账单、快递单这类文档通常不必永久保留。在“目录 → 类型”里给类型设保留期限，超过期限的文档会列在这里，由你确认后再移到回收站。')}
        action={t('去设置')} onAction={() => nav('/directory?tab=document-types')} />
    );
  }
  return (
    <Box>
      <Box sx={{ px: 3, pb: 0.5 }}>
        <Typography variant="body2" sx={{ color: 'text.secondary' }}>
          {t('这些文档超过了类型的保留期限（{list}）。大事、被抽屉固定或被其他文档链接的不会列出。选中后可以移到回收站，30 天内还能恢复。', {
            list: kept.map((ty) => `${ty.name} ${retentionLabel(ty.retentionMonths)}`).join(t('、')),
          })}
          <Button size="small" onClick={() => nav('/directory?tab=document-types')}>{t('修改保留期限')}</Button>
        </Typography>
      </Box>
      <DocList listKey="cleanup" base={BASE} placeholder={t('在可以清理的文档中搜索…')} backLabel={t('可以清理')}
        emptyTitle={t('没有需要清理的文档')} emptyText={t('超过保留期限的文档会出现在这里。')} />
    </Box>
  );
}

// 详情页的分类依据：每个字段的值从哪里来——匹配规则（及命中的词）、自动学习（及把握）、从正文识别、上传时指定、手动修改。
import { Box, Stack, Typography } from '@mui/material';
import { describeSource, sourceLines } from '../../api/classifier';
import type { DocumentDetail } from '../../api/documents';
import type { TaxonomyIndex } from '../../components/pickers';
import { t } from '../../i18n';

export function Sources({ d, ix }: { d: DocumentDetail; ix: TaxonomyIndex }) {
  const lines = sourceLines(d.fieldSources, d.tagIds, (id) => ix.tags.get(id)?.name);
  if (!lines.length) {
    return <Typography variant="body2" sx={{ color: 'text.secondary', fontSize: 12 }}>{t('没有自动分类的记录：分类都是手动填写的，或文档入库时还没有自动分类。')}</Typography>;
  }
  return (
    <Box component="dl" sx={{ m: 0 }}>
      {lines.map((l) => (
        <Stack key={l.field} direction="row" spacing={1.25} sx={{ fontSize: 12, lineHeight: '20px', py: 0.25 }}>
          <Box component="dt" sx={{ width: 88, flex: 'none', color: 'text.secondary', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{l.field}</Box>
          <Box component="dd" sx={{ m: 0, minWidth: 0 }}>{describeSource(l.source)}</Box>
        </Stack>
      ))}
    </Box>
  );
}

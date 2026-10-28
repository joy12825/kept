import { Link as RouterLink } from 'react-router-dom';
import { Box, Link, Typography } from '@mui/material';
import { CopyCheck } from 'lucide-react';
import DocList from '../components/doc-list';
import { useDuplicates } from '../api/duplicates';
import { SW } from '../components/icons';
import { t } from '../i18n';

export function FilesPage() {
  // 有疑似重复的文档时在列表上方给出入口（阶段 20）
  const dup = useDuplicates().data?.pairs.length ?? 0;
  return (
    <Box sx={{ px: 3, pt: 0.5 }}>
      {dup > 0 && (
        <Typography variant="body2" sx={{ display: 'flex', alignItems: 'center', gap: 0.75, color: 'text.secondary', mb: 1 }}>
          <CopyCheck size={16} strokeWidth={SW} />{t('发现 {n} 对内容几乎一样的文档。', { n: dup })}
          <Link component={RouterLink} to="/duplicates">{t('去看看')}</Link>
        </Typography>
      )}
      <DocList listKey="files" placeholder={t('搜索标题、正文、备注…')} showSave backLabel={t('全部文件')} />
    </Box>
  );
}

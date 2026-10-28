// 详情页的“关联到这份的文档”（v1.5.3）：别的文档的文档链接字段指向这份时列出来，比如合同下面看到引用它的发票。
// 只是反向查询，要改关联请到对方文档的字段里改。
import { Link as RouterLink } from 'react-router-dom';
import { Accordion, AccordionDetails, AccordionSummary, Box, Link, Typography } from '@mui/material';
import { ChevronDown } from 'lucide-react';
import type { DocumentDetail } from '../../api/documents';
import { SW } from '../../components/icons';
import { t } from '../../i18n';

export function LinkedFrom({ items }: { items: DocumentDetail['linkedFrom'] }) {
  if (!items?.length) return null;
  return (
    <Accordion disableGutters defaultExpanded>
      <AccordionSummary expandIcon={<ChevronDown size={18} strokeWidth={SW} />} sx={{ px: 0, minHeight: 44 }}>
        <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('关联到这份的文档 · {n}', { n: items.length })}</Typography>
      </AccordionSummary>
      <AccordionDetails sx={{ px: 0, pt: 0 }}>
        <Box component="ul" aria-label={t('关联到这份的文档')} sx={{ listStyle: 'none', p: 0, m: 0 }}>
          {items.map((d) => (
            <Box component="li" key={`${d.id}-${d.field}`} sx={{ py: 0.75, borderBottom: 1, borderColor: 'divider' }}>
              <Link component={RouterLink} to={`/docs/${d.id}`} underline="hover" variant="body2" sx={{ fontWeight: 500, overflowWrap: 'anywhere' }}>{d.title}</Link>
              <Typography variant="caption" sx={{ color: 'text.secondary', ml: 1 }}>{t('{date} · 通过“{field}”', { date: d.createdDate, field: d.field })}</Typography>
            </Box>
          ))}
        </Box>
      </AccordionDetails>
    </Accordion>
  );
}

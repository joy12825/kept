// 版本快到期或已到期（v1.12）：实例管理员在每个页面顶部看到一条，点开到版本与授权页。
import { useLocation, useNavigate } from 'react-router-dom';
import { Alert, Button } from '@mui/material';
import { useEdition } from '../api/edition';
import { useMe } from '../api/session';
import { t } from '../i18n';

export function EditionBanner() {
  const isAdmin = !!useMe().data?.user.isAdmin;
  const ed = useEdition().data;
  const nav = useNavigate();
  const here = useLocation().pathname === '/settings/edition';
  if (!isAdmin || !ed || here || ed.overridden) return null;
  const text = ed.trial && ed.warn ? t('收费版试用还剩 {n} 天，结束后回到免费版，数据都不受影响。', { n: ed.daysLeft })
    : ed.license && ed.expired ? t('许可证已到期，现在是免费版：数据照常能看能导出，收费功能不能再新建和运行。')
    : ed.license && ed.warn ? t('许可证还剩 {n} 天到期。', { n: ed.daysLeft })
    : null;
  if (!text) return null;
  return (
    <Alert severity="warning" sx={{ mx: 3, mb: 1.5 }} data-testid="edition-banner"
      action={<Button color="inherit" size="small" onClick={() => nav('/settings/edition')}>{t('版本与授权')}</Button>}>
      {text}
    </Alert>
  );
}

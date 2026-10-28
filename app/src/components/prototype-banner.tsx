import { Alert } from '@mui/material';
import { useLocation } from 'react-router-dom';
import { isPrototypeRoute } from '../features';
import { t } from '../i18n';

export function PrototypeBanner() {
  const { pathname } = useLocation();
  if (!isPrototypeRoute(pathname)) return null;
  return (
    <Alert severity="info" sx={{ borderRadius: 0, py: 0 }}>
      {t('此页面暂为示例数据，将在后续版本接入真实数据。')}
    </Alert>
  );
}

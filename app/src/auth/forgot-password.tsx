import { Button, Typography } from '@mui/material';
import { ArrowLeft } from 'lucide-react';
import { SW } from '../components/icons';
import { fontMono } from '../theme';
import { t } from '../i18n';

/** 忘记密码：网页端不支持邮件重置，只说明如何联系管理员或在服务器上执行命令。 */
export function ForgotPassword({ onBack }: { onBack: () => void }) {
  return (
    <>
      <Button startIcon={<ArrowLeft size={16} strokeWidth={SW} />} onClick={onBack} sx={{ ml: -1.5, mb: 2, color: 'text.secondary' }}>
        {t('返回登录')}
      </Button>
      <Typography variant="h2">{t('重置密码')}</Typography>
      <Typography
        sx={{
          color: "text.secondary",
          mt: 0.5,
          mb: 3
        }}>
        {t('网页端暂不支持通过邮件重置密码。请联系管理员在“设置 → 账号管理”中为你重置，或在服务器上执行：')}
      </Typography>
      <Typography
        component="pre"
        sx={{ fontFamily: fontMono, fontSize: 13, p: 2, borderRadius: 3, bgcolor: 'surface.containerLow', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}
      >
        {t('kept user set-password <用户名或邮箱>')}
      </Typography>
    </>
  );
}

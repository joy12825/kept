// 显示恢复码（v1.8.1 家人的钥匙，v1.11.0 存储加密）：只显示一次，必须勾选“已保存”才能关闭。
import { useState } from 'react';
import { Box, Button, Checkbox, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Stack, Typography } from '@mui/material';
import { useApp } from '../store/app-store';
import { t } from '../i18n';

/** 显示恢复码：必须勾选“已保存”才能关闭。body 换掉默认的说明（v1.11.0 存储加密也用它）。 */
export function RecoveryCodeDialog({ code, body, onClose }: { code: string; body?: string; onClose: () => void }) {
  const app = useApp();
  const [saved, setSaved] = useState(false);
  return (
    <Dialog open fullWidth maxWidth="xs" aria-labelledby="recovery-code-title">
      <DialogTitle id="recovery-code-title">{t('请保存恢复码')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 2 }}>
          {body ?? t('恢复码只显示这一次，收好不会保存它。本机的文件丢了、要从云端或远程备份恢复私密文档时，要用它（或负责人的登录密码）。请抄在纸上，或存进密码管理器。')}
        </Typography>
        <Box sx={{ fontFamily: 'monospace', fontSize: { xs: 15, sm: 17 }, letterSpacing: 0.5, whiteSpace: 'nowrap', overflowX: 'auto', textAlign: 'center', p: 2, borderRadius: 2, bgcolor: 'surface.containerHigh', userSelect: 'all' }}
          aria-label={t('恢复码')}>{code}</Box>
        <Stack direction="row" sx={{ justifyContent: 'center', mt: 1 }}>
          <Button size="small" onClick={() => { void navigator.clipboard?.writeText(code).then(() => app.toast(t('已复制'))); }}>{t('复制')}</Button>
        </Stack>
        <FormControlLabel sx={{ mt: 1 }} control={<Checkbox checked={saved} onChange={(e) => setSaved(e.target.checked)} />} label={t('我已经抄下或保存了恢复码')} />
      </DialogContent>
      <DialogActions>
        <Button variant="contained" disabled={!saved} onClick={onClose}>{t('完成')}</Button>
      </DialogActions>
    </Dialog>
  );
}

// 设置 → 通知（阶段 16）：邮件提醒（服务器每天早上汇总发送）与浏览器通知（只在这个浏览器）。
import { useEffect, useState } from 'react';
import { Alert, Box, Card, Stack, Switch, Typography } from '@mui/material';
import { useAlerts } from '../../api/drawers';
import { useMe } from '../../api/session';
import { useRole } from '../../api/workspaces';
import {
  browserNotificationsOn, browserNotificationsSupported, setBrowserNotifications, useNotificationPrefs, useSetNotificationPrefs,
} from '../../api/notify';
import { NOTIFY_CHANGED } from '../../components/alert-notifier';
import { useApp } from '../../store/app-store';
import { t } from '../../i18n';

function Row({ title, text, checked, disabled, onChange, label, first }: { title: string; text: string; checked: boolean; disabled?: boolean; onChange: (v: boolean) => void; label: string; first?: boolean }) {
  return (
    <Stack direction="row" sx={{ alignItems: 'center', p: 2, gap: 2, borderTop: first ? 0 : 1, borderColor: 'divider' }}>
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <Typography variant="h6">{title}</Typography>
        <Typography variant="caption" sx={{ color: 'text.secondary' }}>{text}</Typography>
      </Box>
      <Switch checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} slotProps={{ input: { 'aria-label': label } }} />
    </Stack>
  );
}

export function NotificationsPanel() {
  const app = useApp();
  const me = useMe().data;
  const role = useRole();
  const prefs = useNotificationPrefs();
  const setPrefs = useSetNotificationPrefs();
  const alerts = useAlerts().data;
  const ws = me?.workspace.id;
  const [browserOn, setBrowserOn] = useState(() => (ws !== undefined ? browserNotificationsOn(ws) : false));
  useEffect(() => { if (ws !== undefined) setBrowserOn(browserNotificationsOn(ws)); }, [ws]);
  const supported = browserNotificationsSupported();
  const p = prefs.data;

  const emailText = !p ? '' : !p.smtp ? t('管理员还没有配置发信（KEPT_SMTP_HOST），暂时不能发送邮件。')
    : !p.email ? t('先在“个人资料”中填写邮箱，才能收到邮件提醒。')
      : role.isOwner
        ? t('每天早上 8 点，把新出现的提醒汇总成一封邮件，发到 {email}。同一条提醒只发一次。空间存储用到 90% 和用满时，也会各发一封邮件给你。', { email: p.email })
        : t('每天早上 8 点，把新出现的提醒汇总成一封邮件，发到 {email}。同一条提醒只发一次。', { email: p.email });

  const toggleBrowser = async (on: boolean) => {
    if (ws === undefined) return;
    if (on && Notification.permission !== 'granted') {
      const r = await Notification.requestPermission();
      if (r !== 'granted') { app.toast(t('浏览器没有允许通知，可以在浏览器的网站设置中允许后再开启')); return; }
    }
    setBrowserNotifications(ws, on, (alerts ?? []).map((a) => a.id));
    setBrowserOn(on);
    window.dispatchEvent(new Event(NOTIFY_CHANGED));
    app.toast(on ? t('已开启浏览器通知，之后出现的新提醒会弹出通知') : t('已关闭浏览器通知'));
  };

  return (
    <>
      <Typography variant="h4">{t('通知')}</Typography>
      <Typography sx={{ color: 'text.secondary', mt: 0.5, mb: 2.5 }}>
        {t('抽屉里的日期快到了、该存的文档还没存时，收好会提醒你。这里的设置只对当前空间“{name}”有效。', { name: me?.workspace.name ?? '' })}
      </Typography>
      {prefs.error && <Alert severity="error" sx={{ mb: 2 }}>{prefs.error.message}</Alert>}
      <Card>
        <Row first title={t('邮件提醒')} label={t('邮件提醒')} text={emailText} checked={p?.emailAlerts ?? false}
          disabled={!p || setPrefs.isPending || (!p.emailAlerts && (!p.smtp || !p.email))}
          onChange={(v) => setPrefs.mutate({ emailAlerts: v }, {
            onSuccess: () => app.toast(v ? t('已开启邮件提醒') : t('已关闭邮件提醒')),
            onError: (e) => app.toast(e.message),
          })} />
        <Row title={t('浏览器通知')} label={t('浏览器通知')}
          text={supported ? t('这个浏览器开着收好时，出现新提醒就弹出系统通知；关闭页面后不会收到。只对这个浏览器有效。') : t('这个浏览器不支持系统通知。')}
          checked={browserOn} disabled={!supported || ws === undefined} onChange={(v) => void toggleBrowser(v)} />
      </Card>
    </>
  );
}

// 浏览器通知（阶段 16）：这个浏览器开着收好时，出现新的提醒就弹出系统通知。
// 关掉页面后不会收到：不使用 Web Push，避免依赖浏览器厂商的推送服务。
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAlerts } from '../api/drawers';
import { useMe } from '../api/session';
import { browserNotificationsOn, seenAlerts, setSeenAlerts } from '../api/notify';
import { t } from '../i18n';

/** 设置页改了开关后通知这里重新读取。 */
export const NOTIFY_CHANGED = 'kept-notify-changed';

export function AlertNotifier() {
  const ws = useMe().data?.workspace.id;
  const [, bump] = useState(0);
  useEffect(() => {
    const on = () => bump((n) => n + 1);
    window.addEventListener(NOTIFY_CHANGED, on);
    return () => window.removeEventListener(NOTIFY_CHANGED, on);
  }, []);
  if (ws === undefined || !browserNotificationsOn(ws)) return null;
  return <Watcher ws={ws} />;
}

function Watcher({ ws }: { ws: number }) {
  const alerts = useAlerts().data;
  const nav = useNavigate();
  useEffect(() => {
    if (!alerts) return;
    const seen = seenAlerts(ws);
    const fresh = alerts.filter((a) => !seen.has(a.id));
    // 一下子出现很多条时只弹前 3 条，其余合成一条
    fresh.slice(0, 3).forEach((a) => {
      const n = new Notification(t('收好提醒'), { body: a.text, tag: `kept-alert-${a.id}` });
      n.onclick = () => { window.focus(); nav(`/drawers/${a.drawerId}`); n.close(); };
    });
    if (fresh.length > 3) {
      const n = new Notification(t('收好提醒'), { body: t('还有 {n} 条新提醒', { n: fresh.length - 3 }), tag: 'kept-alert-more' });
      n.onclick = () => { window.focus(); nav('/'); n.close(); };
    }
    if (fresh.length) setSeenAlerts(ws, alerts.map((a) => a.id));
  }, [alerts, ws, nav]);
  return null;
}

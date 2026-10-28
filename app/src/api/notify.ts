// 发信与通知（阶段 16）：通过邮件发送文档、通知设置、测试邮件。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import { jobsKey } from './documents';
import type { components } from './schema';

export type NotificationPrefs = components['schemas']['NotificationPrefs'];
export type EmailDocumentsRequest = components['schemas']['EmailDocumentsRequest'];

export const notificationPrefsKey = ['notification-prefs'] as const;

/** 自己在当前空间的通知设置。 */
export function useNotificationPrefs() {
  return useQuery<NotificationPrefs, ApiError>({
    queryKey: notificationPrefsKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/me/notifications')),
  });
}

export function useSetNotificationPrefs() {
  const qc = useQueryClient();
  return useMutation<NotificationPrefs, ApiError, { emailAlerts: boolean }>({
    mutationFn: async (body) => unwrap(await api.PUT('/api/v1/me/notifications', { body })),
    onSuccess: (p) => qc.setQueryData(notificationPrefsKey, p),
  });
}

/** 把文档作为附件发出去；在后台发送，结果在任务记录中。 */
export function useEmailDocuments() {
  const qc = useQueryClient();
  return useMutation<{ jobId: number }, ApiError, EmailDocumentsRequest>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/documents/email', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: jobsKey }),
  });
}

/** 实例管理员：立即发一封测试邮件。 */
export function useTestSmtp() {
  return useMutation<void, ApiError, string>({
    mutationFn: async (to) => { unwrap(await api.POST('/api/v1/admin/smtp/test', { body: { to } })); },
  });
}

// —— 浏览器通知：开关保存在这个浏览器里（按空间），已经通知过的提醒也记在这里 ——

const browserKey = (ws: number) => `kept.notify.browser.${ws}`;
const seenKey = (ws: number) => `kept.notify.seen.${ws}`;

function read(key: string): string | null {
  try { return window.localStorage.getItem(key); } catch { return null; }
}
function write(key: string, v: string | null) {
  try { if (v === null) window.localStorage.removeItem(key); else window.localStorage.setItem(key, v); } catch { /* 存不了就算了 */ }
}

export const browserNotificationsSupported = () => typeof window !== 'undefined' && 'Notification' in window;

export function browserNotificationsOn(ws: number): boolean {
  return browserNotificationsSupported() && Notification.permission === 'granted' && read(browserKey(ws)) === '1';
}

export function setBrowserNotifications(ws: number, on: boolean, currentAlertIds: number[]) {
  write(browserKey(ws), on ? '1' : null);
  // 第一次开启时把已有的提醒记为已见，只通知之后新出现的
  if (on) write(seenKey(ws), JSON.stringify(currentAlertIds));
}

export function seenAlerts(ws: number): Set<number> {
  try { return new Set(JSON.parse(read(seenKey(ws)) ?? '[]') as number[]); } catch { return new Set(); }
}

export function setSeenAlerts(ws: number, ids: number[]) {
  write(seenKey(ws), JSON.stringify(ids));
}

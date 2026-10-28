// 紧急访问（v1.6.4）：空间管理员指定紧急联系人；对方申请后，等待期内没有拒绝就只读看到指定的抽屉。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';
import { t } from '../i18n';

export type EmergencyContact = components['schemas']['EmergencyContact'];
export type EmergencyAccess = components['schemas']['EmergencyAccess'];

const CONTACTS = ['emergency', 'contacts'] as const;
const MINE = ['emergency', 'mine'] as const;

/** 等待期的选项（小时）。 */
export const WAITS: [number, string][] = [24, 48, 72, 168, 336, 720].map((h): [number, string] => [h, t('{n} 天', { n: h / 24 })]);
export const waitLabel = (h: number) => WAITS.find(([x]) => x === h)?.[1] ?? (h % 24 === 0 ? t('{n} 天', { n: h / 24 }) : t('{n} 小时', { n: h }));

export function useEmergencyContacts(enabled = true) {
  return useQuery<EmergencyContact[], ApiError>({ queryKey: CONTACTS, queryFn: async () => unwrap(await api.GET('/api/v1/emergency-contacts')), enabled, refetchInterval: 60_000 });
}

export function useMyEmergencyAccess() {
  return useQuery<EmergencyAccess[], ApiError>({ queryKey: MINE, queryFn: async () => unwrap(await api.GET('/api/v1/emergency-access')), refetchInterval: 60_000 });
}

function useDone() {
  const qc = useQueryClient();
  return () => { void qc.invalidateQueries({ queryKey: ['emergency'] }); void qc.invalidateQueries({ queryKey: ['drawers'] }); /* 共享来的抽屉 */ };
}

export function useAddEmergencyContact() {
  const done = useDone();
  return useMutation<EmergencyContact, ApiError, { login: string; drawerIds: number[]; waitHours: number }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/emergency-contacts', { body })),
    onSuccess: done,
  });
}

export function useUpdateEmergencyContact() {
  const done = useDone();
  return useMutation<EmergencyContact, ApiError, { id: number; drawerIds: number[]; waitHours: number }>({
    mutationFn: async ({ id, ...body }) => unwrap(await api.PUT('/api/v1/emergency-contacts/{id}', { params: { path: { id } }, body })),
    onSuccess: done,
  });
}

export function useRemoveEmergencyContact() {
  const done = useDone();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/emergency-contacts/{id}', { params: { path: { id } } })); },
    onSuccess: done,
  });
}

/** 同意、拒绝、收回。 */
export function useEmergencyAction() {
  const done = useDone();
  return useMutation<EmergencyContact, ApiError, { id: number; action: 'approve' | 'deny' | 'revoke' }>({
    mutationFn: async ({ id, action }) => {
      const path = { params: { path: { id } } };
      if (action === 'approve') return unwrap(await api.POST('/api/v1/emergency-contacts/{id}/approve', path));
      if (action === 'deny') return unwrap(await api.POST('/api/v1/emergency-contacts/{id}/deny', path));
      return unwrap(await api.POST('/api/v1/emergency-contacts/{id}/revoke', path));
    },
    onSuccess: done,
  });
}

/** 申请或撤回（紧急联系人这一边）。 */
export function useRequestEmergencyAccess() {
  const done = useDone();
  return useMutation<EmergencyContact, ApiError, { id: number; reason?: string; cancel?: boolean }>({
    mutationFn: async ({ id, reason, cancel }) => cancel
      ? unwrap(await api.POST('/api/v1/emergency-access/{id}/cancel', { params: { path: { id } } }))
      : unwrap(await api.POST('/api/v1/emergency-access/{id}/request', { params: { path: { id } }, body: { reason } })),
    onSuccess: done,
  });
}

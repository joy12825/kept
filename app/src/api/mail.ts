import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';

export type MailAccount = components['schemas']['MailAccount'];
export type MailAccountInput = components['schemas']['MailAccountInput'];

const mailKey = (ws: number | undefined) => ['mail-accounts', ws] as const;

/** 当前空间的收件邮箱（空间管理员）。 */
export function useMailAccounts(ws: number | undefined) {
  return useQuery<MailAccount[], ApiError>({
    queryKey: mailKey(ws),
    enabled: !!ws,
    queryFn: async () => unwrap(await api.GET('/api/v1/mail-accounts')),
  });
}

export function useSaveMailAccount(ws: number | undefined) {
  const qc = useQueryClient();
  return useMutation<MailAccount, ApiError, { id?: number; body: MailAccountInput }>({
    mutationFn: async ({ id, body }) => (id
      ? unwrap(await api.PATCH('/api/v1/mail-accounts/{id}', { params: { path: { id } }, body }))
      : unwrap(await api.POST('/api/v1/mail-accounts', { body }))),
    onSuccess: () => qc.invalidateQueries({ queryKey: mailKey(ws) }),
  });
}

export function useDeleteMailAccount(ws: number | undefined) {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/mail-accounts/{id}', { params: { path: { id } } })); },
    onSuccess: () => qc.invalidateQueries({ queryKey: mailKey(ws) }),
  });
}

export function useTestMailAccount() {
  return useMutation<{ folders: string[] }, ApiError, number>({
    mutationFn: async (id) => unwrap(await api.POST('/api/v1/mail-accounts/{id}/test', { params: { path: { id } } })),
  });
}

export function useFetchMailAccount(ws: number | undefined) {
  const qc = useQueryClient();
  return useMutation<{ jobId: number }, ApiError, number>({
    mutationFn: async (id) => unwrap(await api.POST('/api/v1/mail-accounts/{id}/fetch', { params: { path: { id } } })),
    // 收取在后台进行：过一会儿刷新邮箱的“最近一次”
    onSuccess: () => { window.setTimeout(() => void qc.invalidateQueries({ queryKey: mailKey(ws) }), 4000); },
  });
}

export type MailMessage = components['schemas']['MailMessage'];

/** 邮箱最近处理过的邮件（v1.5.7）。 */
export function useMailMessages(id: number | undefined) {
  return useQuery<MailMessage[], ApiError>({
    queryKey: ['mail-accounts', 'messages', id],
    enabled: !!id,
    queryFn: async () => unwrap(await api.GET('/api/v1/mail-accounts/{id}/messages', { params: { path: { id: id! } } })),
  });
}

/** 重新收取一封邮件：按现在的规则再看一次。 */
export function useRefetchMailMessage(id: number) {
  const qc = useQueryClient();
  return useMutation<{ jobId: number }, ApiError, { uidValidity: number; uid: number }>({
    mutationFn: async ({ uidValidity, uid }) => unwrap(await api.POST('/api/v1/mail-accounts/{id}/messages/{uidValidity}/{uid}/refetch', { params: { path: { id, uidValidity, uid } } })),
    // 收取在后台进行：过一会儿刷新记录
    onSuccess: () => { window.setTimeout(() => void qc.invalidateQueries({ queryKey: ['mail-accounts'] }), 4000); },
  });
}

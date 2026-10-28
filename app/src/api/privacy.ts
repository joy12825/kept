// 家人的私密文档（v1.8.0）：谁能看某位家人的私密文档——本人、照看人、代管、只读；帮忙上传的人可被收回；私密文档的紧急联系人。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';
import { taxonomyKey } from './taxonomy';
import { t } from '../i18n';

export type PersonPrivacy = components['schemas']['PersonPrivacy'];
export type PersonAccess = components['schemas']['PersonAccess'];
export type PersonEmergency = components['schemas']['PersonEmergency'];
export type MyPrivacy = components['schemas']['MyPrivacy'];
export type AccessLevel = PersonAccess['level'];

export const LEVEL_LABEL: Record<AccessLevel, string> = { self: t('本人'), guardian: t('照看人'), manage: t('代管'), read: t('只读') };
export const LEVEL_HELP: Record<AccessLevel, string> = {
  self: t('这位家人自己的账号'),
  guardian: t('照看没有账号的家人（比如孩子），管理谁能看'),
  manage: t('能看、能修改和核对，不能管理谁能看'),
  read: t('只能查看'),
};

const MINE = ['privacy', 'mine'] as const;

export function useMyPrivacy() {
  return useQuery<MyPrivacy, ApiError>({ queryKey: MINE, queryFn: async () => unwrap(await api.GET('/api/v1/me/private')) });
}

/** 改了谁能看，家人列表、我的私密、文档都要重新取。 */
function useDone() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: taxonomyKey('persons') });
    void qc.invalidateQueries({ queryKey: ['privacy'] });
    void qc.invalidateQueries({ queryKey: ['documents'] });
  };
}

type Target = { personId: number; userId: number };
const path = ({ personId, userId }: Target) => ({ params: { path: { id: personId, userId } } });

export function useSetPersonAccess() {
  const done = useDone();
  return useMutation<PersonPrivacy, ApiError, Target & { level: AccessLevel }>({
    mutationFn: async ({ level, ...t }) => unwrap(await api.PUT('/api/v1/persons/{id}/access/{userId}', { ...path(t), body: { level } })),
    onSuccess: done,
  });
}

export function useRemovePersonAccess() {
  const done = useDone();
  return useMutation<PersonPrivacy, ApiError, Target>({
    mutationFn: async (t) => unwrap(await api.DELETE('/api/v1/persons/{id}/access/{userId}', path(t))),
    onSuccess: done,
  });
}

export function useReviewPersonAccess() {
  const done = useDone();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.POST('/api/v1/persons/{id}/access-reviewed', { params: { path: { id } } })); },
    onSuccess: done,
  });
}

export function useRevokeUploader() {
  const done = useDone();
  return useMutation<{ revoked: number }, ApiError, Target>({
    mutationFn: async (t) => unwrap(await api.DELETE('/api/v1/persons/{id}/uploaders/{userId}', path(t))),
    onSuccess: done,
  });
}

export function useSetPersonEmergency() {
  const done = useDone();
  return useMutation<PersonPrivacy, ApiError, Target & { waitHours: number }>({
    mutationFn: async ({ waitHours, ...t }) => unwrap(await api.PUT('/api/v1/persons/{id}/emergency/{userId}', { ...path(t), body: { waitHours } })),
    onSuccess: done,
  });
}

/** 本人或照看人：取消紧急联系人、拒绝申请；联系人自己：申请、撤回。 */
export function usePersonEmergencyAction() {
  const done = useDone();
  return useMutation<PersonPrivacy, ApiError, Target & { action: 'remove' | 'deny' | 'request' | 'cancel'; reason?: string }>({
    mutationFn: async ({ action, reason, ...t }) => {
      const p = { params: { path: { id: t.personId } } };
      switch (action) {
        case 'remove': return unwrap(await api.DELETE('/api/v1/persons/{id}/emergency/{userId}', path(t)));
        case 'deny': return unwrap(await api.POST('/api/v1/persons/{id}/emergency/{userId}/deny', path(t)));
        case 'request': return unwrap(await api.POST('/api/v1/persons/{id}/emergency-request', { ...p, body: reason ? { reason } : {} }));
        default: return unwrap(await api.DELETE('/api/v1/persons/{id}/emergency-request', p));
      }
    },
    onSuccess: done,
  });
}

// ---- 钥匙与云端 AI（v1.8.1）----

export type PersonKey = components['schemas']['PersonKey'];

/** 设钥匙：返回恢复码，只显示这一次。 */
export function useSetupPersonKey() {
  const done = useDone();
  return useMutation<{ recoveryCode: string }, ApiError, { personId: number; password: string }>({
    mutationFn: async ({ personId, password }) =>
      unwrap(await api.POST('/api/v1/persons/{id}/key', { params: { path: { id: personId } }, body: password ? { password } : {} })),
    onSuccess: done,
  });
}

/** 重新生成恢复码：用登录密码或现在的恢复码。 */
export function useNewRecoveryCode() {
  const done = useDone();
  return useMutation<{ recoveryCode: string }, ApiError, { personId: number; password?: string; recoveryCode?: string }>({
    mutationFn: async ({ personId, ...body }) =>
      unwrap(await api.POST('/api/v1/persons/{id}/key/recovery', { params: { path: { id: personId } }, body })),
    onSuccess: done,
  });
}

export function useCheckRecoveryCode() {
  return useMutation<{ ok: boolean }, ApiError, { personId: number; recoveryCode: string }>({
    mutationFn: async ({ personId, recoveryCode }) =>
      unwrap(await api.POST('/api/v1/persons/{id}/key/check', { params: { path: { id: personId } }, body: { recoveryCode } })),
  });
}

export function useSetPersonCloudAI() {
  const done = useDone();
  return useMutation<PersonPrivacy, ApiError, { personId: number; cloud: boolean }>({
    mutationFn: async ({ personId, cloud }) =>
      unwrap(await api.PUT('/api/v1/persons/{id}/ai', { params: { path: { id: personId } }, body: { cloud } })),
    onSuccess: done,
  });
}

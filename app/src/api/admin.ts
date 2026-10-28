import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap, type User } from './client';
import { meKey } from './session';

export const usersKey = ['admin', 'users'] as const;
export const settingsKey = ['admin', 'settings'] as const;

export function useUsers() {
  return useQuery<User[], ApiError>({ queryKey: usersKey, queryFn: async () => unwrap(await api.GET('/api/v1/admin/users')) });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation<User, ApiError, { email: string; username?: string; password: string; displayName?: string; isAdmin?: boolean }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/admin/users', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: usersKey }),
  });
}

export function useUpdateUser() {
  const qc = useQueryClient();
  return useMutation<User, ApiError, { id: number; disabled?: boolean; isAdmin?: boolean }>({
    mutationFn: async ({ id, ...body }) => unwrap(await api.PATCH('/api/v1/admin/users/{id}', { params: { path: { id } }, body })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: usersKey });
      qc.invalidateQueries({ queryKey: meKey });
    },
  });
}

export function useSetUserPassword() {
  return useMutation<void, ApiError, { id: number; password: string }>({
    mutationFn: async ({ id, password }) => {
      unwrap(await api.POST('/api/v1/admin/users/{id}/password', { params: { path: { id } }, body: { password } }));
    },
  });
}

export function useDisableUserTwoFactor() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: number }>({
    mutationFn: async ({ id }) => {
      unwrap(await api.DELETE('/api/v1/admin/users/{id}/2fa', { params: { path: { id } } }));
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: usersKey }),
  });
}

/** 解除账号的单点登录绑定（v1.0）；下次单点登录时按邮箱重新绑定。 */
export function useUnlinkUserSSO() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: number }>({
    mutationFn: async ({ id }) => {
      unwrap(await api.DELETE('/api/v1/admin/users/{id}/sso', { params: { path: { id } } }));
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: usersKey }),
  });
}

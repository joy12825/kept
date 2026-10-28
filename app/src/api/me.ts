import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap, type Me } from './client';
import { meKey } from './session';

export function useUpdateMe() {
  const qc = useQueryClient();
  return useMutation<Me, ApiError, { displayName: string; email: string }>({
    mutationFn: async (body) => unwrap(await api.PATCH('/api/v1/me', { body })),
    // 会话方式调用时响应也带 csrfToken，这里以响应为准、缺失时保留旧令牌
    onSuccess: (me) => qc.setQueryData<Me | null>(meKey, (old) => ({ ...me, csrfToken: old?.csrfToken ?? me.csrfToken })),
  });
}

export function useChangePassword() {
  return useMutation<void, ApiError, { currentPassword: string; newPassword: string }>({
    mutationFn: async (body) => { unwrap(await api.POST('/api/v1/me/password', { body })); },
  });
}

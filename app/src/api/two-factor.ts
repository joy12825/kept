import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap, type RecoveryCodes, type TwoFactorSetup, type TwoFactorStatus } from './client';
import { meKey } from './session';

export const twoFactorKey = ['twoFactor'] as const;

export function useTwoFactorStatus() {
  return useQuery<TwoFactorStatus, ApiError>({
    queryKey: twoFactorKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/me/2fa')),
  });
}

export function useBeginTwoFactor() {
  return useMutation<TwoFactorSetup, ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/me/2fa/setup')),
  });
}

export function useEnableTwoFactor() {
  const qc = useQueryClient();
  return useMutation<RecoveryCodes, ApiError, { code: string }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/me/2fa/enable', { body })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: twoFactorKey });
      qc.invalidateQueries({ queryKey: meKey });
    },
  });
}

export function useDisableTwoFactor() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { password: string }>({
    mutationFn: async (body) => { unwrap(await api.POST('/api/v1/me/2fa/disable', { body })); },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: twoFactorKey });
      qc.invalidateQueries({ queryKey: meKey });
    },
  });
}

export function useRegenerateRecoveryCodes() {
  const qc = useQueryClient();
  return useMutation<RecoveryCodes, ApiError, { password: string }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/me/2fa/recovery-codes', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: twoFactorKey }),
  });
}

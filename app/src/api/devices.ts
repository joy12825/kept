import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';

export type Device = components['schemas']['Device'];
export type PairingCode = components['schemas']['PairingCode'];

export const devicesKey = ['devices'] as const;

/** 我连接的手机。poll 为毫秒数时定期刷新（连接手机页等待配对时用）。 */
export function useDevices(poll?: number) {
  return useQuery<Device[], ApiError>({
    queryKey: devicesKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/devices')),
    refetchInterval: poll ?? false,
  });
}

/** 生成配对码（当前空间）；之前未使用的配对码作废。 */
export function useCreatePairingCode() {
  return useMutation<PairingCode, ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/devices/pairing-codes')),
  });
}

export function useRevokeDevice() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/devices/{id}', { params: { path: { id } } })); },
    onSuccess: () => qc.invalidateQueries({ queryKey: devicesKey }),
  });
}

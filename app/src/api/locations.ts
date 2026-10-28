// 纸质原件的存放位置（v1.6.3）：书房柜子第 2 层、文件盒 A、银行保险箱。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';

export type Location = components['schemas']['Location'];
export type LocationInput = components['schemas']['LocationInput'];

const KEY = ['taxonomy', 'locations'] as const;

export function useLocations() {
  return useQuery<Location[], ApiError>({ queryKey: KEY, queryFn: async () => unwrap(await api.GET('/api/v1/locations')) });
}

/** 新建或修改（有 id 时）。 */
export function useSaveLocation() {
  const qc = useQueryClient();
  return useMutation<Location, ApiError, { id?: number; body: LocationInput }>({
    mutationFn: async ({ id, body }) => id
      ? unwrap(await api.PUT('/api/v1/locations/{id}', { params: { path: { id } }, body }))
      : unwrap(await api.POST('/api/v1/locations', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  });
}

export function useDeleteLocation() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/locations/{id}', { params: { path: { id } } })); },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: KEY }); void qc.invalidateQueries({ queryKey: ['documents'] }); },
  });
}

/** 按原件编号范围批量指定位置；locationId 为 0 时清除。 */
export function useAssignLocationByAsn() {
  const qc = useQueryClient();
  return useMutation<{ updated: number }, ApiError, { locationId: number; from: number; to: number }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/locations/assign-asn', { body })),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: KEY }); void qc.invalidateQueries({ queryKey: ['documents'] }); },
  });
}

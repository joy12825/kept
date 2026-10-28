// 近似重复（阶段 20）：疑似重复的文档对，立即检查，保留其中一份或标记为不是重复。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import { documentsKey, jobsKey } from './documents';
import type { components } from './schema';

export type DuplicateList = components['schemas']['DuplicateList'];
export type DuplicatePair = components['schemas']['DuplicatePair'];

export const duplicatesKey = ['duplicates'] as const;

/** 正在检查时每 2 秒刷新一次，检查完就停。 */
export function useDuplicates() {
  return useQuery<DuplicateList, ApiError>({
    queryKey: duplicatesKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/duplicates')),
    refetchInterval: (q) => (q.state.data?.scanning ? 2000 : false),
  });
}

/** 含这份文档、还没处理的疑似重复（v1.3.7，详情页的提示）。 */
export function useDocumentDuplicates(id: number | undefined) {
  return useQuery<DuplicateList, ApiError>({
    queryKey: [...duplicatesKey, 'document', id],
    queryFn: async () => unwrap(await api.GET('/api/v1/duplicates', { params: { query: { document: id! } } })),
    enabled: id !== undefined,
    staleTime: 60_000,
  });
}

export function useScanDuplicates() {
  const qc = useQueryClient();
  return useMutation<{ jobId: number }, ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/duplicates/scan')),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: duplicatesKey }); void qc.invalidateQueries({ queryKey: jobsKey }); },
  });
}

export function useKeepDuplicate() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: number; documentId: number }>({
    mutationFn: async ({ id, documentId }) => { unwrap(await api.POST('/api/v1/duplicates/{id}/keep', { params: { path: { id } }, body: { documentId } })); },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: duplicatesKey }); void qc.invalidateQueries({ queryKey: documentsKey }); },
  });
}

export function useDismissDuplicate() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.POST('/api/v1/duplicates/{id}/dismiss', { params: { path: { id } } })); },
    onSuccess: () => qc.invalidateQueries({ queryKey: duplicatesKey }),
  });
}

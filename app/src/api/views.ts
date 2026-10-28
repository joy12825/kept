import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';

export type SavedView = components['schemas']['SavedView'];

export const viewsKey = ['saved-views'] as const;

/** 空间的常用筛选（成员共享）：按调整过的顺序，没调整过时按拼音。 */
export function useSavedViews() {
  return useQuery<SavedView[], ApiError>({ queryKey: viewsKey, queryFn: async () => unwrap(await api.GET('/api/v1/saved-views')) });
}

export function useCreateView() {
  const qc = useQueryClient();
  return useMutation<SavedView, ApiError, components['schemas']['SavedViewInput']>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/saved-views', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: viewsKey }),
  });
}

export function useUpdateView() {
  const qc = useQueryClient();
  return useMutation<SavedView, ApiError, { id: number; patch: components['schemas']['SavedViewPatch'] }>({
    mutationFn: async ({ id, patch }) => unwrap(await api.PATCH('/api/v1/saved-views/{id}', { params: { path: { id } }, body: patch })),
    onSuccess: () => qc.invalidateQueries({ queryKey: viewsKey }),
  });
}

export function useDeleteView() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/saved-views/{id}', { params: { path: { id } } })); },
    onSuccess: () => qc.invalidateQueries({ queryKey: viewsKey }),
  });
}

/** 调整常用筛选的顺序（v1.6.5）：先在本地换好，失败时重新读取。 */
export function useReorderViews() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number[], { before?: SavedView[] }>({
    mutationFn: async (ids) => { unwrap(await api.PUT('/api/v1/saved-views/order', { body: { ids } })); },
    onMutate: (ids) => {
      const before = qc.getQueryData<SavedView[]>(viewsKey);
      if (before) qc.setQueryData(viewsKey, ids.map((id) => before.find((v) => v.id === id)).filter((v): v is SavedView => !!v));
      return { before };
    },
    onError: (_e, _ids, c) => { if (c?.before) qc.setQueryData(viewsKey, c.before); },
    onSettled: () => qc.invalidateQueries({ queryKey: viewsKey }),
  });
}

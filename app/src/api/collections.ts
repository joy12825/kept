// 共享集（v1.0 阶段 37）：把选定的文档或一个筛选条件只读共享给另一个空间；对方从“共享来的”里看。
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, getWorkspaceId, unwrap } from './client';
import type { components } from './schema';
import type { SharedDocument } from './drawers';

export type Collection = components['schemas']['Collection'];
export type SharedCollection = components['schemas']['SharedCollection'];

export const collectionsKey = ['collections'] as const;
export const sharedCollectionsKey = ['shared-collections'] as const;

export function useCollections(enabled = true) {
  return useQuery<Collection[], ApiError>({
    queryKey: collectionsKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/collections')),
    enabled,
  });
}

export function useCreateCollection() {
  const qc = useQueryClient();
  return useMutation<Collection, ApiError, { name: string; documentIds?: number[]; filter?: Record<string, unknown>; workspaceIds: number[] }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/collections', { body })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: collectionsKey }),
  });
}

export function useUpdateCollection() {
  const qc = useQueryClient();
  return useMutation<Collection, ApiError, { id: number; name?: string; removeDocumentIds?: number[] }>({
    mutationFn: async ({ id, ...body }) => unwrap(await api.PATCH('/api/v1/collections/{id}', { params: { path: { id } }, body })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: collectionsKey }),
  });
}

export function useDeleteCollection() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/collections/{id}', { params: { path: { id } } })); },
    onSuccess: () => void qc.invalidateQueries({ queryKey: collectionsKey }),
  });
}

export function useSetCollectionShare() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: number; workspaceId: number; shared: boolean }>({
    mutationFn: async ({ id, workspaceId, shared }) => {
      const params = { path: { id, workspaceId } };
      if (shared) unwrap(await api.PUT('/api/v1/collections/{id}/shares/{workspaceId}', { params }));
      else unwrap(await api.DELETE('/api/v1/collections/{id}/shares/{workspaceId}', { params }));
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: collectionsKey }),
  });
}

// ---------- 对方：共享来的共享集 ----------

export function useSharedCollections(enabled = true) {
  return useQuery<SharedCollection[], ApiError>({
    queryKey: sharedCollectionsKey,
    enabled,
    queryFn: async () => unwrap(await api.GET('/api/v1/shared-collections')),
    staleTime: 30_000,
  });
}

export function useSharedCollection(id: number) {
  return useQuery<SharedCollection, ApiError>({
    queryKey: [...sharedCollectionsKey, id],
    queryFn: async () => unwrap(await api.GET('/api/v1/shared-collections/{id}', { params: { path: { id } } })),
  });
}

export function useSharedCollectionDocuments(id: number, q: string) {
  return useInfiniteQuery<components['schemas']['SharedDocumentList'], ApiError>({
    queryKey: [...sharedCollectionsKey, id, 'documents', q],
    initialPageParam: '',
    queryFn: async ({ pageParam }) => unwrap(await api.GET('/api/v1/shared-collections/{id}/documents', {
      params: { path: { id }, query: { q: q || undefined, limit: 50, cursor: (pageParam as string) || undefined } },
    })),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function useSharedCollectionDocument(id: number, docId: number) {
  return useQuery<SharedDocument, ApiError>({
    queryKey: [...sharedCollectionsKey, id, 'document', docId],
    queryFn: async () => unwrap(await api.GET('/api/v1/shared-collections/{id}/documents/{documentId}', { params: { path: { id, documentId: docId } } })),
  });
}

export function sharedCollectionFileUrl(id: number, docId: number, kind: 'original' | 'preview' | 'thumbnail') {
  const ws = getWorkspaceId();
  return `/api/v1/shared-collections/${id}/documents/${docId}/${kind}${ws !== null ? `?workspace=${ws}` : ''}`;
}

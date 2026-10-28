// 书单（阶段 28）：空间里共享的书单，书按顺序，每本可以写一句推荐语。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';

export type BookList = components['schemas']['BookList'];
export type BookListDetail = components['schemas']['BookListDetail'];
export type BookListItem = components['schemas']['BookListItem'];

const key = ['book-lists'] as const;

export function useBookLists() {
  return useQuery<BookList[], ApiError>({ queryKey: [...key, 'all'], queryFn: async () => unwrap(await api.GET('/api/v1/book-lists')) });
}

export function useBookList(id: number) {
  return useQuery<BookListDetail, ApiError>({
    queryKey: [...key, 'detail', id],
    queryFn: async () => unwrap(await api.GET('/api/v1/book-lists/{id}', { params: { path: { id } } })),
    enabled: id > 0,
  });
}

export function useDocumentBookLists(docId: number, enabled = true) {
  return useQuery<number[], ApiError>({
    queryKey: [...key, 'of', docId],
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/book-lists', { params: { path: { id: docId } } })),
    enabled,
  });
}

function useRefresh() {
  const qc = useQueryClient();
  return () => { void qc.invalidateQueries({ queryKey: key }); };
}

export function useCreateBookList() {
  const refresh = useRefresh();
  return useMutation<BookList, ApiError, { name: string; description?: string }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/book-lists', { body })),
    onSuccess: refresh,
  });
}

export function useUpdateBookList(id: number) {
  const refresh = useRefresh();
  return useMutation<BookList, ApiError, { name?: string; description?: string }>({
    mutationFn: async (body) => unwrap(await api.PATCH('/api/v1/book-lists/{id}', { params: { path: { id } }, body })),
    onSuccess: refresh,
  });
}

export function useDeleteBookList() {
  const refresh = useRefresh();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/book-lists/{id}', { params: { path: { id } } })); },
    onSuccess: refresh,
  });
}

export function useAddToBookList() {
  const refresh = useRefresh();
  return useMutation<void, ApiError, { listId: number; documentId: number; note?: string }>({
    mutationFn: async ({ listId, documentId, note }) => { unwrap(await api.POST('/api/v1/book-lists/{id}/items', { params: { path: { id: listId } }, body: { documentId, note } })); },
    onSuccess: refresh,
  });
}

export function useRemoveFromBookList() {
  const refresh = useRefresh();
  return useMutation<void, ApiError, { listId: number; documentId: number }>({
    mutationFn: async ({ listId, documentId }) => {
      unwrap(await api.DELETE('/api/v1/book-lists/{id}/items/{documentId}', { params: { path: { id: listId, documentId } } }));
    },
    onSuccess: refresh,
  });
}

export function useSetBookListNote(listId: number) {
  const refresh = useRefresh();
  return useMutation<void, ApiError, { documentId: number; note: string }>({
    mutationFn: async ({ documentId, note }) => {
      unwrap(await api.PATCH('/api/v1/book-lists/{id}/items/{documentId}', { params: { path: { id: listId, documentId } }, body: { note } }));
    },
    onSuccess: refresh,
  });
}

export function useReorderBookList(listId: number) {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number[]>({
    mutationFn: async (ids) => { unwrap(await api.PUT('/api/v1/book-lists/{id}/order', { params: { path: { id: listId } }, body: { ids } })); },
    // 先在缓存里排好，界面不等请求
    onMutate: (ids) => {
      qc.setQueryData<BookListDetail>([...key, 'detail', listId], (old) => old && {
        ...old, items: ids.map((id) => old.items.find((it) => it.documentId === id)!).filter(Boolean),
      });
    },
    onSettled: () => { void qc.invalidateQueries({ queryKey: key }); },
  });
}

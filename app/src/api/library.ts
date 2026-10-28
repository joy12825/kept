// 书房助手（阶段 33）：补全书目与摘要（按书，空间共享）、复习卡片（只属于自己）。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import { jobsKey } from './documents';
import type { components } from './schema';

export type BookSummary = components['schemas']['BookSummary'];
export type ReviewCard = components['schemas']['ReviewCard'];
export type ReviewCards = { items: ReviewCard[]; total: number; due: number };

export const bookSummaryKey = (id: number) => ['book-summary', id] as const;
export const reviewCardsKey = ['review-cards'] as const;

/** 让书房助手补全书目（提出建议）和 / 或写摘要，后台进行。 */
export function useAssistBook(docId: number) {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { catalog?: boolean; summary?: boolean }>({
    mutationFn: async (body) => { unwrap(await api.POST('/api/v1/documents/{id}/book/assist', { params: { path: { id: docId } }, body })); },
    onSuccess: () => void qc.invalidateQueries({ queryKey: jobsKey }),
  });
}

/** 书的摘要；没有时为 null（接口返回 404）。 */
export function useBookSummary(docId: number, poll = false) {
  return useQuery<BookSummary | null, ApiError>({
    queryKey: bookSummaryKey(docId),
    queryFn: async () => {
      const r = await api.GET('/api/v1/documents/{id}/book/summary', { params: { path: { id: docId } } });
      if (r.response.status === 404) return null;
      return unwrap(r);
    },
    refetchInterval: poll ? 2000 : false,
  });
}

export function useDeleteBookSummary(docId: number) {
  const qc = useQueryClient();
  return useMutation<void, ApiError, void>({
    mutationFn: async () => { unwrap(await api.DELETE('/api/v1/documents/{id}/book/summary', { params: { path: { id: docId } } })); },
    onSuccess: () => qc.setQueryData(bookSummaryKey(docId), null),
  });
}

export function useReviewCards(opts: { due?: boolean; documentId?: number } = {}, poll = false) {
  return useQuery<ReviewCards, ApiError>({
    queryKey: [...reviewCardsKey, opts],
    queryFn: async () => unwrap(await api.GET('/api/v1/review-cards', { params: { query: { due: opts.due, documentId: opts.documentId } } })),
    refetchInterval: poll ? 2000 : false,
  });
}

export function useGenerateCards() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (documentId) => { unwrap(await api.POST('/api/v1/review-cards/generate', { body: { documentId } })); },
    onSuccess: () => void qc.invalidateQueries({ queryKey: jobsKey }),
  });
}

/** 记下一次复习；列表在一轮复习结束后再刷新，免得复习中的卡片消失。 */
export function useReviewCard() {
  return useMutation<{ dueOn: string }, ApiError, { id: number; remembered: boolean }>({
    mutationFn: async ({ id, remembered }) => unwrap(await api.POST('/api/v1/review-cards/{id}/review', { params: { path: { id } }, body: { remembered } })),
  });
}

export function useDeleteCard() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/review-cards/{id}', { params: { path: { id } } })); },
    onSuccess: () => void qc.invalidateQueries({ queryKey: reviewCardsKey }),
  });
}

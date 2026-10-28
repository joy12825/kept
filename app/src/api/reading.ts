// 阅读位置（阶段 26）：每个人在每份文档里读到哪儿，PDF 为 page:页码，EPUB 为 CFI。
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, getCsrfToken, getWorkspaceId, unwrap } from './client';
import type { components } from './schema';
import { t, locale } from '../i18n';

export type ReadingProgress = components['schemas']['ReadingProgress'];

export const readingKey = (id: number) => ['reading', id] as const;

export function useReadingProgress(id: number | undefined) {
  return useQuery<ReadingProgress, ApiError>({
    queryKey: readingKey(id ?? 0),
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/reading', { params: { path: { id: id! } } })),
    enabled: id !== undefined,
    staleTime: 0,
  });
}

/** 保存阅读位置。keepalive：页面关闭时请求也能发出去。 */
export function saveReading(id: number, position: string, percent: number, keepalive = false): Promise<unknown> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json', 'Accept-Language': locale() };
  const csrf = getCsrfToken();
  if (csrf) headers['X-CSRF-Token'] = csrf;
  const ws = getWorkspaceId();
  if (ws !== null) headers['X-Workspace-ID'] = String(ws);
  const body = JSON.stringify({ position, percent: Math.min(1, Math.max(0, percent || 0)) });
  return fetch(`/api/v1/documents/${id}/reading`, { method: 'PUT', headers, body, credentials: 'include', keepalive }).catch(() => undefined);
}

// ---------- 阅读状态与批注（阶段 27） ----------

export type ReadingStatus = components['schemas']['ReadingStatus'];
export type ReadingStatePatch = components['schemas']['ReadingStatePatch'];
export type Highlight = components['schemas']['Highlight'];
export type HighlightColor = components['schemas']['HighlightColor'];
export type HighlightInput = components['schemas']['HighlightInput'];
export type HighlightPage = components['schemas']['HighlightPage'];

export const STATUS_LABEL: Record<Exclude<ReadingStatus, ''>, string> = { want: t('想读'), reading: t('在读'), finished: t('读完') };
export const HIGHLIGHT_COLORS: { value: HighlightColor; label: string; css: string }[] = [
  { value: 'yellow', label: t('黄'), css: 'rgba(255, 213, 79, 0.45)' },
  { value: 'green', label: t('绿'), css: 'rgba(129, 199, 132, 0.45)' },
  { value: 'blue', label: t('蓝'), css: 'rgba(100, 181, 246, 0.45)' },
  { value: 'pink', label: t('粉'), css: 'rgba(240, 98, 146, 0.4)' },
];
export const colorCss = (c: HighlightColor) => HIGHLIGHT_COLORS.find((x) => x.value === c)?.css ?? HIGHLIGHT_COLORS[0].css;

export function useSetReadingState(id: number) {
  const qc = useQueryClient();
  return useMutation<ReadingProgress, ApiError, ReadingStatePatch>({
    mutationFn: async (body) => unwrap(await api.PATCH('/api/v1/documents/{id}/reading', { params: { path: { id } }, body })),
    onSuccess: (p) => { qc.setQueryData(readingKey(id), p); void qc.invalidateQueries({ queryKey: ['documents', 'list'] }); void qc.invalidateQueries({ queryKey: ['yearbook'] }); },
  });
}

const highlightsKey = (id: number) => ['highlights', 'document', id] as const;

export function useHighlights(id: number | undefined) {
  return useQuery<Highlight[], ApiError>({
    queryKey: highlightsKey(id ?? 0),
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/highlights', { params: { path: { id: id! } } })),
    enabled: id !== undefined,
  });
}

/** 高亮有改动：这本书的列表、阅读状态里的条数、“我的批注”都刷新。 */
function useRefreshHighlights(id: number) {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: highlightsKey(id) });
    void qc.invalidateQueries({ queryKey: ['highlights', 'search'] });
    void qc.invalidateQueries({ queryKey: readingKey(id) });
  };
}

export function useCreateHighlight(id: number) {
  const refresh = useRefreshHighlights(id);
  return useMutation<Highlight, ApiError, HighlightInput>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/documents/{id}/highlights', { params: { path: { id } }, body })),
    onSuccess: refresh,
  });
}

export function useUpdateHighlight(docId: number) {
  const refresh = useRefreshHighlights(docId);
  return useMutation<Highlight, ApiError, { id: number; note?: string; color?: HighlightColor }>({
    mutationFn: async ({ id, ...body }) => unwrap(await api.PATCH('/api/v1/highlights/{id}', { params: { path: { id } }, body })),
    onSuccess: refresh,
  });
}

export function useDeleteHighlight(docId: number) {
  const refresh = useRefreshHighlights(docId);
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/highlights/{id}', { params: { path: { id } } })); },
    onSuccess: refresh,
  });
}

export function useSearchHighlights(q: string, color: HighlightColor | '', documentId?: number) {
  return useInfiniteQuery<HighlightPage, ApiError>({
    queryKey: ['highlights', 'search', q, color, documentId ?? 0],
    initialPageParam: '',
    queryFn: async ({ pageParam }) => unwrap(await api.GET('/api/v1/highlights', {
      params: { query: { q: q || undefined, color: color || undefined, documentId, cursor: (pageParam as string) || undefined, limit: 50 } },
    })),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

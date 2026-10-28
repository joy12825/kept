// 抽屉（设计文档 3.6、5.7）：抽屉与收纳规则、预览、模板、要点、提醒、年度回顾。
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, getWorkspaceId, unwrap } from './client';
import type { components } from './schema';

export type Drawer = components['schemas']['Drawer'];
export type DrawerDetail = components['schemas']['DrawerDetail'];
export type DrawerList = components['schemas']['DrawerList'];
export type DrawerIconName = components['schemas']['DrawerIcon'];
export type DrawerColorName = components['schemas']['DrawerColor'];
export type DrawerInput = components['schemas']['DrawerInput'];
export type DrawerPatch = components['schemas']['DrawerPatch'];
export type DrawerPreview = components['schemas']['DrawerPreview'];
export type DrawerPreviewRequest = components['schemas']['DrawerPreviewRequest'];
export type DrawerTemplates = components['schemas']['DrawerTemplates'];
export type Fact = components['schemas']['Fact'];
export type FactKind = components['schemas']['FactKind'];
export type FactConfig = components['schemas']['FactConfig'];
export type FactValue = components['schemas']['FactValue'];
export type FactInput = components['schemas']['FactInput'];
export type Alert = components['schemas']['Alert'];
export type Yearbook = components['schemas']['Yearbook'];

export const DRAWER_ICONS: DrawerIconName[] = ['home', 'car', 'heart', 'briefcase', 'baby', 'idcard', 'paw', 'wallet', 'plane', 'book', 'users', 'file', 'kanban', 'building'];
export const DRAWER_COLORS: DrawerColorName[] = ['blue', 'cyan', 'green', 'orange', 'red', 'purple', 'pink', 'gray'];

const drawersKey = ['drawers'] as const;

/** 抽屉、提醒和文档列表都受抽屉变化影响：一起刷新。 */
export function useRefreshDrawers() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: drawersKey });
    void qc.invalidateQueries({ queryKey: ['alerts'] });
    void qc.invalidateQueries({ queryKey: ['documents'] });
    void qc.invalidateQueries({ queryKey: ['yearbook'] });
  };
}

export function useDrawers() {
  return useQuery<DrawerList, ApiError>({
    queryKey: [...drawersKey, 'list'],
    queryFn: async () => unwrap(await api.GET('/api/v1/drawers')),
    staleTime: 30_000,
  });
}

export function useDrawer(id: number | undefined) {
  return useQuery<DrawerDetail, ApiError>({
    queryKey: [...drawersKey, 'detail', id],
    queryFn: async () => unwrap(await api.GET('/api/v1/drawers/{id}', { params: { path: { id: id! } } })),
    enabled: id !== undefined && id > 0,
  });
}

export function useCreateDrawer() {
  const refresh = useRefreshDrawers();
  return useMutation<components['schemas']['DrawerSaved'], ApiError, DrawerInput>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/drawers', { body })),
    onSuccess: refresh,
  });
}

export function useUpdateDrawer() {
  const refresh = useRefreshDrawers();
  return useMutation<components['schemas']['DrawerSaved'], ApiError, { id: number; patch: DrawerPatch }>({
    mutationFn: async ({ id, patch }) => unwrap(await api.PATCH('/api/v1/drawers/{id}', { params: { path: { id } }, body: patch })),
    onSuccess: refresh,
  });
}

export function useDeleteDrawer() {
  const refresh = useRefreshDrawers();
  return useMutation<components['schemas']['JobStarted'], ApiError, number>({
    mutationFn: async (id) => unwrap(await api.DELETE('/api/v1/drawers/{id}', { params: { path: { id } } })),
    onSuccess: refresh,
  });
}

export function useReorderDrawers() {
  const refresh = useRefreshDrawers();
  return useMutation<components['schemas']['JobStarted'], ApiError, number[]>({
    mutationFn: async (ids) => unwrap(await api.PUT('/api/v1/drawers/order', { body: { ids } })),
    onSuccess: refresh,
  });
}

export function usePreviewDrawers() {
  return useMutation<DrawerPreview, ApiError, DrawerPreviewRequest>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/drawers/preview', { body })),
  });
}

export function useDrawerTemplates(enabled = true) {
  return useQuery<DrawerTemplates, ApiError>({
    queryKey: ['drawer-templates'],
    queryFn: async () => unwrap(await api.GET('/api/v1/drawer-templates')),
    staleTime: Infinity,
    enabled,
  });
}

export function useFromTemplates() {
  const refresh = useRefreshDrawers();
  return useMutation<components['schemas']['DrawersCreated'], ApiError, string[]>({
    mutationFn: async (keys) => unwrap(await api.POST('/api/v1/drawers/from-templates', { body: { keys } })),
    onSuccess: refresh,
  });
}

export function useAddFact(drawerId: number) {
  const refresh = useRefreshDrawers();
  return useMutation<FactValue, ApiError, FactInput>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/drawers/{id}/facts', { params: { path: { id: drawerId } }, body })),
    onSuccess: refresh,
  });
}

export function useUpdateFact() {
  const refresh = useRefreshDrawers();
  return useMutation<FactValue, ApiError, { id: number; label: string; config: FactConfig }>({
    mutationFn: async ({ id, label, config }) => unwrap(await api.PATCH('/api/v1/drawer-facts/{id}', { params: { path: { id } }, body: { label, config } })),
    onSuccess: refresh,
  });
}

export function useDeleteFact() {
  const refresh = useRefreshDrawers();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/drawer-facts/{id}', { params: { path: { id } } })); },
    onSuccess: refresh,
  });
}

export function useReorderFacts(drawerId: number) {
  const refresh = useRefreshDrawers();
  return useMutation<void, ApiError, number[]>({
    mutationFn: async (ids) => { unwrap(await api.PUT('/api/v1/drawers/{id}/facts/order', { params: { path: { id: drawerId } }, body: { ids } })); },
    onSuccess: refresh,
  });
}

export function useAlerts() {
  return useQuery<Alert[], ApiError>({
    queryKey: ['alerts'],
    queryFn: async () => unwrap(await api.GET('/api/v1/alerts')),
  });
}

export function useSetAlert() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: number; dismissed: boolean }>({
    mutationFn: async ({ id, dismissed }) => {
      if (dismissed) unwrap(await api.POST('/api/v1/alerts/{id}/dismiss', { params: { path: { id } } }));
      else unwrap(await api.POST('/api/v1/alerts/{id}/restore', { params: { path: { id } } }));
    },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ['alerts'] }); void qc.invalidateQueries({ queryKey: drawersKey }); },
  });
}

export function useYears() {
  return useQuery<number[], ApiError>({
    queryKey: ['yearbook', 'years'],
    queryFn: async () => unwrap(await api.GET('/api/v1/yearbook')),
  });
}

export type MilestoneRules = components['schemas']['MilestoneRules'];

/** 年度回顾的大事记规则（阶段 21）。 */
export function useMilestoneRules(enabled = true) {
  return useQuery<MilestoneRules, ApiError>({
    queryKey: ['milestone-rules'],
    queryFn: async () => unwrap(await api.GET('/api/v1/yearbook/rules')),
    enabled,
  });
}

export function useSetMilestoneRules() {
  const qc = useQueryClient();
  return useMutation<MilestoneRules, ApiError, { rules?: MilestoneRules['rules']; reset?: boolean }>({
    mutationFn: async (body) => unwrap(await api.PUT('/api/v1/yearbook/rules', { body })),
    onSuccess: (r) => { qc.setQueryData(['milestone-rules'], r); void qc.invalidateQueries({ queryKey: ['yearbook'] }); },
  });
}

export function useYearbook(year: number | undefined) {
  return useQuery<Yearbook, ApiError>({
    queryKey: ['yearbook', year],
    queryFn: async () => unwrap(await api.GET('/api/v1/yearbook/{year}', { params: { path: { year: year! } } })),
    enabled: !!year,
  });
}

// ---------- 只读共享（设计文档 3.6） ----------

export type DrawerShare = components['schemas']['DrawerShare'];
export type SharedDrawer = components['schemas']['SharedDrawer'];
export type SharedDocumentSummary = components['schemas']['SharedDocumentSummary'];
export type SharedDocument = components['schemas']['SharedDocument'];

export function useDrawerShares(id: number, enabled = true) {
  return useQuery<DrawerShare[], ApiError>({
    queryKey: [...drawersKey, 'shares', id],
    queryFn: async () => unwrap(await api.GET('/api/v1/drawers/{id}/shares', { params: { path: { id } } })),
    enabled,
  });
}

export function useSetShare(id: number) {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { workspaceId: number; shared: boolean }>({
    mutationFn: async ({ workspaceId, shared }) => {
      const params = { path: { id, workspaceId } };
      if (shared) unwrap(await api.PUT('/api/v1/drawers/{id}/shares/{workspaceId}', { params }));
      else unwrap(await api.DELETE('/api/v1/drawers/{id}/shares/{workspaceId}', { params }));
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: drawersKey }),
  });
}

export function useSharedDrawers() {
  return useQuery<SharedDrawer[], ApiError>({
    queryKey: [...drawersKey, 'shared'],
    queryFn: async () => unwrap(await api.GET('/api/v1/shared-drawers')),
    staleTime: 30_000,
  });
}

export function useSharedDrawer(id: number) {
  return useQuery<SharedDrawer, ApiError>({
    queryKey: [...drawersKey, 'shared', id],
    queryFn: async () => unwrap(await api.GET('/api/v1/shared-drawers/{id}', { params: { path: { id } } })),
  });
}

export function useSharedDocuments(id: number, q: string) {
  return useInfiniteQuery<components['schemas']['SharedDocumentList'], ApiError>({
    queryKey: [...drawersKey, 'shared', id, 'documents', q],
    initialPageParam: '',
    queryFn: async ({ pageParam }) => unwrap(await api.GET('/api/v1/shared-drawers/{id}/documents', {
      params: { path: { id }, query: { q: q || undefined, limit: 50, cursor: (pageParam as string) || undefined } },
    })),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function useSharedDocument(id: number, docId: number) {
  return useQuery<SharedDocument, ApiError>({
    queryKey: [...drawersKey, 'shared', id, 'document', docId],
    queryFn: async () => unwrap(await api.GET('/api/v1/shared-drawers/{id}/documents/{documentId}', { params: { path: { id, documentId: docId } } })),
  });
}

/** 共享来的文档的文件地址（带当前空间，图片和下载链接没法设请求头）。 */
export function sharedFileUrl(id: number, docId: number, kind: 'original' | 'preview' | 'thumbnail') {
  const ws = getWorkspaceId();
  return `/api/v1/shared-drawers/${id}/documents/${docId}/${kind}${ws !== null ? `?workspace=${ws}` : ''}`;
}

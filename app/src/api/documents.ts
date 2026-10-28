import { useCallback } from 'react';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, getCsrfToken, getWorkspaceId, unwrap } from './client';
import type { components } from './schema';
import { useApp, type Upload } from '../store/app-store';
import { t, locale } from '../i18n';

export type DocumentSummary = components['schemas']['DocumentSummary'];
export type DocumentDetail = components['schemas']['Document'];
export type Job = components['schemas']['Job'];
export type DocumentList = components['schemas']['DocumentList'];
export type DocumentPatch = components['schemas']['DocumentPatch'];
export type FieldValue = components['schemas']['FieldValue'];
export type Facets = components['schemas']['Facets'];
export type Note = components['schemas']['Note'];
export type TrashedDocument = components['schemas']['TrashedDocument'];
export type AuditEvent = components['schemas']['AuditEvent'];
export type BulkRequest = components['schemas']['BulkRequest'];
/** field:字段ID 按自定义字段排序（v1.5.6） */
export type DocumentSort = 'created' | 'added' | 'modified' | 'title' | 'correspondent' | 'amount' | 'due' | 'asn' | 'relevance' | `field:${number}`;

export const MAX_UPLOAD_SIZE = 200 * 1024 * 1024;
export const inboxKey = ['documents', 'inbox'] as const;
/** 待整理：还没有完成整理的文档（设计文档 11.9 的筛选条件）。 */
export const INBOX_FILTER = JSON.stringify({ field: 'inbox', op: 'eq', value: true });
export const jobsKey = ['jobs'] as const;
export const documentKey = (id: number) => ['documents', 'detail', id] as const;
export const trashKey = ['trash'] as const;
// 文档有变化时要刷新的：列表、分面、详情、待整理都在 ['documents'] 下；目录的文档数在分类列表里
export const documentsKey = ['documents'] as const;

/** 待整理的文档，按添加时间从早到晚。 */
export function useInboxDocuments() {
  return useQuery<components['schemas']['DocumentList'], ApiError>({
    queryKey: inboxKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/documents', { params: { query: { filter: INBOX_FILTER, sort: 'added', dir: 'asc', limit: 200 } } })),
  });
}

export function useDocument(id: number | undefined) {
  return useQuery<DocumentDetail, ApiError>({
    queryKey: documentKey(id ?? 0),
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}', { params: { path: { id: id! } } })),
    enabled: id !== undefined,
  });
}

/** 当前空间最近 24 小时的入库任务；之后由 SSE 的 job.updated 更新。 */
export function useJobs() {
  return useQuery<Job[], ApiError>({
    queryKey: jobsKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/jobs')),
  });
}

/** 文件链接（预览、原件、缩略图）。<img> 和下载链接带不了请求头，空间放在查询参数里。 */
export function fileUrl(id: number, kind: 'preview' | 'original' | 'thumbnail' | 'cover', version?: string) {
  const q = new URLSearchParams();
  const ws = getWorkspaceId();
  if (ws !== null) q.set('workspace', String(ws));
  if (version) q.set('v', version);
  const qs = q.toString();
  return `/api/v1/documents/${id}/${kind}${qs ? `?${qs}` : ''}`;
}

/** 下载、打印时带上用途（v1.9.7）：敏感文档会记进文档历史；在页面里显示时不带。 */
export const withPurpose = (url: string, purpose: 'download' | 'print') => `${url}${url.includes('?') ? '&' : '?'}purpose=${purpose}`;

/**
 * 上传时的预设。drawerId：放入这个抽屉；onConflict：同一抽屉已有同名文档时作为它的新版本还是新文档（v1.7.13），不填按抽屉的设置。
 * v1.9.12 起上传框还能预设家人、标签、日期（填了就不从正文识别）、原件编号和自定义字段（值的格式同修改文档）。
 */
export interface Presets {
  documentTypeId?: number; correspondentId?: number; drawerId?: number; onConflict?: 'version' | 'new';
  personId?: number; tagIds?: number[]; createdDate?: string; asn?: number; customFields?: { fieldId: number; value: unknown }[];
}

/** 上传的结果：入库任务；作为新版本时 versionOf 是那份文档。 */
export interface Accepted { jobId: number; versionOf?: number }

/** 上传一个文件。用 XMLHttpRequest 是为了拿到上传进度（fetch 没有）。password 用于加密的 PDF，只用于解密，不保存。 */
export function uploadFile(file: File, presets: Presets, onProgress: (pct: number) => void, password?: string): Promise<Accepted> {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    // 预设字段放在文件之前：服务端边读边保存文件
    if (presets.documentTypeId) form.append('documentTypeId', String(presets.documentTypeId));
    if (presets.correspondentId) form.append('correspondentId', String(presets.correspondentId));
    if (presets.drawerId) form.append('drawerId', String(presets.drawerId));
    if (presets.onConflict) form.append('onConflict', presets.onConflict);
    if (presets.personId) form.append('personId', String(presets.personId));
    if (presets.tagIds?.length) form.append('tagIds', presets.tagIds.join(','));
    if (presets.createdDate) form.append('createdDate', presets.createdDate);
    if (presets.asn) form.append('asn', String(presets.asn));
    if (presets.customFields?.length) form.append('customFields', JSON.stringify(presets.customFields));
    if (password) form.append('password', password);
    form.append('file', file, file.name);
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/v1/documents');
    xhr.withCredentials = true;
    const csrf = getCsrfToken();
    if (csrf) xhr.setRequestHeader('X-CSRF-Token', csrf);
    const ws = getWorkspaceId();
    if (ws !== null) xhr.setRequestHeader('X-Workspace-ID', String(ws));
    xhr.setRequestHeader('Accept-Language', locale()); // 服务端按它回提示（v1.10.1）
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100)); };
    xhr.onload = () => {
      let body: { jobId?: number; versionOf?: number; code?: string; message?: string; details?: Record<string, unknown> } = {};
      try { body = JSON.parse(xhr.responseText); } catch { /* 非 JSON 响应 */ }
      if (xhr.status === 202 && body.jobId) resolve({ jobId: body.jobId, versionOf: body.versionOf });
      else reject(new ApiError(xhr.status, body.code ?? 'internal', body.message ?? t('上传失败，请稍后重试'), body.details));
    };
    xhr.onerror = () => reject(new ApiError(0, 'network', t('网络错误，上传没有完成')));
    xhr.send(form);
  });
}

// 等待输入密码的加密 PDF：File 不能放进界面状态，按托盘里那一行的 key 记在这里
const waitingForPassword = new Map<string, { file: File; presets: Presets }>();

type AppHandle = ReturnType<typeof useApp>;
type QueryClient = ReturnType<typeof useQueryClient>;

/** 上传托盘里的一行；加密的 PDF 没有密码或密码不对时记下文件，等用户输入密码。 */
function send(app: AppHandle, qc: QueryClient, key: string, file: File, presets: Presets, password?: string) {
  const patch = (u: Partial<Upload>) => app.set((p) => ({ uploads: p.uploads.map((x) => (x.key === key ? { ...x, ...u } : x)) }));
  patch({ pct: 0, error: undefined, needsPassword: false });
  uploadFile(file, presets, (pct) => patch({ pct }), password)
    .then(({ jobId, versionOf }) => {
      waitingForPassword.delete(key); patch({ pct: 100, jobId, versionOf }); void qc.invalidateQueries({ queryKey: jobsKey }); void qc.invalidateQueries({ queryKey: ['tasks'] });
      if (versionOf) void qc.invalidateQueries({ queryKey: documentKey(versionOf) });
    })
    .catch((e: ApiError) => {
      const needsPassword = e.code === 'pdf_encrypted' || e.code === 'wrong_password';
      if (needsPassword) waitingForPassword.set(key, { file, presets }); else waitingForPassword.delete(key);
      patch({ error: e.code === 'duplicate' ? t('这份文件已经存在') : e.message, documentId: e.details?.documentId as number | undefined, needsPassword });
    });
}

export type SameNameMatch = { name: string; documentId: number; title: string };

/** 同一抽屉（没有抽屉时为不在抽屉里的文档）里和这些文件同名的文档（v1.7.13）；auto 表示抽屉设为直接作为新版本。 */
export async function checkSameName(drawerId: number | undefined, names: string[]) {
  return unwrap(await api.POST('/api/v1/uploads/same-name', { body: { drawerId, names } }));
}

/** 开始上传：在任务托盘里加一行，上传完后由任务的进度接着显示。versions 里的文件名作为同名文档的新版本，其余作为新文档。 */
function start(app: AppHandle, qc: QueryClient, files: File[], presets: Presets, versions?: Set<string>) {
  const added: Upload[] = files.map((f, i) => ({ key: `${Date.now()}-${i}-${f.name}`, name: f.name, pct: 0 }));
  app.set((p) => ({ uploadOpen: false, trayOpen: true, uploads: [...added, ...p.uploads] }));
  files.forEach((file, i) => {
    const key = added[i].key;
    const fail = (error: string) => app.set((p) => ({ uploads: p.uploads.map((x) => (x.key === key ? { ...x, error } : x)) }));
    if (file.size > MAX_UPLOAD_SIZE) { fail(t('文件超过 200 MB 的上限')); return; }
    if (file.size === 0) { fail(t('文件是空的')); return; }
    send(app, qc, key, file, versions ? { ...presets, onConflict: versions.has(file.name) ? 'version' : 'new' } : presets);
  });
}

// 等用户决定同名文件怎么处理的一组上传：File 不能放进界面状态，按 token 记在这里
const waitingForSameName = new Map<string, { files: File[]; presets: Presets }>();

/** 上传一组文件。同一抽屉里已有同名文档、抽屉又没设为直接作为新版本时，先问用户（见 SameNameDialog）；检查失败时照常上传。 */
export function useUploader() {
  const app = useApp();
  const qc = useQueryClient();
  return useCallback((files: File[], presets: Presets = {}) => {
    app.set({ uploadOpen: false });
    const names = files.filter((f) => f.size > 0 && f.size <= MAX_UPLOAD_SIZE).map((f) => f.name);
    if (!names.length || presets.onConflict) { start(app, qc, files, presets); return; }
    checkSameName(presets.drawerId, names).then(
      (r) => {
        if (r.auto || !r.matches.length) { start(app, qc, files, presets); return; }
        const token = `${Date.now()}-${Math.random()}`;
        waitingForSameName.set(token, { files, presets });
        app.set({ sameName: { token, matches: r.matches } });
      },
      () => start(app, qc, files, presets),
    );
  }, [app, qc]);
}

/** 用户对同名文件的决定：versions 是作为新版本的文件名；null 为取消这次上传。 */
export function useResolveSameName() {
  const app = useApp();
  const qc = useQueryClient();
  return useCallback((token: string, versions: Set<string> | null) => {
    const w = waitingForSameName.get(token);
    waitingForSameName.delete(token);
    app.set({ sameName: null });
    if (w && versions) start(app, qc, w.files, w.presets, versions);
  }, [app, qc]);
}

/** 用密码重新上传托盘里那一份加密的 PDF；文件已经不在了（页面刷新过）时返回 false。 */
export function useRetryWithPassword() {
  const app = useApp();
  const qc = useQueryClient();
  return useCallback((key: string, password: string) => {
    const w = waitingForPassword.get(key);
    if (!w) return false;
    send(app, qc, key, w.file, w.presets, password);
    return true;
  }, [app, qc]);
}

/** 重新处理一个失败的上传（v1.9.12）：服务器保留了文件 7 天，不用再选文件。 */
export function useRetryJob() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.POST('/api/v1/jobs/{id}/retry', { params: { path: { id } } })); },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: jobsKey }); void qc.invalidateQueries({ queryKey: ['tasks'] }); },
  });
}

export type PageEdits = components['schemas']['PageEdits'];

/** 旋转、删除页面（完整的目标状态，页码按编辑前的 PDF）；空状态恢复原样。 */
export function useSetPages(id: number) {
  const qc = useQueryClient();
  return useMutation<DocumentDetail, ApiError, PageEdits>({
    mutationFn: async (body) => unwrap(await api.PUT('/api/v1/documents/{id}/pages', { params: { path: { id } }, body })),
    onSuccess: (d) => { qc.setQueryData(documentKey(id), d); void qc.invalidateQueries({ queryKey: documentsKey }); },
  });
}

/** 在指定的页之后拆开，生成新文档（后台任务）。 */
export function useSplitDocument(id: number) {
  const qc = useQueryClient();
  return useMutation<{ jobId: number }, ApiError, { after: number[]; trashOriginal: boolean }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/documents/{id}/split', { params: { path: { id } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: jobsKey }),
  });
}

/** 按顺序合并几份 PDF 为一份新文档（后台任务）。 */
export function useMergeDocuments() {
  const qc = useQueryClient();
  return useMutation<{ jobId: number }, ApiError, { ids: number[]; trashOriginals: boolean }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/documents/merge', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: jobsKey }),
  });
}

/** 文档能不能编辑页面、拆分、合并：要有 PDF（可搜索版或 PDF 原件）。 */
export const isPdfDocument = (d: { hasArchive: boolean; mimeType: string }) => d.hasArchive || d.mimeType === 'application/pdf';

/** fields：每行带上这些自定义字段的值（逗号分隔的字段 ID，v1.5.6） */
export interface ListQuery { filter: string; sort: DocumentSort; dir: 'asc' | 'desc'; fields?: string }

/** 文档列表，滚到底时用 fetchNextPage 取下一页。filter 为筛选条件 JSON（'' 表示全部）。 */
export function useDocumentList(q: ListQuery, enabled = true) {
  return useInfiniteQuery<DocumentList, ApiError>({
    queryKey: ['documents', 'list', q.filter, q.sort, q.dir, q.fields ?? ''],
    initialPageParam: '',
    queryFn: async ({ pageParam }) => unwrap(await api.GET('/api/v1/documents', {
      params: { query: { filter: q.filter || undefined, sort: q.sort, dir: q.dir, limit: 50, cursor: (pageParam as string) || undefined, fields: q.fields || undefined } },
    })),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    // 只是加减了自定义字段的列：先显示原来的行，不闪成加载中（列菜单也不会被关掉）
    placeholderData: (prev, prevQuery) => (prevQuery && prevQuery.queryKey.slice(0, 5).join('\u0000') === ['documents', 'list', q.filter, q.sort, q.dir].join('\u0000') ? prev : undefined),
    enabled,
  });
}

/** 最近添加的几份文档（首页）。 */
export function useRecentDocuments(limit = 8) {
  return useQuery<DocumentList, ApiError>({
    queryKey: ['documents', 'recent', limit],
    queryFn: async () => unwrap(await api.GET('/api/v1/documents', { params: { query: { sort: 'added', dir: 'desc', limit } } })),
  });
}

/** 筛选按钮各选项的数量和月份分组的份数。 */
export function useFacets(filter: string, groups: string[], enabled = true) {
  return useQuery<Facets, ApiError>({
    queryKey: ['documents', 'facets', filter, groups.join(',')],
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/facets', { params: { query: { filter: filter || undefined, groups: groups.join(',') } } })),
    enabled,
  });
}

/** 文档有变化后刷新相关数据：文档的各种列表、待整理、目录里的文档数、回收站。 */
export function useRefreshDocuments() {
  const qc = useQueryClient();
  return useCallback(() => {
    void qc.invalidateQueries({ queryKey: documentsKey });
    void qc.invalidateQueries({ queryKey: trashKey });
    void qc.invalidateQueries({ queryKey: ['taxonomy'] });
  }, [qc]);
}

export function useUpdateDocument() {
  const qc = useQueryClient();
  const refresh = useRefreshDocuments();
  return useMutation<DocumentDetail, ApiError, { id: number; patch: DocumentPatch }>({
    mutationFn: async ({ id, patch }) => unwrap(await api.PATCH('/api/v1/documents/{id}', { params: { path: { id } }, body: patch })),
    onSuccess: (d) => { qc.setQueryData(documentKey(d.id), d); refresh(); },
  });
}

export function useBulk() {
  const refresh = useRefreshDocuments();
  return useMutation<components['schemas']['BulkResult'], ApiError, BulkRequest>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/documents/bulk', { body })),
    onSuccess: refresh,
  });
}

export function useDeleteDocument() {
  const refresh = useRefreshDocuments();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/documents/{id}', { params: { path: { id } } })); },
    onSuccess: refresh,
  });
}

export function useRestoreDocument() {
  const refresh = useRefreshDocuments();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.POST('/api/v1/documents/{id}/restore', { params: { path: { id } } })); },
    onSuccess: refresh,
  });
}

export function useReprocess() {
  return useMutation<number, ApiError, number>({
    mutationFn: async (id) => unwrap(await api.POST('/api/v1/documents/{id}/reprocess', { params: { path: { id } } })).jobId,
  });
}

// ---------- 回收站 ----------

export function useTrash() {
  return useQuery<TrashedDocument[], ApiError>({ queryKey: trashKey, queryFn: async () => unwrap(await api.GET('/api/v1/trash')) });
}

export function usePurgeDocument() {
  const refresh = useRefreshDocuments();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/trash/{id}', { params: { path: { id } } })); },
    onSuccess: refresh,
  });
}

export function useEmptyTrash() {
  const refresh = useRefreshDocuments();
  return useMutation<number, ApiError, void>({
    mutationFn: async () => unwrap(await api.DELETE('/api/v1/trash')).count,
    onSuccess: refresh,
  });
}

// ---------- 备注与修改历史 ----------

export const notesKey = (id: number) => ['documents', 'notes', id] as const;

export function useNotes(id: number | undefined) {
  return useQuery<Note[], ApiError>({
    queryKey: notesKey(id ?? 0),
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/notes', { params: { path: { id: id! } } })),
    enabled: id !== undefined,
  });
}

export function useAddNote(id: number) {
  const qc = useQueryClient();
  return useMutation<Note, ApiError, string>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/documents/{id}/notes', { params: { path: { id } }, body: { body } })),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: documentsKey }); },
  });
}

export function useDeleteNote(id: number) {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (noteId) => { unwrap(await api.DELETE('/api/v1/documents/{id}/notes/{noteId}', { params: { path: { id, noteId } } })); },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: documentsKey }); },
  });
}

export type MoveResult = components['schemas']['MoveResult'];

/** 把文档移到另一个空间（v1.3.8）。移走后从当前空间的列表里消失。 */
export function useMoveDocuments() {
  const refresh = useRefreshDocuments();
  return useMutation<MoveResult, ApiError, { ids: number[]; workspaceId: number }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/documents/move', { body })),
    onSuccess: refresh,
  });
}

export type FileInfo = components['schemas']['FileInfo'];

/** 文件自带的信息（v1.3.7）：打开“文件信息”时才读取；文件换了（修改时间变了）再读。 */
export function useFileInfo(d: { id: number; modifiedAt: string } | undefined, enabled: boolean) {
  return useQuery<FileInfo, ApiError>({
    queryKey: [...documentKey(d?.id ?? 0), 'file-info', d?.modifiedAt],
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/file-info', { params: { path: { id: d!.id } } })),
    enabled: enabled && !!d,
    staleTime: Infinity,
  });
}

export function useHistory(id: number | undefined, enabled = true) {
  return useInfiniteQuery<components['schemas']['AuditPage'], ApiError>({
    queryKey: ['documents', 'history', id ?? 0],
    initialPageParam: '',
    queryFn: async ({ pageParam }) => unwrap(await api.GET('/api/v1/documents/{id}/history', {
      params: { path: { id: id! }, query: { limit: 20, cursor: (pageParam as string) || undefined } },
    })),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    enabled: enabled && id !== undefined,
  });
}

/** 批量下载的链接：按 ID 或筛选条件打包成 zip。 */
export function downloadUrl(sel: { ids: number[] } | { filter: string }, kind: 'original' | 'archive' = 'original') {
  const q = new URLSearchParams();
  const ws = getWorkspaceId();
  if (ws !== null) q.set('workspace', String(ws));
  if ('ids' in sel) q.set('ids', sel.ids.join(','));
  else q.set('filter', sel.filter);
  q.set('kind', kind);
  return `/api/v1/documents/download?${q}`;
}

/** 让浏览器下载一个链接（不离开当前页）。 */
/** 把当前空间导出为普通文件（v1.4.1）：边打包边下载的 zip。 */
export function exportUrl(file: 'searchable' | 'original') {
  const q = new URLSearchParams({ file });
  const ws = getWorkspaceId();
  if (ws !== null) q.set('workspace', String(ws));
  return `/api/v1/export?${q}`;
}

export function startDownload(url: string) {
  const a = document.createElement('a');
  a.href = url;
  a.download = ''; // 同源链接：下载而不是在页面里打开（可搜索版是 inline）
  a.rel = 'noopener';
  document.body.appendChild(a);
  a.click();
  a.remove();
}

/** 金额：接口里两位小数的字符串 “326.40” → “¥326.40”。 */
export function formatAmount(amount?: string) {
  if (!amount) return '';
  const [i, f = '00'] = amount.replace('-', '').split('.');
  return `${amount.startsWith('-') ? '-' : ''}¥${Number(i).toLocaleString('en-US')}.${f.padEnd(2, '0')}`;
}

/** 原件编号：#000123。 */
export const formatAsn = (asn?: number) => (asn ? `#${String(asn).padStart(6, '0')}` : '');

// 办事材料清单（v1.6.2）：列表、增删改、核对。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, getCsrfToken, getWorkspaceId, unwrap } from './client';
import type { components } from './schema';
import { t } from '../i18n';

export type Checklist = components['schemas']['Checklist'];
export type ChecklistItem = components['schemas']['ChecklistItem'];
export type ChecklistInput = components['schemas']['ChecklistInput'];
export type ChecklistCheck = components['schemas']['ChecklistCheck'];
export type ChecklistCheckItem = components['schemas']['ChecklistCheckItem'];

const key = ['checklists'] as const;

export function useChecklists() {
  return useQuery<Checklist[], ApiError>({ queryKey: key, queryFn: async () => unwrap(await api.GET('/api/v1/checklists')) });
}

/** 核对结果：文档有变化时随文档一起刷新（键以 documents 开头）。 */
export function useChecklistCheck(id: number) {
  return useQuery<ChecklistCheck, ApiError>({
    queryKey: ['documents', 'checklist', id],
    queryFn: async () => unwrap(await api.GET('/api/v1/checklists/{id}/check', { params: { path: { id } } })),
  });
}

export function useSaveChecklist() {
  const qc = useQueryClient();
  return useMutation<Checklist, ApiError, { id?: number; body: ChecklistInput }>({
    mutationFn: async ({ id, body }) => (id
      ? unwrap(await api.PUT('/api/v1/checklists/{id}', { params: { path: { id } }, body }))
      : unwrap(await api.POST('/api/v1/checklists', { body }))),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: key }); void qc.invalidateQueries({ queryKey: ['documents', 'checklist'] }); },
  });
}

export function useDeleteChecklist() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/checklists/{id}', { params: { path: { id } } })); },
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  });
}

// ---------- 直接补材料（v1.6.7）----------

/** 把已有的文档放进某一项，或移出。 */
export function useChecklistDocument(id: number) {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { itemId: string; documentId: number; remove?: boolean }>({
    mutationFn: async ({ itemId, documentId, remove }) => {
      if (remove) unwrap(await api.DELETE('/api/v1/checklists/{id}/items/{itemId}/documents/{documentId}', { params: { path: { id, itemId, documentId } } }));
      else unwrap(await api.POST('/api/v1/checklists/{id}/items/{itemId}/documents', { params: { path: { id, itemId } }, body: { documentId } }));
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['documents', 'checklist', id] }),
  });
}

/** 上传一个文件到某一项。返回后台任务（jobId），或文件已经在空间里时直接放进去的那份（documentId）。 */
export function uploadToChecklistItem(id: number, itemId: string, file: File, onProgress: (pct: number) => void, title?: string): Promise<{ jobId?: number; documentId?: number }> {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    if (title) form.append('title', title); // 放在文件之前：服务端边读边保存文件
    form.append('file', file, file.name);
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `/api/v1/checklists/${id}/items/${encodeURIComponent(itemId)}/upload`);
    xhr.withCredentials = true;
    const csrf = getCsrfToken();
    if (csrf) xhr.setRequestHeader('X-CSRF-Token', csrf);
    const ws = getWorkspaceId();
    if (ws !== null) xhr.setRequestHeader('X-Workspace-ID', String(ws));
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100)); };
    xhr.onload = () => {
      let body: { jobId?: number; documentId?: number; code?: string; message?: string } = {};
      try { body = JSON.parse(xhr.responseText); } catch { /* 非 JSON 响应 */ }
      if (xhr.status === 202 || xhr.status === 200) resolve(body);
      else reject(new ApiError(xhr.status, body.code ?? 'internal', body.message ?? t('上传失败，请稍后重试')));
    };
    xhr.onerror = () => reject(new ApiError(0, 'network', t('网络错误，上传没有完成')));
    xhr.send(form);
  });
}

// 文档版本（阶段 19）：列表、上传新版本、设为当前版本、删除、移除密码。切换后服务端按新版本重新处理。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, getWorkspaceId, unwrap } from './client';
import { documentKey, jobsKey } from './documents';
import type { components } from './schema';
import { t } from '../i18n';

export type DocumentVersion = components['schemas']['DocumentVersion'];

const key = (docId: number) => ['document-versions', docId] as const;

export function useDocumentVersions(docId: number) {
  return useQuery<DocumentVersion[], ApiError>({
    queryKey: key(docId),
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/versions', { params: { path: { id: docId } } })),
  });
}

/** 某个版本显示用的 PDF（v1.9.7 并排看）：旧版本的 Office、邮件、OFD 由服务器现转。取成 blob 地址；转不了时报服务器给的原因。 */
export function useVersionPreview(docId: number, versionId: number | undefined) {
  const q = useQuery<string, ApiError>({
    queryKey: ['document-version-preview', docId, versionId],
    enabled: versionId !== undefined,
    staleTime: Infinity,
    retry: false,
    queryFn: async () => {
      const ws = getWorkspaceId();
      const res = await fetch(`/api/v1/documents/${docId}/versions/${versionId}/preview${ws !== null ? `?workspace=${ws}` : ''}`, { credentials: 'include' });
      if (!res.ok) {
        const body = await res.json().catch(() => ({})) as { code?: string; message?: string };
        throw new ApiError(res.status, body.code ?? 'error', body.message ?? t('这个版本没能显示出来，请稍后再试'));
      }
      return URL.createObjectURL(await res.blob());
    },
  });
  return q;
}

/** 下载某个版本（链接里带上当前空间，浏览器直接打开）；inline：在页面里显示（版本对比，v1.9.2）。 */
export function versionFileUrl(docId: number, versionId: number, inline = false) {
  const q = new URLSearchParams();
  const ws = getWorkspaceId();
  if (ws !== null) q.set('workspace', String(ws));
  if (inline) q.set('inline', 'true');
  const qs = q.toString();
  return `/api/v1/documents/${docId}/versions/${versionId}/file${qs ? `?${qs}` : ''}`;
}

export interface VersionText { text: string; source: 'content' | 'file' | 'none' }

/** 某个版本的文字（版本对比，v1.9.2）：当前版本是文档的文字，旧版本从文件里读，扫描件没有。 */
export function useVersionText(docId: number, versionId: number | undefined) {
  return useQuery<VersionText, ApiError>({
    queryKey: ['document-version-text', docId, versionId],
    enabled: versionId !== undefined,
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/versions/{versionId}/text', { params: { path: { id: docId, versionId: versionId! } } })),
  });
}

function useAfterSwap(docId: number) {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: key(docId) });
    void qc.invalidateQueries({ queryKey: documentKey(docId) });
    void qc.invalidateQueries({ queryKey: jobsKey });
  };
}

export function useAddVersion(docId: number) {
  const done = useAfterSwap(docId);
  return useMutation<{ jobId: number }, ApiError, { file: File; label: string }>({
    mutationFn: async ({ file, label }) => {
      const form = new FormData();
      if (label.trim()) form.append('label', label.trim()); // 说明放在文件之前
      form.append('file', file, file.name);
      return unwrap(await api.POST('/api/v1/documents/{id}/versions', {
        params: { path: { id: docId } }, body: form as never, bodySerializer: (b: unknown) => b as FormData,
      }));
    },
    onSuccess: done,
  });
}

/** 把另一份文档并为这份的新版本（v1.5.8）：它的标签、备注并过来，它不再单独存在。 */
export function useAddVersionFromDocument(docId: number) {
  const done = useAfterSwap(docId);
  const qc = useQueryClient();
  return useMutation<{ jobId: number }, ApiError, { documentId: number; label: string }>({
    mutationFn: async ({ documentId, label }) => unwrap(await api.POST('/api/v1/documents/{id}/versions/from-document', {
      params: { path: { id: docId } }, body: { documentId, ...(label.trim() ? { label: label.trim() } : {}) },
    })),
    onSuccess: () => { done(); void qc.invalidateQueries({ queryKey: ['documents'] }); },
  });
}

export function useSetCurrentVersion(docId: number) {
  const done = useAfterSwap(docId);
  return useMutation<{ jobId: number }, ApiError, number>({
    mutationFn: async (versionId) => unwrap(await api.POST('/api/v1/documents/{id}/versions/{versionId}/current', { params: { path: { id: docId, versionId } } })),
    onSuccess: done,
  });
}

export function useDeleteVersion(docId: number) {
  const done = useAfterSwap(docId);
  return useMutation<void, ApiError, number>({
    mutationFn: async (versionId) => { unwrap(await api.DELETE('/api/v1/documents/{id}/versions/{versionId}', { params: { path: { id: docId, versionId } } })); },
    onSuccess: done,
  });
}

export function useRemovePassword(docId: number) {
  const done = useAfterSwap(docId);
  return useMutation<{ jobId: number }, ApiError, string>({
    mutationFn: async (password) => unwrap(await api.POST('/api/v1/documents/{id}/versions/decrypt', { params: { path: { id: docId } }, body: { password } })),
    onSuccess: done,
  });
}

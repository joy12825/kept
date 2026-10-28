// 收集材料的上传链接（v1.6.2）：管理（编辑者）与公开的上传页。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';
import { t } from '../i18n';

export type UploadLink = components['schemas']['UploadLink'];
export type UploadLinkInput = components['schemas']['UploadLinkInput'];
export type PublicUploadLink = components['schemas']['PublicUploadLink'];

const key = ['upload-links'] as const;

/** 链接的完整地址（发给对方的）。 */
export const uploadLinkUrl = (token: string) => `${window.location.origin}/u/${token}`;

export function useUploadLinks() {
  return useQuery<UploadLink[], ApiError>({ queryKey: key, queryFn: async () => unwrap(await api.GET('/api/v1/upload-links')) });
}

export function useCreateUploadLink() {
  const qc = useQueryClient();
  return useMutation<UploadLink, ApiError, UploadLinkInput>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/upload-links', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  });
}

export function useRevokeUploadLink() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/upload-links/{id}', { params: { path: { id } } })); },
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  });
}

export function usePublicUploadLink(token: string) {
  return useQuery<PublicUploadLink, ApiError>({
    queryKey: ['public-upload', token], retry: false,
    queryFn: async () => unwrap(await api.GET('/api/v1/public/uploads/{token}', { params: { path: { token } } })),
  });
}

/** 通过链接上传一个文件；onProgress 给出 0–100。用 XMLHttpRequest 才拿得到上传进度。 */
export function uploadThroughLink(token: string, file: File, onProgress: (pct: number) => void): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `/api/v1/public/uploads/${encodeURIComponent(token)}`);
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100)); };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) { onProgress(100); resolve(); return; }
      let msg = t('上传失败，请稍后再试');
      try { msg = JSON.parse(xhr.responseText).message ?? msg; } catch { /* 不是 JSON */ }
      reject(new Error(msg));
    };
    xhr.onerror = () => reject(new Error(t('网络连接失败，请稍后再试')));
    const form = new FormData();
    form.append('file', file, file.name);
    xhr.send(form);
  });
}

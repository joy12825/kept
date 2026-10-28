// 分享链接（阶段 18）：成员为一份文档生成带有效期的链接；拿到链接的人不用登录就能查看和下载。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';

export type ShareLink = components['schemas']['ShareLink'];
export type PublicShare = components['schemas']['PublicShare'];

export const shareUrl = (token: string) => `${window.location.origin}/s/${token}`;
export const publicFileUrl = (token: string, download = false) => `/api/v1/public/shares/${encodeURIComponent(token)}/file${download ? '?download=true' : ''}`;

const key = (docId: number) => ['share-links', docId] as const;

export function useShareLinks(docId: number, enabled = true) {
  return useQuery<ShareLink[], ApiError>({
    queryKey: key(docId),
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/share-links', { params: { path: { id: docId } } })),
    enabled,
  });
}

export function useCreateShareLink(docId: number) {
  const qc = useQueryClient();
  return useMutation<ShareLink, ApiError, { days: number; file: 'archive' | 'original'; watermark?: string; allowDownload?: boolean }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/documents/{id}/share-links', { params: { path: { id: docId } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: key(docId) }),
  });
}

export function useRevokeShareLink(docId: number) {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/share-links/{id}', { params: { path: { id } } })); },
    onSuccess: () => qc.invalidateQueries({ queryKey: key(docId) }),
  });
}

export function usePublicShare(token: string) {
  return useQuery<PublicShare, ApiError>({
    queryKey: ['public-share', token],
    queryFn: async () => unwrap(await api.GET('/api/v1/public/shares/{token}', { params: { path: { token } } })),
    retry: false,
  });
}

// 分享包与全部分享（v1.4.2）：多份文档一个链接；设置里集中查看、作废空间里所有有效的分享。
export type ActiveShare = components['schemas']['ActiveShare'];
export type PublicBundle = components['schemas']['PublicBundle'];

export const bundleUrl = (token: string) => `${window.location.origin}/b/${token}`;
export const activeShareUrl = (s: Pick<ActiveShare, 'kind' | 'token'>) => (s.kind === 'bundle' ? bundleUrl(s.token) : shareUrl(s.token));
export const publicBundleFileUrl = (token: string, docId: number, download = false) =>
  `/api/v1/public/bundles/${encodeURIComponent(token)}/documents/${docId}/file${download ? '?download=true' : ''}`;
export const publicBundleZipUrl = (token: string) => `/api/v1/public/bundles/${encodeURIComponent(token)}/zip`;

const activeKey = ['active-shares'] as const;

export function useActiveShares() {
  return useQuery<ActiveShare[], ApiError>({
    queryKey: activeKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/share-links')),
  });
}

export function useCreateShareBundle() {
  const qc = useQueryClient();
  return useMutation<ActiveShare, ApiError, { ids: number[]; title?: string; days: number; file: 'archive' | 'original'; watermark?: string; allowDownload?: boolean }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/share-bundles', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: activeKey }),
  });
}

/** 作废一个分享：单份文档的链接或分享包。 */
export function useRevokeShare() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, Pick<ActiveShare, 'kind' | 'id' | 'documentId'>>({
    mutationFn: async (s) => {
      if (s.kind === 'bundle') unwrap(await api.DELETE('/api/v1/share-bundles/{id}', { params: { path: { id: s.id } } }));
      else unwrap(await api.DELETE('/api/v1/share-links/{id}', { params: { path: { id: s.id } } }));
    },
    onSuccess: (_, s) => {
      void qc.invalidateQueries({ queryKey: activeKey });
      if (s.documentId) void qc.invalidateQueries({ queryKey: key(s.documentId) });
    },
  });
}

export function usePublicBundle(token: string) {
  return useQuery<PublicBundle, ApiError>({
    queryKey: ['public-bundle', token],
    queryFn: async () => unwrap(await api.GET('/api/v1/public/bundles/{token}', { params: { path: { token } } })),
    retry: false,
  });
}

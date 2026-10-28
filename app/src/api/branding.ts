import { useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';
import { t } from '../i18n';

export type Branding = components['schemas']['Branding'];

export const brandingKey = ['branding'] as const;
export const DEFAULT_BRANDING: Branding = { name: '收好' }; // i18n-ignore（服务端的默认名称）
export const MAX_LOGO_SIZE = 512 * 1024;
export const LOGO_TYPES = ['image/png', 'image/jpeg', 'image/webp'];

/** isDefault：还是默认名称（没改过名），这时用收好自己的标志（v1.13.6）。 */
export type BrandingView = Branding & { isDefault: boolean };

/** 收好自己的标志：图形和标签页图标（v1.13.6 品牌图标）。 */
export const BRAND_MARK = '/brand-mark.svg';
export const BRAND_FAVICON = '/icon-32.png';

/** 实例的产品名称与 Logo（公开接口，登录页也能取到）。取到之前用默认值，界面不闪空。 */
export function useBranding(): BrandingView {
  const q = useQuery<Branding, ApiError>({
    queryKey: brandingKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/branding')),
    staleTime: 5 * 60_000,
  });
  const b = q.data ?? DEFAULT_BRANDING;
  // 默认名称按界面语言（英文是 Kept）；管理员改过的照旧
  return b.name === DEFAULT_BRANDING.name ? { ...b, name: t('收好'), isDefault: true } : { ...b, isDefault: false };
}

/** 改过名、没有 Logo 时的图标：和侧边栏一样，主色底、名称的第一个字。 */
export function defaultIcon(name: string): string {
  const first = Array.from(name.trim())[0] ?? '';
  const ch = first && !/[<&>"']/.test(first) ? first : Array.from(t('收好'))[0];
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="18" fill="#2563EB"/><text x="32" y="33" font-size="34" font-weight="600" fill="#fff" text-anchor="middle" dominant-baseline="central" font-family="system-ui, sans-serif">${ch}</text></svg>`;
  return 'data:image/svg+xml,' + encodeURIComponent(svg);
}

/** 浏览器标签页的标题和图标跟随产品名称与 Logo。 */
export function useDocumentTitle() {
  const { name, logoUrl, isDefault } = useBranding();
  useEffect(() => { document.title = name; }, [name]);
  useEffect(() => {
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }
    link.href = logoUrl ?? (isDefault ? BRAND_FAVICON : defaultIcon(name));
  }, [name, logoUrl, isDefault]);
}

export function useUpdateBrandingName() {
  const qc = useQueryClient();
  return useMutation<Branding, ApiError, string>({
    mutationFn: async (name) => unwrap(await api.PATCH('/api/v1/admin/branding', { body: { name } })),
    onSuccess: (b) => qc.setQueryData(brandingKey, b),
  });
}

/** 上传 Logo：请求体就是图片本身。 */
export function useUploadLogo() {
  const qc = useQueryClient();
  return useMutation<Branding, ApiError, File>({
    mutationFn: async (file) => {
      const bytes = await readBytes(file);
      return unwrap(await api.PUT('/api/v1/admin/branding/logo', {
        body: bytes as never,
        bodySerializer: (b: unknown) => b as ArrayBuffer,
        headers: { 'Content-Type': file.type },
      }));
    },
    onSuccess: (b) => qc.setQueryData(brandingKey, b),
  });
}

export function useResetLogo() {
  const qc = useQueryClient();
  return useMutation<Branding, ApiError, void>({
    mutationFn: async () => unwrap(await api.DELETE('/api/v1/admin/branding/logo')),
    onSuccess: (b) => qc.setQueryData(brandingKey, b),
  });
}

/** 读出文件内容。用 FileReader 而不是 file.arrayBuffer()：后者在部分环境（如测试用的 jsdom）里不可用。 */
export function readBytes(file: Blob): Promise<ArrayBuffer> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(r.result as ArrayBuffer);
    r.onerror = () => reject(r.error);
    r.readAsArrayBuffer(file);
  });
}

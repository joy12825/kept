// 书籍与书房（阶段 23）：文档的书目、开启 / 关闭书房。
import { useMutation } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import { readBytes } from './branding';
import { fileUrl } from './documents';
import { useRefreshDrawers } from './drawers';
import type { components } from './schema';

export type Book = components['schemas']['Book'];
export type BookInput = components['schemas']['BookInput'];
export type BookLookup = components['schemas']['BookLookup'];
export type BookLookupResult = components['schemas']['BookLookupResult'];
export type BookSource = components['schemas']['BookSource'];

/** 封面地址；书目的修改时间作为版本，换了封面浏览器会重新取。 */
export const coverUrl = (id: number, updatedAt?: string) => fileUrl(id, 'cover', updatedAt ? String(Date.parse(updatedAt)) : undefined);

/** 按 ISBN 在线查找书目。 */
export function useLookupBook() {
  return useMutation<BookLookup, ApiError, string>({
    mutationFn: async (isbn) => unwrap(await api.GET('/api/v1/books/lookup', { params: { query: { isbn } } })),
  });
}

/** 从查找结果保存封面。 */
export function useFetchCover(id: number) {
  const refresh = useRefreshDrawers();
  return useMutation<Book, ApiError, { isbn: string; source: BookSource }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/documents/{id}/book/cover/fetch', { params: { path: { id } }, body })),
    onSuccess: refresh,
  });
}

/** 上传封面：请求体就是图片本身。 */
export function useUploadCover(id: number) {
  const refresh = useRefreshDrawers();
  return useMutation<Book, ApiError, File>({
    mutationFn: async (file) => {
      const bytes = await readBytes(file);
      return unwrap(await api.PUT('/api/v1/documents/{id}/book/cover', {
        params: { path: { id } },
        body: bytes as never,
        bodySerializer: (b: unknown) => b as ArrayBuffer,
        headers: { 'Content-Type': file.type || 'application/octet-stream' },
      }));
    },
    onSuccess: refresh,
  });
}

export function useDeleteCover(id: number) {
  const refresh = useRefreshDrawers();
  return useMutation<Book, ApiError, void>({
    mutationFn: async () => unwrap(await api.DELETE('/api/v1/documents/{id}/book/cover', { params: { path: { id } } })),
    onSuccess: refresh,
  });
}

/** 设为书籍或修改书目；书房的规则会把它收进来，所以抽屉和列表一起刷新。 */
export function useSetBook(id: number) {
  const refresh = useRefreshDrawers();
  return useMutation<Book, ApiError, BookInput>({
    mutationFn: async (body) => unwrap(await api.PUT('/api/v1/documents/{id}/book', { params: { path: { id } }, body })),
    onSuccess: refresh,
  });
}

export function useDeleteBook(id: number) {
  const refresh = useRefreshDrawers();
  return useMutation<void, ApiError, void>({
    mutationFn: async () => { unwrap(await api.DELETE('/api/v1/documents/{id}/book', { params: { path: { id } } })); },
    onSuccess: refresh,
  });
}

export function useEnableLibrary() {
  const refresh = useRefreshDrawers();
  return useMutation<components['schemas']['DrawerSaved'], ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/library')),
    onSuccess: refresh,
  });
}

export function useDisableLibrary() {
  const refresh = useRefreshDrawers();
  return useMutation<components['schemas']['JobStarted'], ApiError, void>({
    mutationFn: async () => unwrap(await api.DELETE('/api/v1/library')),
    onSuccess: refresh,
  });
}

/** 13 位 ISBN 按 978-7-xxxx 的习惯不好分段，只在前缀后加一个连字符便于阅读。 */
export function formatIsbn(isbn: string): string {
  return isbn.length === 13 ? `${isbn.slice(0, 3)}-${isbn.slice(3)}` : isbn;
}

/** 本地校验（与服务端一致）：10 位或 13 位，校验位要对。空串算有效。 */
export function isbnValid(raw: string): boolean {
  const d = raw.toUpperCase().replace(/[\s\-‐–]/g, '');
  if (d === '') return true;
  if (/^\d{9}[\dX]$/.test(d)) {
    let sum = 0;
    for (let i = 0; i < 10; i++) sum += (10 - i) * (d[i] === 'X' ? 10 : Number(d[i]));
    return sum % 11 === 0;
  }
  if (/^97[89]\d{10}$/.test(d)) {
    let sum = 0;
    for (let i = 0; i < 12; i++) sum += Number(d[i]) * (i % 2 ? 3 : 1);
    return (10 - (sum % 10)) % 10 === Number(d[12]);
  }
  return false;
}

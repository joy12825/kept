// 表格和压缩包（v1.9.3）：Excel、CSV 的单元格；压缩包里的文件和导入。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, getWorkspaceId, unwrap } from './client';
import { inboxKey, jobsKey } from './documents';
import type { operations } from './schema';

type Ok<T> = T extends { responses: { 200: { content: { 'application/json': infer R } } } } ? R : never;
export type Sheets = Ok<operations['getDocumentSheets']>;
export type Sheet = Sheets['sheets'][number];
export type ZipEntries = Ok<operations['listZipEntries']>;
export type WordDocument = Ok<operations['getDocumentWord']>;
export type WordBlock = WordDocument['blocks'][number];
export type WordRun = NonNullable<WordBlock['runs']>[number];
export type ZipEntry = ZipEntries['entries'][number];
export type ImportResult = Ok<operations['importZipEntries']>['results'][number];

export function useSheets(docId: number, enabled = true) {
  return useQuery<Sheets, ApiError>({
    queryKey: ['document-sheets', docId],
    enabled,
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/sheets', { params: { path: { id: docId } } })),
  });
}

/** Word 阅读视图（v1.9.8） */
export function useWord(docId: number, enabled = true) {
  return useQuery<WordDocument, ApiError>({
    queryKey: ['document-word', docId],
    enabled,
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/word', { params: { path: { id: docId } } })),
  });
}

/** 阅读视图里第 n 张图片的地址（带上当前空间，浏览器直接取） */
export function wordImageUrl(docId: number, n: number) {
  const ws = getWorkspaceId();
  return `/api/v1/documents/${docId}/word/images/${n}${ws !== null ? `?workspace=${ws}` : ''}`;
}

/** OFD 的电子签章（v1.11.3）：服务器读原件现算 */
export type OFDSignature = Ok<operations['getDocumentSignatures']>['items'][number];
export function useSignatures(docId: number, enabled = true) {
  return useQuery<OFDSignature[], ApiError>({
    queryKey: ['document-signatures', docId],
    enabled,
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/signatures', { params: { path: { id: docId } } })).items,
  });
}

/** 第 n 个签章的印章图片 */
export function sealImageUrl(docId: number, n: number) {
  const ws = getWorkspaceId();
  return `/api/v1/documents/${docId}/signatures/${n}/seal${ws !== null ? `?workspace=${ws}` : ''}`;
}

export function useZipEntries(docId: number, enabled = true) {
  return useQuery<ZipEntries, ApiError>({
    queryKey: ['document-entries', docId],
    enabled,
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/entries', { params: { path: { id: docId } } })),
  });
}

export function useImportZipEntries(docId: number) {
  const qc = useQueryClient();
  return useMutation<ImportResult[], ApiError, string[]>({
    mutationFn: async (paths) => unwrap(await api.POST('/api/v1/documents/{id}/entries/import', { params: { path: { id: docId } }, body: { paths } })).results,
    onSuccess: () => { void qc.invalidateQueries({ queryKey: jobsKey }); void qc.invalidateQueries({ queryKey: inboxKey }); },
  });
}

// 存储与配额（v1.0 阶段 36）：当前空间的用量；实例管理员看全部空间并设置配额。
// 存储位置（v1.7.0）：实例的各个存储，空间的新文档写到哪里、已有文档移过去的进度。
import { useEffect, useRef } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';

export type StorageUsage = components['schemas']['StorageUsage'];
export type AdminWorkspace = components['schemas']['AdminWorkspace'];
export type QuotaMode = 'default' | 'unlimited' | 'limit';
export type AdminStorage = components['schemas']['AdminStorage'];
export type WorkspaceStorage = components['schemas']['WorkspaceStorage'];
export type StorageTest = { ok: boolean; tookMs: number; error?: string };

export const storageKey = ['storage'] as const;
const adminWorkspacesKey = ['admin-workspaces'] as const;
const adminStoragesKey = ['admin-storages'] as const;

/** 每份文档最多保留几个旧版本（v1.7.13，空间管理员）；0 为全部保留。 */
export function useSetKeepVersions() {
  const qc = useQueryClient();
  return useMutation<unknown, ApiError, number>({
    mutationFn: async (keep) => unwrap(await api.PUT('/api/v1/storage/versions', { body: { keep } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: storageKey }),
  });
}

export function useStorage() {
  return useQuery<StorageUsage, ApiError>({
    queryKey: storageKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/storage')),
    staleTime: 60_000,
  });
}

export function useAdminWorkspaces(enabled = true) {
  const qc = useQueryClient();
  const moving = useRef(false);
  const q = useQuery<{ items: AdminWorkspace[]; defaultQuota: number }, ApiError>({
    queryKey: adminWorkspacesKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/admin/workspaces')),
    enabled,
    // 有空间在移动文档时，每 3 秒看一次进度
    refetchInterval: (q) => (q.state.data?.items.some((w) => w.storage.moving) ? 3000 : false),
  });
  // 移动结束后，各存储的文档数也变了
  const now = !!q.data?.items.some((w) => w.storage.moving);
  useEffect(() => {
    if (moving.current && !now) void qc.invalidateQueries({ queryKey: adminStoragesKey });
    moving.current = now;
  }, [now, qc]);
  return q;
}

export function useAdminStorages() {
  return useQuery<AdminStorage[], ApiError>({
    queryKey: adminStoragesKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/admin/storages')).items,
    // 有存储正在转换加密时，每 3 秒看一次进度（v1.11.0）
    refetchInterval: (q) => (q.state.data?.some((s) => s.encryption?.converting) ? 3000 : false),
  });
}

// ---------- 存储加密（v1.11.0） ----------

export type EncryptionStatus = components['schemas']['EncryptionStatus'];
const encryptionKey = ['storage-encryption'] as const;

/** 恢复码是否生成、哪些存储的钥匙解不开。 */
export function useEncryption() {
  return useQuery<EncryptionStatus, ApiError>({
    queryKey: encryptionKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/admin/storage-encryption')),
  });
}

/** 开关存储的加密；第一次开启时返回恢复码（只这一次）。 */
export function useSetStorageEncryption() {
  const qc = useQueryClient();
  return useMutation<{ recoveryCode?: string; jobId?: number }, ApiError, { name: string; enabled: boolean }>({
    mutationFn: async ({ name, enabled }) => unwrap(await api.PUT('/api/v1/admin/storages/{name}/encryption', { params: { path: { name } }, body: { enabled } })),
    onSuccess: () => { invalidateStorages(qc); void qc.invalidateQueries({ queryKey: encryptionKey }); },
  });
}

/** 重新生成恢复码，旧的作废。 */
export function useNewRecoveryCode() {
  const qc = useQueryClient();
  return useMutation<string, ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/admin/storage-encryption/recovery-code')).recoveryCode,
    onSuccess: () => { void qc.invalidateQueries({ queryKey: encryptionKey }); },
  });
}

/** 用恢复码解开钥匙解不开的存储。 */
export function useRecoverEncryption() {
  const qc = useQueryClient();
  return useMutation<number, ApiError, string>({
    mutationFn: async (code) => unwrap(await api.POST('/api/v1/admin/storage-encryption/recover', { body: { code } })).recovered,
    onSuccess: () => { invalidateStorages(qc); void qc.invalidateQueries({ queryKey: encryptionKey }); },
  });
}

/** 写入一个小文件、读回、删除，看存储能不能用。 */
export function useTestStorage() {
  return useMutation<StorageTest, ApiError, string>({
    mutationFn: async (name) => unwrap(await api.POST('/api/v1/admin/storages/{name}/test', { params: { path: { name } } })),
  });
}

/** 空间的新文档写到 storage；move 时把已有文档也移过去。 */
export function useSetWorkspaceStorage() {
  const qc = useQueryClient();
  return useMutation<{ jobId?: number }, ApiError, { id: number; storage: string; move: boolean }>({
    mutationFn: async ({ id, storage, move }) =>
      unwrap(await api.PUT('/api/v1/admin/workspaces/{id}/storage', { params: { path: { id } }, body: { storage, move } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: adminWorkspacesKey });
      void qc.invalidateQueries({ queryKey: adminStoragesKey });
    },
  });
}

export function useSetQuota() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: number; mode: QuotaMode; bytes?: number }>({
    mutationFn: async ({ id, mode, bytes }) => {
      unwrap(await api.PUT('/api/v1/admin/workspaces/{id}/quota', { params: { path: { id } }, body: { mode, bytes } }));
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: adminWorkspacesKey });
      void qc.invalidateQueries({ queryKey: storageKey });
    },
  });
}

/** 用量占配额的比例（没有配额时为 null）。 */
export const quotaShare = (u: StorageUsage) => (u.quota > 0 ? u.total / u.quota : null);

// ---- 页面上配置存储（v1.7.2）----

export type StorageInput = components['schemas']['StorageInput'];
export type StorageChoice = components['schemas']['StorageChoice'];

const invalidateStorages = (qc: ReturnType<typeof useQueryClient>) => {
  void qc.invalidateQueries({ queryKey: adminStoragesKey });
  void qc.invalidateQueries({ queryKey: adminWorkspacesKey });
  void qc.invalidateQueries({ queryKey: storageLocationKey });
  void qc.invalidateQueries({ queryKey: ['tasks'] });
};

export function useSaveStorage() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { name?: string; input: StorageInput }>({
    mutationFn: async ({ name, input }) => {
      if (name) unwrap(await api.PUT('/api/v1/admin/storages/{name}', { params: { path: { name } }, body: input }));
      else unwrap(await api.POST('/api/v1/admin/storages', { body: input }));
    },
    onSuccess: () => invalidateStorages(qc),
  });
}

export function useDeleteStorage() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: async (name) => { unwrap(await api.DELETE('/api/v1/admin/storages/{name}', { params: { path: { name } } })); },
    onSuccess: () => invalidateStorages(qc),
  });
}

/** 测试还没保存的设置；existing 是正在修改的存储（没填 Secret Key 时用它保存的）。 */
export function useTryStorage() {
  return useMutation<StorageTest, ApiError, { existing?: string; storage: StorageInput }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/admin/storage-test', { body })),
  });
}

const storageLocationKey = ['storage-location'] as const;

/** 当前空间的文档放在哪里、可以换到哪些存储（空间管理员）。 */
export function useStorageLocation(enabled = true) {
  return useQuery<{ current: WorkspaceStorage; choices: StorageChoice[] }, ApiError>({
    queryKey: storageLocationKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/storage/location')),
    enabled,
    refetchInterval: (q) => (q.state.data?.current.moving ? 3000 : false),
  });
}

export function useSetStorageLocation() {
  const qc = useQueryClient();
  return useMutation<{ jobId?: number }, ApiError, { storage: string; move: boolean }>({
    mutationFn: async (body) => unwrap(await api.PUT('/api/v1/storage/location', { body })),
    onSuccess: () => invalidateStorages(qc),
  });
}

// ---- 备份到存储（v1.7.6）----

export type BackupTarget = components['schemas']['BackupTarget'];
export type BackupRun = components['schemas']['BackupRun'];
const backupTargetKey = ['backup-target'] as const;

export function useBackupTarget() {
  return useQuery<BackupTarget, ApiError>({
    queryKey: backupTargetKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/admin/backup-target')),
    // 备份在排队或进行时，每 3 秒看一次
    refetchInterval: (q) => (q.state.data?.last && (q.state.data.last.status === 'queued' || q.state.data.last.status === 'running') ? 3000 : false),
  });
}

export function useSetBackupTarget() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { storage: string; keep: number }>({
    mutationFn: async (body) => { unwrap(await api.PUT('/api/v1/admin/backup-target', { body })); },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: backupTargetKey });
      invalidateStorages(qc);
    },
  });
}

export function useRunBackupToStorage() {
  const qc = useQueryClient();
  return useMutation<{ jobId: number }, ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/admin/backup-target/run')),
    onSuccess: () => void qc.invalidateQueries({ queryKey: backupTargetKey }),
  });
}

// ---- 远程收件夹（v1.7.7）----

export type RemoteInbox = components['schemas']['RemoteInbox'];
const remoteInboxKey = ['remote-inbox'] as const;

export function useRemoteInbox() {
  return useQuery<RemoteInbox, ApiError>({
    queryKey: remoteInboxKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/remote-inbox')),
    // 有文件在等待或正在入库时，半分钟看一次
    refetchInterval: (q) => (q.state.data && ((q.state.data.counts.waiting ?? 0) + (q.state.data.counts.queued ?? 0) + (q.state.data.counts.changed ?? 0) > 0) ? 30_000 : false),
  });
}

export function useSetRemoteInbox() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { storage: string; folder: string; move: boolean; enabled: boolean }>({
    mutationFn: async (body) => { unwrap(await api.PUT('/api/v1/remote-inbox', { body })); },
    onSuccess: () => void qc.invalidateQueries({ queryKey: remoteInboxKey }),
  });
}

export function useScanRemoteInbox() {
  const qc = useQueryClient();
  return useMutation<RemoteInbox, ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/remote-inbox/scan')),
    onSuccess: (data) => qc.setQueryData(remoteInboxKey, data),
  });
}

// ---- 冷存储（v1.7.8）----

/** 当前空间：超过 months 个月的文档移到 storage（空为不移）。 */
export function useSetStorageCold() {
  const qc = useQueryClient();
  return useMutation<{ jobId?: number }, ApiError, { storage: string; months: number }>({
    mutationFn: async (body) => unwrap(await api.PUT('/api/v1/storage/cold', { body })),
    onSuccess: () => invalidateStorages(qc),
  });
}

/** 实例管理员给某个空间设置冷存储。 */
export function useAdminSetCold() {
  const qc = useQueryClient();
  return useMutation<{ jobId?: number }, ApiError, { id: number; storage: string; months: number }>({
    mutationFn: async ({ id, storage, months }) =>
      unwrap(await api.PUT('/api/v1/admin/workspaces/{id}/cold', { params: { path: { id } }, body: { storage, months } })),
    onSuccess: () => invalidateStorages(qc),
  });
}

// ---- 可读副本（v1.7.9）----

export type ExportTarget = components['schemas']['ExportTarget'];
const exportTargetKey = ['export-target'] as const;

export function useExportTarget() {
  return useQuery<ExportTarget, ApiError>({
    queryKey: exportTargetKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/admin/export-target')),
    refetchInterval: (q) => (q.state.data?.runs.some((r) => r.status === 'queued' || r.status === 'running') ? 3000 : false),
  });
}

export function useSetExportTarget() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { storage: string; folder: string; sensitive: boolean }>({
    mutationFn: async (body) => { unwrap(await api.PUT('/api/v1/admin/export-target', { body })); },
    onSuccess: () => void qc.invalidateQueries({ queryKey: exportTargetKey }),
  });
}

export function useRunExportToStorage() {
  const qc = useQueryClient();
  return useMutation<{ jobs: number }, ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/admin/export-target/run')),
    onSuccess: () => void qc.invalidateQueries({ queryKey: exportTargetKey }),
  });
}

/** 给备份目标（S3 兼容存储）的桶开启版本控制（v1.7.12）。 */
export function useEnableBackupVersioning() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, void>({
    mutationFn: async () => { unwrap(await api.POST('/api/v1/admin/backup-target/versioning')); },
    onSuccess: () => void qc.invalidateQueries({ queryKey: backupTargetKey }),
  });
}

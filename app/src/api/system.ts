import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';

export type Task = components['schemas']['Task'];
export type TaskStatus = Task['status'];
export type TaskGroup = components['schemas']['TaskGroup'];
/** 任务记录的筛选（v1.7.14）：active 为排队中或进行中；mine 只看自己发起的。 */
export interface TaskFilter { status?: TaskStatus | 'active'; days?: number; group?: TaskGroup; mine?: boolean; limit?: number }
const isActive = (t: Task) => t.status === 'queued' || t.status === 'running';
export type LogEntry = components['schemas']['LogEntry'];
export type SystemStatus = components['schemas']['SystemStatus'];

/** 当前空间最近的后台任务（设置 → 任务记录）。有排队或进行中的任务时每 5 秒刷新。 */
export function useTasks(workspaceId: number | undefined, filter: TaskFilter) {
  return useQuery<Task[], ApiError>({
    queryKey: ['tasks', workspaceId, filter],
    enabled: !!workspaceId,
    queryFn: async () => unwrap(await api.GET('/api/v1/tasks', { params: { query: filter } })),
    refetchInterval: (q) => (q.state.data?.some(isActive) ? 5_000 : false),
  });
}

/** 顶栏的任务面板（v1.7.14）：最近 7 天有人发起的 20 个任务（v1.7.16 起不含定时检查）。有进行中的每 3 秒刷新，否则每 30 秒看一次有没有新任务。 */
export function useRecentTasks(workspaceId: number | undefined) {
  return useQuery<Task[], ApiError>({
    queryKey: ['tasks', workspaceId, 'recent'],
    enabled: !!workspaceId,
    queryFn: async () => unwrap(await api.GET('/api/v1/tasks', { params: { query: { days: 7, limit: 20, people: true } } })),
    refetchInterval: (q) => (q.state.data?.some(isActive) ? 3_000 : 30_000),
  });
}

export const activeTasks = (tasks: Task[]) => tasks.filter(isActive);

/** 系统状态（实例管理员）。备份在排队或进行中时每 2 秒刷新。 */
export function useSystemStatus() {
  return useQuery<SystemStatus, ApiError>({
    queryKey: ['admin', 'system'],
    queryFn: async () => unwrap(await api.GET('/api/v1/admin/system')),
    refetchInterval: (q) => (['queued', 'running'].includes(q.state.data?.backup.last?.status ?? '') ? 2_000 : false),
  });
}

/** 立即备份到 KEPT_BACKUP_DIR（实例管理员）。 */
export function useRunBackup() {
  const qc = useQueryClient();
  return useMutation<{ jobId: number }, ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/admin/backup')),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'system'] }),
  });
}

/** 每天备份的目录（v1.13.9，实例管理员，各版本都有）：选服务器上的一个目录，空字符串表示停止。 */
export function useSetBackupDir() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: async (dir) => { unwrap(await api.PUT('/api/v1/admin/backup', { body: { dir } })); },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['admin', 'system'] });
      void qc.invalidateQueries({ queryKey: ['backup-target'] });
    },
  });
}

/** 监听目录的设置（v1.7.13，实例管理员）：同一路径再次放入的文件作为新版本。 */
export function useSetWatch() {
  const qc = useQueryClient();
  return useMutation<unknown, ApiError, { versions: boolean }>({
    mutationFn: async (body) => unwrap(await api.PUT('/api/v1/admin/watch', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'system'] }),
  });
}

export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

/** 最近的日志（实例管理员），从新到旧。 */
export function useLogs(filter: { level?: LogLevel; q?: string }) {
  return useQuery<LogEntry[], ApiError>({
    queryKey: ['admin', 'logs', filter],
    queryFn: async () => unwrap(await api.GET('/api/v1/admin/logs', { params: { query: { ...filter, limit: 500 } } })),
  });
}

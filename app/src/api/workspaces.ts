import { useCallback } from 'react';
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { api, ApiError, setWorkspaceId, unwrap, type Workspace, type WorkspaceColor, type WorkspaceRole } from './client';
import { meKey, setupKey, useMe } from './session';

export const workspacesKey = ['workspaces'] as const;

/** 与当前空间无关的查询：切换空间时保留，其余全部丢弃。 */
const ACCOUNT_LEVEL = new Set<unknown>([meKey[0], setupKey[0], workspacesKey[0], 'healthz', 'system-version', 'branding']);

/** 丢弃上一个空间的数据，并重新取当前用户（其中带当前空间和角色）。 */
export async function reloadForWorkspace(qc: QueryClient) {
  qc.removeQueries({ predicate: (q) => !ACCOUNT_LEVEL.has(q.queryKey[0]) });
  await Promise.all([qc.invalidateQueries({ queryKey: meKey }), qc.invalidateQueries({ queryKey: workspacesKey })]);
}

/** 我所在的空间：个人空间排第一，其余按创建时间。 */
export function useWorkspaces() {
  return useQuery<Workspace[], ApiError>({
    queryKey: workspacesKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/workspaces')),
  });
}

/** 切换当前空间。个人空间不带请求头，所以保存为 null。 */
export function useSwitchWorkspace() {
  const qc = useQueryClient();
  return useCallback(async (ws: Pick<Workspace, 'id' | 'kind'>) => {
    setWorkspaceId(ws.kind === 'personal' ? null : ws.id);
    await reloadForWorkspace(qc);
  }, [qc]);
}

export function useCreateWorkspace() {
  const qc = useQueryClient();
  return useMutation<Workspace, ApiError, { name: string; kind: 'family' | 'team'; color?: WorkspaceColor }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/workspaces', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: workspacesKey }),
  });
}

export function useUpdateWorkspace() {
  const qc = useQueryClient();
  return useMutation<Workspace, ApiError, { id: number; name?: string; color?: WorkspaceColor }>({
    mutationFn: async ({ id, ...body }) => unwrap(await api.PATCH('/api/v1/workspaces/{id}', { params: { path: { id } }, body })),
    onSuccess: () => Promise.all([qc.invalidateQueries({ queryKey: workspacesKey }), qc.invalidateQueries({ queryKey: meKey })]),
  });
}

/** 删除空间；name 必须是空间的名称，用来确认。 */
export function useDeleteWorkspace() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: number; name: string }>({
    mutationFn: async ({ id, name }) => { unwrap(await api.DELETE('/api/v1/workspaces/{id}', { params: { path: { id } }, body: { name } })); },
    onSuccess: () => qc.invalidateQueries({ queryKey: workspacesKey }),
  });
}

export interface RoleInfo {
  role: WorkspaceRole | undefined;
  /** 成员或管理员：可以上传、整理、修改。只看的成员为 false。 */
  canWrite: boolean;
  /** 空间管理员：可以管理成员、空间设置，查看活动记录。 */
  isOwner: boolean;
}

/** 当前用户在当前空间的角色。每个接入真实接口的页面都应据此隐藏或禁用只看成员的修改入口。 */
export function useRole(): RoleInfo {
  const role = useMe().data?.workspace.role;
  return { role, canWrite: role === 'owner' || role === 'editor', isOwner: role === 'owner' };
}

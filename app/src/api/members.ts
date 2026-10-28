import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap, type WorkspaceRole } from './client';
import type { components } from './schema';
import { meKey } from './session';
import { workspacesKey } from './workspaces';
import { t, tc } from '../i18n';

export type Member = components['schemas']['Member'];

export const membersKey = (workspaceId: number) => ['members', workspaceId] as const;

/** 界面上的角色名称。 */
export const ROLE_LABEL: Record<WorkspaceRole, string> = { owner: tc('角色', '管理员'), editor: tc('角色', '成员'), viewer: tc('角色', '只看') };
export const ROLES: WorkspaceRole[] = ['owner', 'editor', 'viewer'];

/** 空间的成员，按加入时间排序。空间的任何成员都可以查看。 */
export function useMembers(workspaceId: number | undefined) {
  return useQuery<Member[], ApiError>({
    queryKey: membersKey(workspaceId ?? 0),
    queryFn: async () => unwrap(await api.GET('/api/v1/workspaces/{id}/members', { params: { path: { id: workspaceId! } } })),
    enabled: workspaceId !== undefined,
  });
}

/** 成员变动后，成员列表、空间列表里的人数、当前空间的人数都要重新取。 */
function useRefreshMembers(workspaceId: number) {
  const qc = useQueryClient();
  return () => Promise.all([
    qc.invalidateQueries({ queryKey: membersKey(workspaceId) }),
    qc.invalidateQueries({ queryKey: workspacesKey }),
    qc.invalidateQueries({ queryKey: meKey }),
  ]);
}

/** 按用户名或邮箱添加已有的账号。没有邀请链接：账号需要先由管理员创建。 */
export function useAddMember(workspaceId: number) {
  const refresh = useRefreshMembers(workspaceId);
  return useMutation<Member, ApiError, { login: string; role: WorkspaceRole }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/workspaces/{id}/members', { params: { path: { id: workspaceId } }, body })),
    onSuccess: refresh,
  });
}

export function useUpdateMember(workspaceId: number) {
  const refresh = useRefreshMembers(workspaceId);
  return useMutation<Member, ApiError, { userId: number; role: WorkspaceRole }>({
    mutationFn: async ({ userId, role }) => unwrap(await api.PATCH('/api/v1/workspaces/{id}/members/{userId}', {
      params: { path: { id: workspaceId, userId } }, body: { role },
    })),
    onSuccess: refresh,
  });
}

/** 移除成员；userId 是自己时即退出空间（退出后由调用方切回个人空间，这里不再刷新已无权访问的列表）。 */
export function useRemoveMember(workspaceId: number) {
  const refresh = useRefreshMembers(workspaceId);
  return useMutation<void, ApiError, { userId: number; self?: boolean }>({
    mutationFn: async ({ userId }) => {
      unwrap(await api.DELETE('/api/v1/workspaces/{id}/members/{userId}', { params: { path: { id: workspaceId, userId } } }));
    },
    onSuccess: (_data, vars) => (vars.self ? undefined : refresh()),
  });
}

// ---- 成员的上传额度（v1.7.11）----

export type MemberUpload = { userId: number; uploaded: number; quota: number };
const memberUploadsKey = ['member-uploads'] as const;

/** 各成员在当前空间上传了多少、额度多少（空间管理员）。 */
export function useMemberUploads(enabled: boolean) {
  return useQuery<MemberUpload[], ApiError>({
    queryKey: memberUploadsKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/members/uploads')),
    enabled,
  });
}

/** 设置成员的上传额度；0 为不单独限制。 */
export function useSetMemberUploadQuota() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { userId: number; bytes: number }>({
    mutationFn: async ({ userId, bytes }) => {
      unwrap(await api.PUT('/api/v1/members/{userId}/upload-quota', { params: { path: { userId } }, body: { bytes } }));
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: memberUploadsKey }),
  });
}

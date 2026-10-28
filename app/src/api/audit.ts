import { useInfiniteQuery } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';

export type AuditEvent = components['schemas']['AuditEvent'];
export type AuditPage = components['schemas']['AuditPage'];

export interface AuditFilter {
  /** 操作类型的前缀，例如 member. */
  action?: string;
  actorId?: number;
  /** RFC 3339；只看这个时间之后的记录 */
  from?: string;
}

/** workspace：当前空间的活动记录（空间管理员）；instance：账号管理类记录（实例管理员）。 */
export type AuditScope = 'workspace' | 'instance';

const PAGE_SIZE = 50;

export function useAudit(scope: AuditScope, filter: AuditFilter) {
  return useInfiniteQuery<AuditPage, ApiError>({
    queryKey: ['audit', scope, filter],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const query = { ...filter, limit: PAGE_SIZE, cursor: pageParam as string | undefined };
      return scope === 'workspace'
        ? unwrap(await api.GET('/api/v1/audit', { params: { query } }))
        : unwrap(await api.GET('/api/v1/admin/audit', { params: { query } }));
    },
    getNextPageParam: (last) => last.nextCursor,
  });
}

import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { getWorkspaceId } from './client';
import { useMe } from './session';
import { inboxKey, jobsKey, type Job } from './documents';

type JobEvent = Pick<Job, 'id' | 'stage' | 'progress' | 'documentId' | 'error'> & { status: string };

/**
 * 订阅当前空间的实时事件（SSE）：任务进度写进任务列表的缓存；待整理有变化时重新取。
 * 断线后浏览器的 EventSource 会自动重连；重连时把相关数据重新取一遍，补上断线期间错过的事件。
 */
export function useEvents() {
  const qc = useQueryClient();
  const workspaceId = useMe().data?.workspace.id;

  useEffect(() => {
    if (workspaceId === undefined || typeof EventSource === 'undefined') return;
    const ws = getWorkspaceId();
    const es = new EventSource(`/api/v1/events${ws !== null ? `?workspace=${ws}` : ''}`, { withCredentials: true });
    let opened = false;
    es.onopen = () => {
      if (opened) {
        void qc.invalidateQueries({ queryKey: jobsKey });
        void qc.invalidateQueries({ queryKey: inboxKey });
      }
      opened = true;
    };
    es.addEventListener('job.updated', (e) => {
      const ev = JSON.parse((e as MessageEvent).data) as JobEvent;
      let known = false;
      qc.setQueryData<Job[]>(jobsKey, (old) => old?.map((j) => {
        if (j.id !== ev.id) return j;
        known = true;
        const status = ev.status === 'retrying' ? 'queued' : (ev.status as Job['status']);
        return { ...j, status, stage: ev.stage || j.stage, progress: ev.status === 'succeeded' ? 100 : ev.progress, documentId: ev.documentId ?? j.documentId, error: ev.error ?? j.error };
      }));
      if (!known) void qc.invalidateQueries({ queryKey: jobsKey });
    });
    const drawers = () => { void qc.invalidateQueries({ queryKey: ['drawers'] }); void qc.invalidateQueries({ queryKey: ['yearbook'] }); };
    // 新文档入库也会发这个事件：打开着的列表（包括书房的封面墙）一起刷新
    es.addEventListener('inbox.changed', () => { void qc.invalidateQueries({ queryKey: ['documents'] }); drawers(); });
    es.addEventListener('document.updated', () => { void qc.invalidateQueries({ queryKey: ['documents'] }); drawers(); });
    // 抽屉、规则、顺序变了（含后台重新计算归属之后）；提醒变了
    es.addEventListener('drawers.changed', () => { void qc.invalidateQueries({ queryKey: ['documents'] }); drawers(); });
    // 别的空间建、改、取消了共享给这里的共享集（v1.0）
    es.addEventListener('collections.changed', () => void qc.invalidateQueries({ queryKey: ['shared-collections'] }));
    es.addEventListener('proposals.changed', (e) => {
      const { documentId } = JSON.parse((e as MessageEvent).data || '{}') as { documentId?: number };
      void qc.invalidateQueries({ queryKey: documentId ? ['proposals', documentId] : ['proposals'] });
    });
    es.addEventListener('alerts.changed', () => { void qc.invalidateQueries({ queryKey: ['alerts'] }); void qc.invalidateQueries({ queryKey: ['drawers'] }); });
    return () => es.close();
  }, [qc, workspaceId]);
}

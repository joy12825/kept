// 工作流（阶段 17）：列表、新建、修改、删除、调整顺序、运行记录。只有空间管理员能看到和修改。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';
import { t, tc } from '../i18n';

export type Workflow = components['schemas']['Workflow'];
export type WorkflowInput = components['schemas']['WorkflowInput'];
export type WorkflowTrigger = components['schemas']['WorkflowTrigger'];
export type WorkflowAction = components['schemas']['WorkflowAction'];
export type WorkflowRun = components['schemas']['WorkflowRun'];
export type TriggerType = WorkflowTrigger['type'];
export type ActionType = WorkflowAction['type'];

export const workflowsKey = ['workflows'] as const;

export function useWorkflows() {
  return useQuery<Workflow[], ApiError>({
    queryKey: workflowsKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/workflows')),
  });
}

export function useSaveWorkflow() {
  const qc = useQueryClient();
  return useMutation<Workflow, ApiError, { id?: number; body: WorkflowInput }>({
    mutationFn: async ({ id, body }) => (id
      ? unwrap(await api.PUT('/api/v1/workflows/{id}', { params: { path: { id } }, body }))
      : unwrap(await api.POST('/api/v1/workflows', { body }))),
    onSuccess: () => qc.invalidateQueries({ queryKey: workflowsKey }),
  });
}

export function useDeleteWorkflow() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/workflows/{id}', { params: { path: { id } } })); },
    onSuccess: () => qc.invalidateQueries({ queryKey: workflowsKey }),
  });
}

export function useReorderWorkflows() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number[]>({
    mutationFn: async (ids) => { unwrap(await api.POST('/api/v1/workflows/reorder', { body: { ids } })); },
    onMutate: (ids) => {
      qc.setQueryData<Workflow[]>(workflowsKey, (old) => old && ids.map((id) => old.find((w) => w.id === id)!).filter(Boolean));
    },
    onSettled: () => qc.invalidateQueries({ queryKey: workflowsKey }),
  });
}

export function useWorkflowRuns(id: number | null) {
  return useQuery<WorkflowRun[], ApiError>({
    queryKey: ['workflow-runs', id],
    queryFn: async () => unwrap(await api.GET('/api/v1/workflows/{id}/runs', { params: { path: { id: id! } } })),
    enabled: id !== null,
  });
}

export const TRIGGER_LABEL: Record<TriggerType, string> = {
  consumption: t('入库时'), added: t('添加后'), updated: t('修改后'), scheduled: t('定时'),
};

export const TRIGGER_HELP: Record<TriggerType, string> = {
  consumption: t('文件入库、自动分类之前。只能按文件名、来源和关键词判断，只能设置标题、类型、往来方、标签和抽屉'),
  added: t('新文档入库完成后（已经自动分类、放进抽屉）'),
  updated: t('有人修改了文档的元数据后；工作流自己做的修改不会再触发'),
  scheduled: t('按文档的某个日期，到了指定的天数时运行'),
};

export const ACTION_LABEL: Record<ActionType, string> = {
  assign: tc('动作', '设置'), remove: t('移除'), email: t('发送邮件'), webhook: t('调用 Webhook'),
};

export const SOURCE_LABEL: Record<string, string> = {
  web: t('网页上传'), api: 'API', mobile: t('手机'), watch: t('监听目录'), mail: t('邮件收取'), link: t('上传链接'), import: t('导入'), remote: t('远程收件夹'),
};

/** 标题、主题、正文、请求体里可以用的占位符。 */
export const PLACEHOLDERS = ['{标题}', '{往来方}', '{家人}', '{类型}', '{日期}', '{标签}', '{原文件名}', '{添加时间}', '{链接}']; // i18n-ignore（服务端认的占位符）

/** 一句话描述触发方式，列表里用。 */
export function describeTriggers(ts: WorkflowTrigger[], dateFieldName: (f: string) => string): string {
  return ts.map((tr) => {
    if (tr.type !== 'scheduled') return TRIGGER_LABEL[tr.type];
    const days = tr.offsetDays ?? 0;
    const field = dateFieldName(tr.field ?? 'created');
    const when = days === 0 ? t('{field}当天', { field }) : days > 0 ? t('{field}后 {n} 天', { field, n: days }) : t('{field}前 {n} 天', { field, n: -days });
    return tr.repeatDays ? t('{when}，每 {n} 天重复', { when, n: tr.repeatDays }) : when;
  }).join(t('、'));
}

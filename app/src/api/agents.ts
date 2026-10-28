// AI 智能体（阶段 22）：空间的 AI 设置、活动记录、文档上的提案（识别员填入的内容与拆分建议）。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import { documentKey, documentsKey, jobsKey } from './documents';
import type { components } from './schema';
import { t } from '../i18n';

export type AgentSettings = components['schemas']['AgentSettings'];
export type AgentRun = components['schemas']['AgentRun'];
export type Proposal = components['schemas']['Proposal'];
export type OrganizeSuggestion = components['schemas']['OrganizeSuggestion'];
export type FactSuggestion = components['schemas']['FactSuggestion'];

export const agentSettingsKey = ['agent-settings'] as const;
export const proposalsKey = (docId: number) => ['proposals', docId] as const;
export const suggestionsKey = ['organize-suggestions'] as const;

export function useAgentSettings(enabled = true) {
  return useQuery<AgentSettings, ApiError>({
    queryKey: agentSettingsKey,
    enabled,
    queryFn: async () => unwrap(await api.GET('/api/v1/agents/settings')),
  });
}

export function useSetAgentSettings() {
  const qc = useQueryClient();
  return useMutation<AgentSettings, ApiError, { enabled: boolean; intake: boolean; steward?: boolean; narrative?: boolean; library?: boolean; auto?: AgentSettings['auto']; dailyLimit: number; cloudAcknowledged?: boolean }>({
    mutationFn: async (body) => unwrap(await api.PATCH('/api/v1/agents/settings', { body })),
    onSuccess: (s) => qc.setQueryData(agentSettingsKey, s),
  });
}

export function useAgentRuns(enabled = true) {
  return useQuery<AgentRun[], ApiError>({
    queryKey: ['agent-runs'],
    queryFn: async () => unwrap(await api.GET('/api/v1/agents/runs', { params: { query: { limit: 50 } } })),
    enabled,
  });
}

export function useTestAI() {
  return useMutation<{ ms: number }, ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/admin/ai/test')),
  });
}

export function useProposals(docId: number) {
  return useQuery<Proposal[], ApiError>({
    queryKey: proposalsKey(docId),
    queryFn: async () => unwrap(await api.GET('/api/v1/documents/{id}/proposals', { params: { path: { id: docId } } })),
  });
}

function useAfter(docId: number) {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: proposalsKey(docId) });
    void qc.invalidateQueries({ queryKey: documentKey(docId) });
    void qc.invalidateQueries({ queryKey: jobsKey });
  };
}

export function useDecideProposal(docId: number) {
  const done = useAfter(docId);
  return useMutation<void, ApiError, { id: number; action: 'accept' | 'reject' | 'revert' }>({
    mutationFn: async ({ id, action }) => {
      const path = { params: { path: { id } } };
      if (action === 'accept') unwrap(await api.POST('/api/v1/proposals/{id}/accept', path));
      else if (action === 'reject') unwrap(await api.POST('/api/v1/proposals/{id}/reject', path));
      else unwrap(await api.POST('/api/v1/proposals/{id}/revert', path));
    },
    onSuccess: done,
  });
}

/** 让 AI 整理选中的文档（阶段 31）：后台逐份给出修改建议。 */
export function useOrganizeDocuments() {
  const qc = useQueryClient();
  return useMutation<{ queued: number }, ApiError, number[]>({
    mutationFn: async (ids) => unwrap(await api.POST('/api/v1/agents/organize', { body: { ids } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: jobsKey }),
  });
}

/** 目录的整理建议：匹配规则与合并。 */
export function useOrganizeSuggestions(enabled = true) {
  return useQuery<OrganizeSuggestion[], ApiError>({
    queryKey: suggestionsKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/agents/suggestions')),
    enabled,
  });
}

export function useRefreshSuggestions() {
  return useMutation<void, ApiError, void>({
    mutationFn: async () => { unwrap(await api.POST('/api/v1/agents/suggestions/refresh')); },
  });
}

export function useDecideSuggestion() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: number; accept: boolean }>({
    mutationFn: async ({ id, accept }) => {
      const path = { params: { path: { id } } };
      if (accept) unwrap(await api.POST('/api/v1/proposals/{id}/accept', path));
      else unwrap(await api.POST('/api/v1/proposals/{id}/reject', path));
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: suggestionsKey });
      void qc.invalidateQueries({ queryKey: ['fact-suggestions'] });
      void qc.invalidateQueries({ queryKey: ['taxonomy'] });
      void qc.invalidateQueries({ queryKey: ['drawers'] });
      void qc.invalidateQueries({ queryKey: documentsKey });
    },
  });
}

/** 抽屉管家对一个抽屉的要点建议（阶段 32）。 */
export function useFactSuggestions(drawerId: number, enabled = true) {
  return useQuery<FactSuggestion[], ApiError>({
    queryKey: ['fact-suggestions', drawerId],
    queryFn: async () => unwrap(await api.GET('/api/v1/drawers/{id}/fact-suggestions', { params: { path: { id: drawerId } } })),
    enabled,
  });
}

/** 让 AI 为某一年写年度回顾叙述（后台进行）。 */
export function useWriteNarrative() {
  return useMutation<void, ApiError, number>({
    mutationFn: async (year) => { unwrap(await api.POST('/api/v1/yearbook/{year}/narrative', { params: { path: { year } } })); },
  });
}

/** 提案改了什么，一句话。names 把 ID 换成名称。 */
export function describeProposal(p: Proposal, names: { type: (id: number) => string; corr: (id: number) => string; tag: (id: number) => string; drawer: (id: number) => string; field: (id: number) => string }): string {
  const c = p.change;
  const pending = p.status === 'pending';
  switch (p.kind) {
    case 'set_fields': {
      const label: Record<string, string> = { title: t('标题'), type: t('类型'), correspondent: t('往来方'), date: t('日期') };
      const value = (k: string, v: unknown) => (v == null || v === '' || v === 0 ? '' : k === 'type' ? names.type(v as number) : k === 'correspondent' ? names.corr(v as number) : String(v));
      const parts = Object.entries(c.fields ?? {}).map(([k, v]) => {
        const [before, after] = v as unknown[];
        const name = label[k] ?? (k.startsWith('cf:') ? names.field(Number(k.slice(3))) : k);
        const b = value(k, before);
        return pending && b ? t('{name}“{before}”改为“{after}”', { name, before: b, after: value(k, after) }) : t('{name}“{value}”', { name, value: value(k, after) });
      });
      return pending ? t('建议{list}', { list: parts.join(t('、')) }) : t('填入了{list}', { list: parts.join(t('、')) });
    }
    case 'add_tags': {
      const list = (c.tagIds ?? []).map((id) => `“${names.tag(id)}”`).join(t('、'));
      return pending ? t('建议加上标签{list}', { list }) : t('加上了标签{list}', { list });
    }
    case 'set_drawer': {
      const name = names.drawer((c.drawer ?? [0, 0])[1]);
      return pending ? t('建议放进抽屉“{name}”', { name }) : t('放进了抽屉“{name}”', { name });
    }
    case 'book_metadata': {
      const label: Record<string, string> = { authors: t('作者'), publisher: t('出版社'), published: t('出版时间'), isbn: 'ISBN', pages: t('页数'), language: t('语言'), series: t('丛书'), description: t('简介') };
      const order = Object.keys(label);
      const parts = Object.entries(c.book ?? {}).sort(([a], [b]) => order.indexOf(a) - order.indexOf(b)).map(([k, v]) => {
        const after = (v as unknown[])[1];
        const text = Array.isArray(after) ? after.join(t('、')) : k === 'pages' ? t('{n} 页', { n: Number(after) }) : String(after);
        return t('{name}“{value}”', { name: label[k] ?? k, value: k === 'description' && text.length > 30 ? text.slice(0, 30) + '…' : text });
      });
      return pending ? t('建议补全书目：{list}', { list: parts.join(t('、')) }) : t('补全了书目：{list}', { list: parts.join(t('、')) });
    }
    case 'split_document': return t('建议拆成 {n} 份：{list}', { n: (c.split?.length ?? 0) + 1, list: (c.titles ?? []).filter(Boolean).join(t('、')) });
  }
  return '';
}

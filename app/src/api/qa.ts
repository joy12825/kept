// 问答（阶段 30）：自己的对话；提问的回答以事件流逐段返回。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, getCsrfToken, getWorkspaceId, unwrap } from './client';
import type { components } from './schema';
import { t, locale } from '../i18n';

export type QAConversation = components['schemas']['QAConversation'];
export type QAMessage = components['schemas']['QAMessage'];
export type QASource = components['schemas']['QASource'];

const key = ['qa'] as const;

export function useConversations() {
  return useQuery<QAConversation[], ApiError>({ queryKey: [...key, 'list'], queryFn: async () => unwrap(await api.GET('/api/v1/qa/conversations')) });
}

export function useConversation(id: number | undefined) {
  return useQuery<QAConversation, ApiError>({
    queryKey: [...key, 'one', id ?? 0],
    queryFn: async () => unwrap(await api.GET('/api/v1/qa/conversations/{id}', { params: { path: { id: id! } } })),
    enabled: id !== undefined,
  });
}

export function useCreateConversation() {
  const qc = useQueryClient();
  return useMutation<QAConversation, ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/qa/conversations')),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: key }); },
  });
}

export function useDeleteConversation() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/qa/conversations/{id}', { params: { path: { id } } })); },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: key }); },
  });
}

export interface AskHandlers {
  onSources: (s: QASource[]) => void;
  onDelta: (text: string) => void;
}

/** 发出问题并读事件流：按 sources / delta 回调，返回 done 的内容。signal 中止即停止生成。 */
async function streamAnswer<T>(url: string, body: unknown, h: AskHandlers, signal?: AbortSignal): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json', Accept: 'text/event-stream', 'Accept-Language': locale() };
  const csrf = getCsrfToken();
  if (csrf) headers['X-CSRF-Token'] = csrf;
  const ws = getWorkspaceId();
  if (ws !== null) headers['X-Workspace-ID'] = String(ws);
  const res = await fetch(url, { method: 'POST', headers, body: JSON.stringify(body), credentials: 'include', signal });
  if (!res.ok) {
    const b = await res.json().catch(() => ({})) as { code?: string; message?: string };
    throw new ApiError(res.status, b.code ?? 'error', b.message ?? t('提问失败'));
  }
  const reader = res.body!.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  let done: T | null = null;
  for (;;) {
    const { value, done: end } = await reader.read();
    if (value) buf += decoder.decode(value, { stream: true });
    let i: number;
    while ((i = buf.indexOf('\n\n')) >= 0) {
      const block = buf.slice(0, i);
      buf = buf.slice(i + 2);
      let event = '';
      let data = '';
      for (const line of block.split('\n')) {
        if (line.startsWith('event: ')) event = line.slice(7);
        else if (line.startsWith('data: ')) data += line.slice(6);
      }
      if (!event) continue;
      const payload = data ? JSON.parse(data) : null;
      if (event === 'sources') h.onSources(payload as QASource[]);
      else if (event === 'delta') h.onDelta(payload as string);
      else if (event === 'done') done = payload as T;
      else if (event === 'error') throw new ApiError(500, 'qa_failed', (payload as { message: string }).message);
    }
    if (end) break;
  }
  if (!done) throw new ApiError(500, 'qa_failed', t('回答中断了'));
  return done;
}

/** 提问：返回保存后的回答。 */
export function ask(conversationId: number, question: string, h: AskHandlers, signal?: AbortSignal): Promise<QAMessage> {
  return streamAnswer<QAMessage>(`/api/v1/qa/conversations/${conversationId}/messages`, { question }, h, signal);
}

export type DocAnswer = components['schemas']['DocAnswer'];
export interface DocTurn { question: string; answer: string }

/** 问这份文档（v1.5.0）：只依据这份文档回答，不保存；history 是前几轮。 */
export function askDocument(docId: number, question: string, history: DocTurn[], h: AskHandlers, signal?: AbortSignal): Promise<DocAnswer> {
  return streamAnswer<DocAnswer>(`/api/v1/documents/${docId}/ask`, { question, history: history.slice(-3) }, h, signal);
}

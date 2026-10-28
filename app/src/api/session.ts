import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import {
  api, ApiError, setCsrfToken, setWorkspaceId, unwrap,
  type LoginRequest, type LoginTwoFactorRequest, type Me, type NewAccountRequest, type SetupStatus, type TwoFactorChallenge,
} from './client';
import { applyPreference, savedPreference, type LangPreference } from '../i18n';

export const meKey = ['me'] as const;
export const setupKey = ['setup'] as const;

/** 清除与当前用户有关的一切前端状态（退出、会话过期、换人登录时调用），保留 me/setup 两个 Query 本身以免 AuthGate 失去订阅。 */
export function resetUserState(qc: QueryClient, me: Me | null) {
  setCsrfToken(me?.csrfToken ?? null);
  // 保存的空间属于上一个登录的人；登录响应里的空间总是个人空间
  setWorkspaceId(null);
  qc.setQueryData(meKey, me);
  qc.removeQueries({ predicate: (q) => q.queryKey[0] !== meKey[0] && q.queryKey[0] !== setupKey[0] });
  qc.getMutationCache().clear();
}

export function useMe() {
  return useQuery<Me | null, ApiError>({
    queryKey: meKey,
    queryFn: async () => {
      const r = await api.GET('/api/v1/me');
      if (r.response.status === 401) return null;
      const me = unwrap(r);
      setCsrfToken(me.csrfToken);
      // 账号还没设语言、这台浏览器在登录页选过：用这个选择，存到账号上（v1.10.0）
      const saved = savedPreference();
      if (me.locale === '' && saved !== '') {
        void api.PUT('/api/v1/me/locale', { body: { locale: saved } });
        return { ...me, locale: saved };
      }
      // 账号上的语言和这台浏览器记下的不同（换了设备）：记下，语言变了就重新加载
      applyPreference(me.locale);
      return me;
    },
    staleTime: 5 * 60_000,
  });
}

/** 改账号上的界面语言（v1.10.0）：存到服务器，显示的语言变了就重新加载页面。 */
export function useSetLocale() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, LangPreference>({
    mutationFn: async (locale) => { unwrap(await api.PUT('/api/v1/me/locale', { body: { locale } })); },
    onSuccess: (_, locale) => {
      qc.setQueryData<Me | null>(meKey, (me) => (me ? { ...me, locale } : me));
      applyPreference(locale);
    },
  });
}

export function useSetupStatus() {
  return useQuery<SetupStatus, ApiError>({
    queryKey: setupKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/setup')),
  });
}

function applySignedIn(qc: QueryClient, me: Me) {
  resetUserState(qc, me);
  // 裁决 R2：登录、初始化成功后，初始化必然已完成；直接写入缓存而不是重新请求，
  // 避免重新请求导致界面仍停留在初始化页。
  qc.setQueryData<SetupStatus>(setupKey, (old) => old && { ...old, needsSetup: false });
}

function useSignedIn<V>(call: (v: V) => Promise<Me>) {
  const qc = useQueryClient();
  return useMutation<Me, ApiError, V>({
    mutationFn: call,
    onSuccess: (me) => applySignedIn(qc, me),
  });
}

export const useSetup = () =>
  useSignedIn<NewAccountRequest>(async (body) => unwrap(await api.POST('/api/v1/setup', { body })));

/** 登录成功（200）或需要两步验证（202）；只有 signedIn 时才重置用户态。 */
export type LoginOutcome = { kind: 'signedIn'; me: Me } | { kind: 'challenge'; challenge: string };

export function useLogin() {
  const qc = useQueryClient();
  return useMutation<LoginOutcome, ApiError, LoginRequest>({
    mutationFn: async (body) => {
      const r = await api.POST('/api/v1/auth/login', { body });
      const data = unwrap<Me | TwoFactorChallenge>(r);
      if (r.response.status === 202) return { kind: 'challenge', challenge: (data as TwoFactorChallenge).challenge };
      return { kind: 'signedIn', me: data as Me };
    },
    onSuccess: (outcome) => { if (outcome.kind === 'signedIn') applySignedIn(qc, outcome.me); },
  });
}

export function useLoginTwoFactor() {
  const qc = useQueryClient();
  return useMutation<Me, ApiError, LoginTwoFactorRequest>({
    mutationFn: async (body) => unwrap<Me>(await api.POST('/api/v1/auth/login/2fa', { body })),
    onSuccess: (me) => applySignedIn(qc, me),
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, void>({
    mutationFn: async () => { unwrap(await api.POST('/api/v1/auth/logout')); },
    onSettled: () => resetUserState(qc, null),
  });
}

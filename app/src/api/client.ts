import createClient, { type Middleware } from 'openapi-fetch';
import { locale, t } from '../i18n';
import type { components, paths } from './schema';

export type Me = components['schemas']['Me'];
export type User = components['schemas']['User'];
export type Workspace = components['schemas']['Workspace'];
export type WorkspaceRole = components['schemas']['WorkspaceRole'];
export type WorkspaceColor = components['schemas']['WorkspaceColor'];
export type ApiToken = components['schemas']['ApiToken'];
export type CreatedApiToken = components['schemas']['CreatedApiToken'];
export type SetupStatus = components['schemas']['SetupStatus'];
export type NewAccountRequest = components['schemas']['NewAccountRequest'];
export type LoginRequest = components['schemas']['LoginRequest'];
export type LoginTwoFactorRequest = components['schemas']['LoginTwoFactorRequest'];
export type TwoFactorChallenge = components['schemas']['TwoFactorChallenge'];
export type TwoFactorStatus = components['schemas']['TwoFactorStatus'];
export type TwoFactorSetup = components['schemas']['TwoFactorSetup'];
export type RecoveryCodes = components['schemas']['RecoveryCodes'];
export type VersionInfo = components['schemas']['VersionInfo'];
export type Health = components['schemas']['Health'];

/** 服务端返回的错误：message 是面向用户的中文，可直接显示。details 登录失败时含 remainingAttempts，被锁定时含 retryAfterSeconds。 */
export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public details?: Record<string, unknown>) {
    super(message);
    this.name = 'ApiError';
  }
}

let csrfToken: string | null = null;
export function getCsrfToken(): string | null {
  return csrfToken;
}

export function setCsrfToken(token: string | null | undefined) {
  csrfToken = token ?? null;
}

let onUnauthorized: () => void = () => {};
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

// 当前空间：保存在浏览器里，每个请求通过 X-Workspace-ID 带给服务端；null 表示个人空间（不带请求头）。
const WORKSPACE_KEY = 'kept-workspace';
const WORKSPACE_HEADER = 'X-Workspace-ID';

function readStoredWorkspace(): number | null {
  try {
    const n = Number(window.localStorage.getItem(WORKSPACE_KEY));
    return Number.isInteger(n) && n > 0 ? n : null;
  } catch {
    return null; // 隐私模式等场景下 localStorage 不可用
  }
}

let workspaceId: number | null = typeof window !== 'undefined' ? readStoredWorkspace() : null;

export function getWorkspaceId(): number | null {
  return workspaceId;
}

export function setWorkspaceId(id: number | null) {
  workspaceId = id;
  try {
    if (id === null) window.localStorage.removeItem(WORKSPACE_KEY);
    else window.localStorage.setItem(WORKSPACE_KEY, String(id));
  } catch {
    // 存不进去时只保留在内存里，刷新后回到个人空间
  }
}

/** 保存的空间已不可用（被移出、空间被删除）时调用：此时已清除保存的空间，调用方负责重新取数据。 */
let onWorkspaceGone: () => void = () => {};
export function setWorkspaceGoneHandler(fn: () => void) {
  onWorkspaceGone = fn;
}

const SAFE = new Set(['GET', 'HEAD', 'OPTIONS']);

const middleware: Middleware = {
  onRequest({ request }) {
    if (csrfToken && !SAFE.has(request.method)) request.headers.set('X-CSRF-Token', csrfToken);
    if (workspaceId !== null) request.headers.set(WORKSPACE_HEADER, String(workspaceId));
    request.headers.set('Accept-Language', locale()); // 服务端按它回提示（v1.10.1）
    return request;
  },
  async onResponse({ request, response }) {
    if (response.status === 401) onUnauthorized();
    if (response.status === 404 && workspaceId !== null && request.headers.get(WORKSPACE_HEADER) === String(workspaceId)) {
      const body = await response.clone().json().catch(() => null) as { code?: string } | null;
      // 并发的请求可能都带着同一个失效的空间：上面的比较保证只清除、通知一次
      if (body?.code === 'workspace_not_found' && request.headers.get(WORKSPACE_HEADER) === String(workspaceId)) {
        setWorkspaceId(null);
        onWorkspaceGone();
      }
    }
    return response;
  },
};

const origin = typeof window !== 'undefined' ? window.location.origin : 'http://localhost';

export function makeClient(fetchImpl: (req: Request) => Promise<Response> = (req) => fetch(req)) {
  const c = createClient<paths>({ baseUrl: origin, fetch: fetchImpl, credentials: 'same-origin' });
  c.use(middleware);
  return c;
}

export const api = makeClient();

/** 取出成功响应的数据；失败时抛出 ApiError。204 时返回 undefined。 */
export function unwrap<T>(r: { data?: T; error?: unknown; response: Response }): T {
  if (r.error !== undefined || !r.response.ok) {
    const e = (r.error ?? {}) as { code?: string; message?: string; details?: Record<string, unknown> };
    throw new ApiError(r.response.status, e.code ?? 'internal', e.message ?? t('网络错误，请稍后重试'), e.details);
  }
  return r.data as T;
}

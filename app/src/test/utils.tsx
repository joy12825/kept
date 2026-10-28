import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { CssBaseline, ThemeProvider } from '@mui/material';
import { render } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { vi } from 'vitest';
import type { ReactElement, ReactNode } from 'react';
import { buildTheme } from '../theme';
import { StoreProvider } from '../store/app-store';

export const jsonResponse = (status: number, body?: unknown) =>
  new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });

type Route = (req: Request) => Response | Promise<Response>;

/** 按 “方法 路径” 匹配桩响应，例如 { 'GET /api/v1/me': () => jsonResponse(401, {...}) }。返回记录下的全部请求。 */
export function stubFetch(routes: Record<string, Route>) {
  const calls: Request[] = [];
  vi.stubGlobal('fetch', vi.fn(async (input: Request | string, init?: RequestInit) => {
    // fetch('/path', init) 直接传地址时也按请求处理
    const req = typeof input === 'string' ? new Request(new URL(input, window.location.href), init) : input;
    calls.push(req);
    const key = `${req.method} ${new URL(req.url).pathname}`;
    const route = routes[key];
    if (!route) return jsonResponse(404, { code: 'not_found', message: `测试未提供 ${key}` });
    return route(req);
  }));
  return calls;
}

export function newQueryClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
}

/** route 可以带 state，如 { pathname: '/docs/7', state: { find: '电费' } } */
type Route0 = string | { pathname: string; search?: string; state?: unknown };

export function Providers({ children, client, route = '/' }: { children: ReactNode; client?: QueryClient; route?: Route0 }) {
  return (
    <QueryClientProvider client={client ?? newQueryClient()}>
      <StoreProvider>
        <ThemeProvider theme={buildTheme('light')}>
          <CssBaseline />
          <MemoryRouter initialEntries={[route]}>{children}</MemoryRouter>
        </ThemeProvider>
      </StoreProvider>
    </QueryClientProvider>
  );
}

export function renderWithProviders(ui: ReactElement, opts: { route?: Route0; client?: QueryClient } = {}) {
  return render(<Providers route={opts.route} client={opts.client}>{ui}</Providers>);
}

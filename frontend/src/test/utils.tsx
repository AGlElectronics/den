import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render } from '@testing-library/react';
import type { ReactElement } from 'react';
import { afterEach, vi } from 'vitest';

afterEach(() => cleanup());

/** Renders ui with a fresh React Query client (no retries). */
export function renderWithQuery(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

type Route = { method?: string; path: string | RegExp; status?: number; body?: unknown };

/** Stubs fetch with fixed answers and records every call. */
export function mockFetch(routes: Route[]) {
  const calls: { method: string; url: string }[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: string, init?: RequestInit) => {
      const url = new URL(input, 'http://localhost');
      const method = init?.method ?? 'GET';
      calls.push({ method, url: url.pathname + url.search });
      const r = routes.find(
        (x) => (x.method ?? 'GET') === method && (typeof x.path === 'string' ? x.path === url.pathname : x.path.test(url.pathname)),
      );
      if (!r) return new Response(JSON.stringify({ error: { code: 'not_found', message: 'no mock' } }), { status: 404 });
      return new Response(r.body === undefined ? null : JSON.stringify(r.body), { status: r.status ?? 200 });
    }),
  );
  return calls;
}

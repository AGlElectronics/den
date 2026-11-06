import { useQuery } from '@tanstack/react-query';

import type { UsableModule } from '@/api/dashboard';
import { getOidcBearer } from '@/auth/oidc';

// Suite-wide "portal to-dos": each paired module MAY serve
// GET <base_url>/api/portal/todos. Modules that don't (404, CORS, timeout) are
// skipped silently.

export type TodoSeverity = 'info' | 'warning' | 'danger';

export type PortalTodo = {
  id: string;
  title: string;
  detail?: string;
  due_at: string | null;
  url: string;
  severity: TodoSeverity;
  module_slug: string;
  module_name: string;
};

type TodosResponse = {
  items?: Array<Omit<PortalTodo, 'module_slug' | 'module_name'>>;
};

const TIMEOUT_MS = 5_000;
const REFETCH_MS = 5 * 60_000;
const SEVERITY_RANK: Record<TodoSeverity, number> = { danger: 0, warning: 1, info: 2 };

function todosUrl(baseUrl: string): string {
  return `${baseUrl.replace(/\/+$/, '')}/api/portal/todos`;
}

function isHttpUrl(url: unknown): boolean {
  if (typeof url !== 'string') return false;
  try {
    return /^https?:$/.test(new URL(url).protocol);
  } catch {
    return false;
  }
}

/** Null when the module doesn't implement the contract or can't be reached. */
async function fetchModuleTodos(mod: UsableModule, bearer: string): Promise<PortalTodo[] | null> {
  const ctrl = new AbortController();
  const timer = window.setTimeout(() => ctrl.abort(), TIMEOUT_MS);
  try {
    const res = await fetch(todosUrl(mod.base_url), {
      headers: { Accept: 'application/json', Authorization: `Bearer ${bearer}` },
      signal: ctrl.signal,
      credentials: 'omit',
    });
    if (!res.ok) {
      console.debug(`[portal-todos] ${mod.slug}: HTTP ${res.status}`);
      return null;
    }
    const data = (await res.json()) as TodosResponse;
    if (!Array.isArray(data?.items)) return null;
    return data.items
      .filter((it) => it && typeof it.id === 'string' && typeof it.title === 'string' && isHttpUrl(it.url))
      .map((it) => ({
        ...it,
        due_at: it.due_at ?? null,
        severity: it.severity in SEVERITY_RANK ? it.severity : 'info',
        module_slug: mod.slug,
        module_name: mod.display_name,
      }));
  } catch (err) {
    console.debug(`[portal-todos] ${mod.slug}: skipped`, err);
    return null;
  } finally {
    window.clearTimeout(timer);
  }
}

function dueTime(t: PortalTodo): number {
  const ms = t.due_at ? Date.parse(t.due_at) : NaN;
  return Number.isNaN(ms) ? Number.POSITIVE_INFINITY : ms;
}

function compareTodos(a: PortalTodo, b: PortalTodo): number {
  const da = dueTime(a);
  const db = dueTime(b);
  if (da !== db) return da < db ? -1 : 1;
  return SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity];
}

type PortalTodosResult = {
  items: PortalTodo[];
  /** How many modules answered with a valid to-do list. */
  responded: number;
};

async function fetchPortalTodos(modules: UsableModule[]): Promise<PortalTodosResult> {
  const bearer = await getOidcBearer();
  if (!bearer) return { items: [], responded: 0 };
  const results = await Promise.all(modules.map((m) => fetchModuleTodos(m, bearer)));
  const answered = results.filter((r): r is PortalTodo[] => r !== null);
  return { items: answered.flat().sort(compareTodos), responded: answered.length };
}

/** Merged to-dos from every usable module, sorted by due date (nulls last), then severity. */
export function usePortalTodos(modules: UsableModule[], enabled = true) {
  const q = useQuery({
    queryKey: ['portal-todos', modules.map((m) => `${m.slug}@${m.base_url}`).sort()],
    queryFn: () => fetchPortalTodos(modules),
    enabled: enabled && modules.length > 0,
    refetchInterval: REFETCH_MS,
    staleTime: 60_000,
    retry: false,
  });
  return {
    items: q.data?.items ?? [],
    responded: q.data?.responded ?? 0,
    loading: q.isPending && q.fetchStatus !== 'idle',
  };
}

import { useQuery } from '@tanstack/react-query';

import { apiRequest } from '@/api/client';

// Suite-wide "portal to-dos". Den's backend asks every module the person can
// use for GET <module>/api/portal/todos itself (with the module's pairing
// credential), so the browser only ever talks to Den and no token leaves it.

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

export type TodoModuleStatus = {
  slug: string;
  display_name: string;
  status: 'ok' | 'unsupported' | 'not_paired_for_todos' | 'error';
  count: number;
};

export type PortalTodosResponse = {
  /** False when Den has no DEN_CREDENTIAL_KEY and cannot call modules. */
  enabled: boolean;
  items: PortalTodo[];
  modules: TodoModuleStatus[];
  /** How many modules answered with a valid to-do list. */
  responded?: number;
};

const REFETCH_MS = 5 * 60_000;

export const fetchPortalTodos = () => apiRequest<PortalTodosResponse>('/api/v1/portal/todos');

/** Merged to-dos from every module the person can use, sorted by due date (nulls last), then severity. */
export function usePortalTodos(enabled = true) {
  const q = useQuery({
    queryKey: ['portal-todos'],
    queryFn: fetchPortalTodos,
    enabled,
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

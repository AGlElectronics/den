import { AlertTriangle, ArrowUpRight, CheckCircle2, CircleAlert, ExternalLink, Info } from 'lucide-react';

import type { UsableModule } from '@/api/dashboard';
import { type PortalTodo, type TodoSeverity, usePortalTodos } from '@/api/portalTodos';
import { cn } from '@/lib/cn';

const DAY_MS = 24 * 60 * 60 * 1000;

const SEVERITY_STYLE: Record<TodoSeverity, { icon: typeof Info; className: string }> = {
  danger: { icon: CircleAlert, className: 'bg-den-danger-bg text-den-danger' },
  warning: { icon: AlertTriangle, className: 'bg-den-warn-bg text-den-warn' },
  info: { icon: Info, className: 'bg-den-accent text-den-primary' },
};

function tasksModule(modules: UsableModule[]): UsableModule | null {
  return modules.find((m) => m.slug.toLowerCase() === 'tasks') ?? null;
}

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

function relativeDue(dueAt: string | null): { text: string; overdue: boolean } | null {
  if (!dueAt) return null;
  const due = new Date(dueAt);
  if (Number.isNaN(due.getTime())) return null;
  const days = Math.round((startOfDay(due) - startOfDay(new Date())) / DAY_MS);
  if (days === 0) return { text: 'Due today', overdue: false };
  if (days === 1) return { text: 'Due tomorrow', overdue: false };
  if (days > 1) return { text: `Due in ${days} days`, overdue: false };
  if (days === -1) return { text: 'Overdue by 1 day', overdue: true };
  return { text: `Overdue by ${-days} days`, overdue: true };
}

function TodoRow({ todo }: { todo: PortalTodo }) {
  const { icon: Icon, className } = SEVERITY_STYLE[todo.severity];
  const due = relativeDue(todo.due_at);
  return (
    <li>
      <a
        href={todo.url}
        target="_blank"
        rel="noreferrer"
        className="group flex items-start gap-3 rounded-2xl px-3 py-3 transition hover:bg-den-muted"
      >
        <span className={cn('mt-0.5 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-xl', className)}>
          <Icon size={16} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="font-medium text-den-foreground group-hover:text-den-primary">{todo.title}</span>
            <span className="rounded-full border border-den-border px-2 py-0.5 text-xs font-medium text-den-foreground/60">
              {todo.module_name}
            </span>
          </div>
          {todo.detail ? <p className="mt-0.5 text-sm text-den-foreground/60">{todo.detail}</p> : null}
        </div>
        {due ? (
          <span
            className={cn(
              'mt-1 shrink-0 text-xs font-medium',
              due.overdue ? 'text-den-danger' : 'text-den-foreground/50',
            )}
            title={new Date(todo.due_at!).toLocaleString()}
          >
            {due.text}
          </span>
        ) : null}
        <ArrowUpRight
          size={14}
          className="mt-1 shrink-0 text-den-foreground/30 transition group-hover:translate-x-0.5 group-hover:-translate-y-0.5 group-hover:text-den-primary"
        />
      </a>
    </li>
  );
}

const SEVERITY_RANK: Record<TodoSeverity, number> = { danger: 0, warning: 1, info: 2 };

/** Highest-priority first; keep the portal strip short (top 4). */
function topTodos(items: PortalTodo[], limit = 4): PortalTodo[] {
  return [...items]
    .sort((a, b) => {
      const sev = SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity];
      if (sev !== 0) return sev;
      if (a.due_at && b.due_at) return a.due_at.localeCompare(b.due_at);
      if (a.due_at) return -1;
      if (b.due_at) return 1;
      return a.title.localeCompare(b.title);
    })
    .slice(0, limit);
}

/** "Your to-dos" — only when the person can use Tasks; link opens Tasks. */
export function PortalTodos({ modules }: { modules: UsableModule[] }) {
  const tasks = tasksModule(modules);
  const { items, responded, loading } = usePortalTodos(Boolean(tasks));
  const shown = topTodos(items);
  const hidden = Math.max(0, items.length - shown.length);

  if (!tasks) return null;

  // No module implements the contract (or none reachable): stay out of the way.
  if (!loading && responded === 0) return null;

  const tasksHref = tasks.base_url.replace(/\/$/, '');

  return (
    <section className="mt-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="font-heading text-xl font-bold">Your to-dos</h2>
          <p className="mt-1 text-sm text-den-foreground/60">
            The four most urgent items across your services.
          </p>
        </div>
        <a
          href={tasksHref}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1.5 rounded-full border border-den-border bg-den-surface px-3.5 py-2 text-sm font-medium text-den-foreground hover:border-den-primary/40 hover:text-den-primary"
        >
          Open Tasks
          <ExternalLink size={12} />
        </a>
      </div>

      <div className="mt-4 rounded-3xl border border-den-border bg-den-surface p-2 shadow-sm">
        {loading ? (
          <div className="flex flex-col gap-2 p-1">
            {[1, 2].map((i) => (
              <div key={i} className="h-14 animate-pulse rounded-2xl bg-den-muted" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="flex items-center gap-3 px-4 py-5 text-sm text-den-foreground/70">
            <CheckCircle2 size={18} className="text-den-success" />
            You&apos;re all caught up.
          </div>
        ) : (
          <>
            <ul className="divide-y divide-den-border/60">
              {shown.map((t) => (
                <TodoRow key={`${t.module_slug}:${t.id}`} todo={t} />
              ))}
            </ul>
            {hidden > 0 ? (
              <p className="px-4 py-2 text-xs text-den-foreground/50">+{hidden} more in Tasks</p>
            ) : null}
          </>
        )}
      </div>
    </section>
  );
}

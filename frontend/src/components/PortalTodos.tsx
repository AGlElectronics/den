import { AlertTriangle, ArrowUpRight, CheckCircle2, CircleAlert, Info } from 'lucide-react';

import type { UsableModule } from '@/api/dashboard';
import { type PortalTodo, type TodoSeverity, usePortalTodos } from '@/api/portalTodos';
import { cn } from '@/lib/cn';

const DAY_MS = 24 * 60 * 60 * 1000;

const SEVERITY_STYLE: Record<TodoSeverity, { icon: typeof Info; className: string }> = {
  danger: { icon: CircleAlert, className: 'bg-den-danger-bg text-den-danger' },
  warning: { icon: AlertTriangle, className: 'bg-den-warn-bg text-den-warn' },
  info: { icon: Info, className: 'bg-den-accent text-den-primary' },
};

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

/** "Your to-dos" across every paired module that implements /api/portal/todos. */
export function PortalTodos({ modules }: { modules: UsableModule[] }) {
  const { items, responded, loading } = usePortalTodos(modules);

  // No module implements the contract (or none reachable): stay out of the way.
  if (!loading && responded === 0) return null;

  return (
    <section className="mt-8">
      <h2 className="font-heading text-xl font-bold">Your to-dos</h2>
      <p className="mt-1 text-sm text-den-foreground/60">Things waiting on you across your services.</p>

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
            You're all caught up.
          </div>
        ) : (
          <ul className="divide-y divide-den-border/60">
            {items.map((t) => (
              <TodoRow key={`${t.module_slug}:${t.id}`} todo={t} />
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}

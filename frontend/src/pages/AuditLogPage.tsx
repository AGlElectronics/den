import { useInfiniteQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';

import { AUDIT_ACTION_GROUPS, listAuditEvents, type AuditEvent, type AuditFilters } from '@/api/audit';

const input = 'mt-1 w-full rounded-md border border-den-border bg-den-surface px-2 py-1';

/** "2026-11-11" from a date input → RFC 3339 at midnight UTC. */
function dayStart(v: string) {
  return v ? `${v}T00:00:00Z` : undefined;
}

function nextDay(v: string) {
  if (!v) return undefined;
  const d = new Date(`${v}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + 1);
  return d.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

/** The admin audit log: every change in Den, newest first, with filters. */
export function AuditLogPage() {
  const [action, setAction] = useState('');
  const [entityID, setEntityID] = useState('');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const filters: AuditFilters = {
    action: action || undefined,
    entity_id: entityID.trim() || undefined,
    since: dayStart(from),
    until: nextDay(to),
  };
  const q = useInfiniteQuery({
    queryKey: ['audit-events', filters],
    queryFn: ({ pageParam }) => listAuditEvents(filters, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor,
  });
  const events = q.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <div className="max-w-6xl space-y-6">
      <div>
        <h1 className="text-2xl font-heading font-bold">Audit log</h1>
        <p className="mt-1 text-sm text-den-foreground/70">
          Every change made in Den: modules, pairing and credentials, branding, settings, backups and downloads, and the
          Keycloak sync. &quot;System&quot; means Den did it by itself.
        </p>
      </div>

      <div className="grid gap-3 rounded-lg border border-den-border bg-den-surface p-4 text-sm md:grid-cols-4">
        <label>
          What
          <select className={input} value={action} onChange={(e) => setAction(e.target.value)} aria-label="Action">
            {AUDIT_ACTION_GROUPS.map((g) => (
              <option key={g.value} value={g.value}>
                {g.label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Item id
          <input className={input} value={entityID} placeholder="module id, …" onChange={(e) => setEntityID(e.target.value)} />
        </label>
        <label>
          From
          <input type="date" className={input} value={from} onChange={(e) => setFrom(e.target.value)} />
        </label>
        <label>
          To
          <input type="date" className={input} value={to} onChange={(e) => setTo(e.target.value)} />
        </label>
      </div>

      <div className="overflow-hidden rounded-lg border border-den-border bg-den-surface">
        <table className="w-full text-sm">
          <thead className="bg-den-muted text-left text-xs uppercase tracking-wide text-den-foreground/60">
            <tr>
              <th className="w-6 px-2 py-2" />
              <th className="px-4 py-2">When</th>
              <th className="px-4 py-2">Who</th>
              <th className="px-4 py-2">What</th>
              <th className="px-4 py-2">Note</th>
            </tr>
          </thead>
          <tbody>
            {events.map((ev) => (
              <AuditRow key={ev.id} ev={ev} />
            ))}
            {!q.isLoading && events.length === 0 && (
              <tr>
                <td colSpan={5} className="px-4 py-6 text-center text-den-foreground/60">
                  {q.isError ? (q.error as Error).message : 'Nothing found.'}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {q.isLoading && <p className="text-sm text-den-foreground/60">Loading…</p>}
      {q.hasNextPage && (
        <button
          type="button"
          className="rounded-md border border-den-border px-3 py-2 text-sm disabled:opacity-50"
          disabled={q.isFetchingNextPage}
          onClick={() => void q.fetchNextPage()}
        >
          {q.isFetchingNextPage ? 'Loading…' : 'Show older events'}
        </button>
      )}
    </div>
  );
}

function AuditRow({ ev }: { ev: AuditEvent }) {
  const [open, setOpen] = useState(false);
  const hasDetail = ev.before !== undefined || ev.after !== undefined;
  return (
    <>
      <tr className="border-t border-den-border align-top">
        <td className="px-2 py-2">
          {hasDetail && (
            <button type="button" onClick={() => setOpen((v) => !v)} aria-label={open ? 'Hide details' : 'Show details'}>
              {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            </button>
          )}
        </td>
        <td className="whitespace-nowrap px-4 py-2">{new Date(ev.created_at).toLocaleString()}</td>
        <td className="px-4 py-2">
          {ev.actor ? (
            <span title={ev.actor.email}>{ev.actor.display_name || ev.actor.id}</span>
          ) : (
            <span className="text-den-foreground/50">System</span>
          )}
        </td>
        <td className="px-4 py-2">
          <code className="font-mono text-xs">{ev.action}</code>
          <div className="font-mono text-[11px] text-den-foreground/50">
            {ev.entity_type} {ev.entity_id}
          </div>
        </td>
        <td className="px-4 py-2 text-den-foreground/80">{ev.note}</td>
      </tr>
      {open && (
        <tr className="border-t border-den-border bg-den-muted/40">
          <td />
          <td colSpan={4} className="grid gap-3 px-4 py-2 md:grid-cols-2">
            {(['before', 'after'] as const).map((k) => (
              <div key={k}>
                <div className="text-xs font-semibold uppercase text-den-foreground/60">{k}</div>
                <pre className="mt-1 max-h-64 overflow-auto rounded bg-den-surface p-2 text-xs">
                  {ev[k] === undefined ? '—' : JSON.stringify(ev[k], null, 2)}
                </pre>
              </div>
            ))}
          </td>
        </tr>
      )}
    </>
  );
}

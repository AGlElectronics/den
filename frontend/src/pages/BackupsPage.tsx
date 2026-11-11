import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle2, Download, Loader2, Lock, Play } from 'lucide-react';

import {
  backupNow,
  backupProblem,
  backupWarning,
  downloadBackup,
  formatBytes,
  getOperationsStatus,
  listBackups,
  saveBackupSettings,
  type Backup,
  type BackupSettings,
  type OperationsStatus,
} from '@/api/operations';
import { useMe } from '@/hooks/useMe';
import { cn } from '@/lib/cn';
import { isOps } from '@/lib/roles';

const card = 'rounded-lg border border-den-border bg-den-surface p-4 text-sm';
const input = 'mt-1 w-full rounded-md border border-den-border bg-den-surface px-2 py-1';

export function BackupsPage() {
  const qc = useQueryClient();
  const me = useMe();
  const ops = isOps(me.data);
  const status = useQuery({
    queryKey: ['operations-status'],
    queryFn: getOperationsStatus,
    refetchInterval: (q) => (isRunning(q.state.data) ? 3_000 : 30_000),
  });
  const s = status.data;
  const ready = s?.configured && s.reachable ? s : undefined;
  const backups = useQuery({
    queryKey: ['backups'],
    queryFn: listBackups,
    enabled: ops && !!ready?.backups_enabled,
    refetchInterval: isRunning(s) ? 3_000 : 60_000,
  });
  const now = useMutation({
    mutationFn: backupNow,
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['operations-status'] }),
  });

  const problem = backupProblem(s);
  const warning = backupWarning(s);

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-heading font-bold">Backups</h1>
          <p className="mt-1 text-sm text-den-foreground/70">
            Every night Den backs up all of Con Services: every module&apos;s database, the sign-in accounts
            (Keycloak) and uploaded files. Backups are encrypted and kept in separate storage.
          </p>
        </div>
        {ops && ready?.backups_enabled && (
          <button
            type="button"
            className="inline-flex shrink-0 items-center gap-1.5 rounded-md bg-den-primary px-3 py-2 text-den-on-primary hover:bg-den-primary-hover disabled:opacity-60"
            disabled={now.isPending || !!ready.running}
            onClick={() => now.mutate()}
          >
            {ready.running ? <Loader2 size={16} className="animate-spin" /> : <Play size={16} />}
            {ready.running ? 'Backing up…' : 'Back up now'}
          </button>
        )}
      </div>

      {problem && (
        <div className="flex items-start gap-2 rounded-lg border border-den-danger-border bg-den-danger-bg p-3 text-sm text-den-danger">
          <AlertTriangle size={16} className="mt-0.5 shrink-0" />
          <span>{problem}</span>
        </div>
      )}
      {warning && (
        <div className="flex items-start gap-2 rounded-lg border border-den-warn-border bg-den-warn-bg p-3 text-sm text-den-warn">
          <AlertTriangle size={16} className="mt-0.5 shrink-0" />
          <span>{warning}</span>
        </div>
      )}
      {now.isError && <p className="text-sm text-den-danger">{(now.error as Error).message}</p>}
      {!ops && me.data && (
        <p className="flex items-start gap-2 rounded-lg border border-den-border bg-den-surface p-3 text-sm text-den-foreground/80">
          <Lock size={16} className="mt-0.5 shrink-0" />
          <span>
            You can see the backup status, but running, scheduling and downloading backups needs the{' '}
            <strong>ops</strong> role (the <code>den-ops</code> group by default).
          </span>
        </p>
      )}

      {status.isLoading ? (
        <p className="text-sm text-den-foreground/60">Loading…</p>
      ) : !s?.configured ? (
        <section className={card}>
          <h2 className="font-heading text-base font-bold">Not set up</h2>
          <p className="mt-1 text-den-foreground/70">
            This Den has no operator, so it neither backs anything up nor manages module databases. Set{' '}
            <code>OPERATOR_URL</code> and <code>OPERATOR_TOKEN</code> on the Den backend (see the operations guide).
            Until then, backups are the responsibility of whoever runs the servers.
          </p>
        </section>
      ) : !s.reachable ? (
        <section className={card}>
          <h2 className="font-heading text-base font-bold">Operator unreachable</h2>
          <p className="mt-1 text-den-danger">{s.error}</p>
        </section>
      ) : (
        <>
          {ready && <Overview s={ready} />}
          {ops && ready?.backups_enabled && ready.settings && (
            <ScheduleCard settings={ready.settings} timezone={ready.timezone} />
          )}
          {ready && <DatabasesCard s={ready} />}
          {ops && ready?.backups_enabled && <BackupList backups={backups.data} loading={backups.isLoading} />}
          <section className={card}>
            <h2 className="font-heading text-base font-bold">Restoring</h2>
            <p className="mt-1 text-den-foreground/70">
              Restoring replaces live data, so it is done on the command line rather than with a button. The{' '}
              <a
                className="text-den-primary underline hover:text-den-primary-hover"
                href="https://aglelectronics.github.io/con-services/operations/databases-and-backups/#restore"
                target="_blank"
                rel="noreferrer"
              >
                restore guide
              </a>{' '}
              walks through it step by step. You need the secret backup key, which is kept outside Con Services.
            </p>
          </section>
        </>
      )}
    </div>
  );
}

type Ready = Extract<OperationsStatus, { reachable: true }>;

function isRunning(s: OperationsStatus | undefined) {
  return !!(s && s.configured && s.reachable && s.running);
}

function Overview({ s }: { s: Ready }) {
  if (!s.backups_enabled) {
    return (
      <section className={card}>
        <h2 className="font-heading text-base font-bold">Backups are turned off</h2>
        <p className="mt-1 text-den-foreground/70">
          The operator runs with <code>BACKUP_ENABLED=false</code>. Nothing is backed up by Den.
        </p>
      </section>
    );
  }
  const last = s.last;
  return (
    <section className={cn(card, 'grid gap-4 md:grid-cols-3')}>
      <div>
        <div className="text-xs uppercase tracking-wide text-den-foreground/60">Last backup</div>
        {s.running ? (
          <p className="mt-1 flex items-center gap-1.5">
            <Loader2 size={14} className="animate-spin" /> Running now…
          </p>
        ) : last ? (
          <>
            <p className={cn('mt-1 flex items-center gap-1.5 font-medium', last.ok ? 'text-den-success' : 'text-den-danger')}>
              {last.ok ? <CheckCircle2 size={14} /> : <AlertTriangle size={14} />}
              {last.ok ? 'Succeeded' : 'Failed'} · {new Date(last.finished_at).toLocaleString()}
            </p>
            {last.ok && (
              <p className="text-den-foreground/70">
                {formatBytes(last.size_bytes)} · {last.databases?.length ?? 0} databases, {last.buckets?.length ?? 0} file
                stores
              </p>
            )}
          </>
        ) : (
          <p className="mt-1 text-den-foreground/70">None yet.</p>
        )}
      </div>
      <div>
        <div className="text-xs uppercase tracking-wide text-den-foreground/60">Next backup</div>
        <p className="mt-1">{s.next_run ? new Date(s.next_run).toLocaleString() : '—'}</p>
      </div>
      <div>
        <div className="text-xs uppercase tracking-wide text-den-foreground/60">Stored in</div>
        <p className="mt-1 break-all font-mono text-xs">{s.target}</p>
        <p className="mt-1 flex items-center gap-1 text-den-foreground/70">
          <Lock size={12} /> Encrypted for {s.recipients?.length ?? 0} key{s.recipients?.length === 1 ? '' : 's'}
        </p>
      </div>
    </section>
  );
}

function ScheduleCard({ settings, timezone }: { settings: BackupSettings; timezone: string }) {
  const qc = useQueryClient();
  const [form, setForm] = useState(settings);
  useEffect(() => setForm(settings), [settings]);
  const save = useMutation({
    mutationFn: saveBackupSettings,
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['operations-status'] }),
  });
  const dirty = JSON.stringify(form) !== JSON.stringify(settings);
  const num = (k: keyof BackupSettings, label: string, hint: string, min: number) => (
    <label className="text-sm">
      {label}
      <input
        type="number"
        min={min}
        className={input}
        value={form[k]}
        onChange={(e) => setForm({ ...form, [k]: Number(e.target.value) })}
      />
      <span className="text-xs text-den-foreground/60">{hint}</span>
    </label>
  );

  return (
    <section className={cn(card, 'space-y-3')}>
      <div>
        <h2 className="font-heading text-base font-bold">Schedule and how long backups are kept</h2>
        <p className="text-den-foreground/70">
          Older daily backups are thinned out automatically. Backups made with “Back up now” are kept for as many
          days as daily ones; backups made before removing a module are never deleted automatically.
        </p>
      </div>
      <div className="grid gap-3 md:grid-cols-4">
        <label className="text-sm">
          Daily backup at
          <select className={input} value={form.hour} onChange={(e) => setForm({ ...form, hour: Number(e.target.value) })}>
            {Array.from({ length: 24 }, (_, h) => (
              <option key={h} value={h}>
                {String(h).padStart(2, '0')}:00
              </option>
            ))}
          </select>
          <span className="text-xs text-den-foreground/60">Time zone {timezone}</span>
        </label>
        {num('keep_daily', 'Days', 'One backup per day', 1)}
        {num('keep_weekly', 'Weeks', 'Then one per week', 0)}
        {num('keep_monthly', 'Months', 'Then one per month', 0)}
      </div>
      <div className="flex items-center gap-3">
        <button
          type="button"
          className="rounded-md bg-den-primary px-3 py-2 text-den-on-primary hover:bg-den-primary-hover disabled:opacity-50"
          disabled={!dirty || save.isPending}
          onClick={() => save.mutate(form)}
        >
          {save.isPending ? 'Saving…' : 'Save'}
        </button>
        {save.isSuccess && !dirty && <span className="text-den-success">Saved.</span>}
        {save.isError && <span className="text-den-danger">{(save.error as Error).message}</span>}
      </div>
    </section>
  );
}

function DatabasesCard({ s }: { s: Ready }) {
  return (
    <section className={cn(card, 'space-y-3')}>
      <div>
        <h2 className="font-heading text-base font-bold">Databases</h2>
        <p className="text-den-foreground/70">
          {s.managed_databases
            ? 'Den creates a database when you add a module and can delete it when you remove the module.'
            : 'Den-managed databases are turned off: databases are created and removed by whoever runs the servers.'}
        </p>
      </div>
      <table className="w-full">
        <thead className="text-left text-xs uppercase tracking-wide text-den-foreground/60">
          <tr>
            <th className="py-1">Name</th>
            <th className="py-1">Size</th>
            <th className="py-1" />
          </tr>
        </thead>
        <tbody>
          {s.databases.map((d) => (
            <tr key={d.name} className="border-t border-den-border">
              <td className="py-1.5 font-mono">{d.name}</td>
              <td className="py-1.5">{formatBytes(d.size_bytes)}</td>
              <td className="py-1.5 text-xs text-den-foreground/60">{d.protected ? 'Never deleted by Den' : ''}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {s.file_buckets.length > 0 && (
        <p className="text-den-foreground/70">
          File stores included in backups: <span className="font-mono">{s.file_buckets.join(', ')}</span>
        </p>
      )}
    </section>
  );
}

function kindLabel(b: Backup) {
  switch (b.kind) {
    case 'scheduled':
      return 'Daily';
    case 'manual':
      return 'Manual';
    case 'pre-drop':
      return `Before removing ${b.database}`;
    default:
      return b.kind;
  }
}

function BackupList({ backups, loading }: { backups?: Backup[]; loading: boolean }) {
  const [error, setError] = useState('');
  return (
    <section className={cn(card, 'space-y-3')}>
      <h2 className="font-heading text-base font-bold">Stored backups</h2>
      <p className="text-den-foreground/70">
        Downloads are encrypted; they can only be opened with the secret backup key.
      </p>
      {error && <p className="text-den-danger">{error}</p>}
      {loading ? (
        <p className="text-den-foreground/60">Loading…</p>
      ) : !backups?.length ? (
        <p className="text-den-foreground/60">No backups yet.</p>
      ) : (
        <table className="w-full">
          <thead className="text-left text-xs uppercase tracking-wide text-den-foreground/60">
            <tr>
              <th className="py-1">Made</th>
              <th className="py-1">Type</th>
              <th className="py-1">Size</th>
              <th className="py-1" />
            </tr>
          </thead>
          <tbody>
            {backups.map((b) => (
              <tr key={b.key} className="border-t border-den-border">
                <td className="py-1.5">{new Date(b.created_at).toLocaleString()}</td>
                <td className="py-1.5">{kindLabel(b)}</td>
                <td className="py-1.5">{formatBytes(b.size_bytes)}</td>
                <td className="py-1.5 text-right">
                  <button
                    type="button"
                    className="inline-flex items-center gap-1 text-den-primary hover:text-den-primary-hover"
                    onClick={() => {
                      setError('');
                      downloadBackup(b.key).catch((e: Error) => setError(e.message));
                    }}
                  >
                    <Download size={14} /> Download
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

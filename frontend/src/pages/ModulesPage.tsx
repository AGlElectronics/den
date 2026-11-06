import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useState } from 'react';
import { AlertTriangle, Copy, Database, Key, Loader2, Plus, Trash2 } from 'lucide-react';

import {
  createModule,
  deleteModule,
  listModules,
  mintPairingToken,
  type CreatedDatabase,
  type Module,
  type PairingToken,
  type RemoveModuleResult,
} from '@/api/modules';
import { formatBytes, getOperationsStatus, type ServerDatabase } from '@/api/operations';
import { cn } from '@/lib/cn';

export function ModulesPage() {
  const qc = useQueryClient();
  const modules = useQuery({ queryKey: ['modules'], queryFn: listModules });
  const ops = useQuery({ queryKey: ['operations-status'], queryFn: getOperationsStatus });
  const ready = ops.data?.configured && ops.data.reachable ? ops.data : undefined;
  const managed = !!ready?.managed_databases;
  const databases = new Map((ready?.databases ?? []).map((d) => [d.name, d]));

  const [showNew, setShowNew] = useState(false);
  const [slug, setSlug] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [baseURL, setBaseURL] = useState('');
  const [environment, setEnvironment] = useState('production');
  const [withDatabase, setWithDatabase] = useState(true);

  const [lastToken, setLastToken] = useState<PairingToken | null>(null);
  const [newDatabase, setNewDatabase] = useState<CreatedDatabase | null>(null);
  const [removing, setRemoving] = useState<Module | null>(null);
  const [removed, setRemoved] = useState<{ name: string; result: RemoveModuleResult } | null>(null);

  const create = useMutation({
    mutationFn: createModule,
    onSuccess: (res) => {
      setShowNew(false);
      setSlug('');
      setDisplayName('');
      setBaseURL('');
      setEnvironment('production');
      setNewDatabase(res.database ?? null);
      void qc.invalidateQueries({ queryKey: ['modules'] });
      void qc.invalidateQueries({ queryKey: ['operations-status'] });
    },
  });

  const mint = useMutation({
    mutationFn: mintPairingToken,
    onSuccess: (res) => setLastToken(res),
  });

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-heading font-bold">Modules</h1>
          <p className="text-sm text-den-foreground/70 mt-1">
            The modules that belong to this organization. Add one, then pair it so it picks up the branding and
            settings from Den.
          </p>
        </div>
        <button
          type="button"
          className="inline-flex items-center gap-1 bg-den-primary text-den-on-primary px-3 py-2 rounded-md hover:bg-den-primary-hover"
          onClick={() => setShowNew((v) => !v)}
        >
          <Plus size={16} /> New module
        </button>
      </div>

      {showNew && (
        <form
          className="bg-den-surface border border-den-border rounded-lg p-4 grid gap-3 md:grid-cols-4"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate({
              slug: slug.trim(),
              display_name: displayName.trim(),
              base_url: baseURL.trim(),
              environment: environment.trim() || 'production',
              create_database: managed && withDatabase,
            });
          }}
        >
          <label className="text-sm">
            Short name
            <input
              className="mt-1 w-full rounded-md border border-den-border px-2 py-1"
              value={slug}
              placeholder="e.g. tickets"
              onChange={(e) => setSlug(e.target.value.toLowerCase())}
            />
          </label>
          <label className="text-sm">
            Display name
            <input
              className="mt-1 w-full rounded-md border border-den-border px-2 py-1"
              value={displayName}
              placeholder="e.g. Tickets"
              onChange={(e) => setDisplayName(e.target.value)}
            />
          </label>
          <label className="text-sm">
            Address
            <input
              className="mt-1 w-full rounded-md border border-den-border px-2 py-1"
              value={baseURL}
              placeholder="https://tickets.example.org"
              onChange={(e) => setBaseURL(e.target.value)}
            />
          </label>
          <label className="text-sm">
            Environment
            <input
              className="mt-1 w-full rounded-md border border-den-border px-2 py-1"
              value={environment}
              onChange={(e) => setEnvironment(e.target.value)}
            />
          </label>
          {managed && (
            <label className="md:col-span-4 flex items-start gap-2 text-sm">
              <input
                type="checkbox"
                className="mt-1"
                checked={withDatabase}
                onChange={(e) => setWithDatabase(e.target.checked)}
              />
              <span>
                Create a database for it
                <span className="block text-xs text-den-foreground/60">
                  Den sets up its own database and login on the shared database server and shows the connection
                  details once, to put into the module&apos;s configuration before starting it.
                </span>
              </span>
            </label>
          )}
          <div className="md:col-span-4 flex gap-2">
            <button
              type="submit"
              className="bg-den-primary text-den-on-primary px-3 py-2 rounded-md hover:bg-den-primary-hover disabled:opacity-60"
              disabled={create.isPending || !slug.trim() || !displayName.trim()}
            >
              {create.isPending ? 'Creating…' : 'Create module'}
            </button>
            <button
              type="button"
              className="px-3 py-2 rounded-md border border-den-border"
              onClick={() => setShowNew(false)}
            >
              Cancel
            </button>
          </div>
          {create.isError && (
            <div className="md:col-span-4 text-sm text-den-danger">
              {(create.error as Error).message}
            </div>
          )}
        </form>
      )}

      {newDatabase && <DatabaseCallout db={newDatabase} onDismiss={() => setNewDatabase(null)} />}
      {lastToken && <PairingTokenCallout token={lastToken} onDismiss={() => setLastToken(null)} />}
      {removed && (
        <div className="rounded-lg border border-den-border bg-den-surface p-4 text-sm">
          <div className="flex items-start justify-between gap-4">
            <p>
              <strong>{removed.name}</strong> was removed.{' '}
              {removed.result.database_dropped ? (
                <>
                  Its database <span className="font-mono">{removed.result.database}</span> was deleted
                  {removed.result.backup_key ? (
                    <>
                      {' '}
                      after a backup; you can download it under{' '}
                      <Link to="/admin/backups" className="text-den-primary underline">
                        Backups
                      </Link>
                      .
                    </>
                  ) : (
                    ' without a backup (backups are turned off).'
                  )}
                </>
              ) : (
                'Its data was kept.'
              )}
            </p>
            <button type="button" className="text-xs text-den-foreground/60" onClick={() => setRemoved(null)}>
              Dismiss
            </button>
          </div>
        </div>
      )}

      <div className="bg-den-surface border border-den-border rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-den-muted text-left text-xs uppercase tracking-wide text-den-foreground/60">
            <tr>
              <th className="px-4 py-2">Module</th>
              <th className="px-4 py-2">Environment</th>
              <th className="px-4 py-2">Status</th>
              {ready && <th className="px-4 py-2">Data</th>}
              <th className="px-4 py-2 w-48">Actions</th>
            </tr>
          </thead>
          <tbody>
            {modules.data?.map((m: Module) => {
              const db = databases.get(m.database);
              return (
                <tr key={m.id} className="border-t border-den-border">
                  <td className="px-4 py-2">
                    <div className="font-medium">{m.display_name}</div>
                    <div className="font-mono text-xs text-den-foreground/60">{m.slug}</div>
                  </td>
                  <td className="px-4 py-2">{m.environment}</td>
                  <td className="px-4 py-2">
                    <span
                      className={cn(
                        'inline-block rounded-full px-2 py-0.5 text-xs',
                        m.status === 'paired'
                          ? 'bg-den-success-bg text-den-success'
                          : m.status === 'unpaired'
                            ? 'bg-den-warn-bg text-den-warn'
                            : 'bg-den-muted text-den-foreground/70',
                      )}
                    >
                      {m.status}
                    </span>
                  </td>
                  {ready && (
                    <td className="px-4 py-2 text-xs">
                      {db ? (
                        <span className="inline-flex items-center gap-1">
                          <Database size={12} />
                          <span className="font-mono">{db.name}</span> · {formatBytes(db.size_bytes)}
                        </span>
                      ) : (
                        <span className="text-den-foreground/50">No database here</span>
                      )}
                    </td>
                  )}
                  <td className="px-4 py-2 space-x-3">
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 text-den-primary hover:text-den-primary-hover"
                      onClick={() => mint.mutate(m.id)}
                      disabled={mint.isPending}
                    >
                      <Key size={14} /> Pair
                    </button>
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 text-den-danger hover:opacity-80"
                      onClick={() => setRemoving(m)}
                    >
                      <Trash2 size={14} /> Remove
                    </button>
                  </td>
                </tr>
              );
            })}
            {modules.data?.length === 0 && (
              <tr>
                <td colSpan={5} className="px-4 py-6 text-center text-den-foreground/60">
                  No modules yet. Click “New module” to add one.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {removing && (
        <RemoveModuleDialog
          module={removing}
          database={managed ? databases.get(removing.database) : undefined}
          backupsEnabled={!!ready?.backups_enabled}
          onClose={() => setRemoving(null)}
          onRemoved={(result) => {
            setRemoved({ name: removing.display_name, result });
            if (newDatabase?.name === removing.database) setNewDatabase(null);
            if (lastToken?.module_id === removing.id) setLastToken(null);
            setRemoving(null);
            void qc.invalidateQueries({ queryKey: ['modules'] });
            void qc.invalidateQueries({ queryKey: ['operations-status'] });
            void qc.invalidateQueries({ queryKey: ['backups'] });
          }}
        />
      )}
    </div>
  );
}

/**
 * Two warnings before data is deleted: first the choice (delete the data or
 * keep it), then typing the module's short name.
 */
function RemoveModuleDialog({
  module: m,
  database,
  backupsEnabled,
  onClose,
  onRemoved,
}: {
  module: Module;
  database?: ServerDatabase;
  backupsEnabled: boolean;
  onClose: () => void;
  onRemoved: (r: RemoveModuleResult) => void;
}) {
  const canDrop = !!database && !database.protected;
  const [choice, setChoice] = useState<'drop' | 'keep'>(canDrop ? 'drop' : 'keep');
  const [step, setStep] = useState<1 | 2>(1);
  const [typed, setTyped] = useState('');
  const remove = useMutation({
    mutationFn: () => deleteModule(m.id, choice === 'drop' ? { confirm: typed } : undefined),
    onSuccess: onRemoved,
  });

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-modal="true">
      <div className="w-full max-w-lg space-y-4 rounded-lg border border-den-border bg-den-surface p-5 text-sm shadow-xl">
        {step === 1 ? (
          <>
            <h2 className="font-heading text-lg font-bold">Remove {m.display_name}?</h2>
            <p className="text-den-foreground/80">
              It is disconnected from Den straight away: it stops receiving branding and settings and its pairing is
              revoked. Uploaded files are not touched.
            </p>
            <div className="space-y-2">
              {canDrop && (
                <label
                  className={cn(
                    'flex cursor-pointer items-start gap-2 rounded-md border p-3',
                    choice === 'drop' ? 'border-den-danger-border bg-den-danger-bg' : 'border-den-border',
                  )}
                >
                  <input type="radio" className="mt-1" checked={choice === 'drop'} onChange={() => setChoice('drop')} />
                  <span>
                    <span className="font-medium">Remove and delete its data</span>
                    <span className="block text-den-foreground/70">
                      Its database <span className="font-mono">{database.name}</span> (
                      {formatBytes(database.size_bytes)}) and everything in it is permanently deleted.
                      {backupsEnabled ? ' A backup is made first.' : ' Backups are turned off, so no backup is made.'}
                    </span>
                  </span>
                </label>
              )}
              <label
                className={cn(
                  'flex cursor-pointer items-start gap-2 rounded-md border p-3',
                  choice === 'keep' ? 'border-den-primary' : 'border-den-border',
                )}
              >
                <input type="radio" className="mt-1" checked={choice === 'keep'} onChange={() => setChoice('keep')} />
                <span>
                  <span className="font-medium">Remove only, keep its data</span>
                  <span className="block text-den-foreground/70">
                    {database
                      ? 'The database stays on the server, so the module can be added and paired again later.'
                      : 'Den has no database for this module; whatever it stores is left alone.'}
                  </span>
                </span>
              </label>
            </div>
            <div className="flex justify-end gap-2">
              <button type="button" className="rounded-md border border-den-border px-3 py-2" onClick={onClose}>
                Cancel
              </button>
              <button
                type="button"
                className="rounded-md bg-den-danger px-3 py-2 text-white hover:opacity-90"
                onClick={() => (choice === 'drop' ? setStep(2) : remove.mutate())}
                disabled={remove.isPending}
              >
                {choice === 'drop' ? 'Continue' : remove.isPending ? 'Removing…' : `Remove ${m.display_name}`}
              </button>
            </div>
          </>
        ) : (
          <>
            <h2 className="flex items-center gap-2 font-heading text-lg font-bold text-den-danger">
              <AlertTriangle size={18} /> Delete {m.display_name}&apos;s data?
            </h2>
            <p className="text-den-foreground/80">
              This is the second and last warning. The database <span className="font-mono">{database?.name}</span>{' '}
              is deleted and anyone still using {m.display_name} is disconnected.{' '}
              {backupsEnabled
                ? 'The backup made first is the only way to get the data back.'
                : 'There is no backup: the data is gone for good.'}
            </p>
            <label className="block">
              Type <span className="font-mono font-semibold">{m.slug}</span> to confirm
              <input
                className="mt-1 w-full rounded-md border border-den-border bg-den-surface px-2 py-1 font-mono"
                value={typed}
                autoComplete="off"
                data-1p-ignore="true"
                data-lpignore="true"
                onChange={(e) => setTyped(e.target.value)}
              />
            </label>
            {remove.isError && <p className="text-den-danger">{(remove.error as Error).message}</p>}
            <div className="flex justify-end gap-2">
              <button
                type="button"
                className="rounded-md border border-den-border px-3 py-2"
                onClick={onClose}
                disabled={remove.isPending}
              >
                Cancel
              </button>
              <button
                type="button"
                className="inline-flex items-center gap-1.5 rounded-md bg-den-danger px-3 py-2 text-white hover:opacity-90 disabled:opacity-50"
                disabled={typed !== m.slug || remove.isPending}
                onClick={() => remove.mutate()}
              >
                {remove.isPending && <Loader2 size={14} className="animate-spin" />}
                {remove.isPending
                  ? backupsEnabled
                    ? 'Backing up and deleting…'
                    : 'Deleting…'
                  : `Delete ${m.display_name} and its data`}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function CopyButton({ text }: { text: string }) {
  return (
    <button
      type="button"
      onClick={() => void navigator.clipboard.writeText(text)}
      className="inline-flex items-center gap-1 bg-den-warn-border hover:opacity-90 text-den-warn px-3 py-1.5 rounded-md text-sm"
    >
      <Copy size={14} /> Copy
    </button>
  );
}

function DatabaseCallout({ db, onDismiss }: { db: CreatedDatabase; onDismiss: () => void }) {
  return (
    <div className="rounded-lg border border-den-warn-border bg-den-warn-bg p-4">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="font-medium text-den-warn">Database created: {db.name}</div>
          <div className="text-xs text-den-warn mt-1">
            Shown only once. Put this into the module&apos;s configuration as <code>DATABASE_URL</code>, start the
            module, then pair it.
          </div>
          <code className="mt-3 block break-all rounded bg-den-surface p-2 text-xs">{db.database_url}</code>
        </div>
        <div className="flex flex-col gap-2">
          <CopyButton text={db.database_url} />
          <button type="button" onClick={onDismiss} className="text-xs text-den-warn hover:opacity-80">
            Dismiss
          </button>
        </div>
      </div>
    </div>
  );
}

function PairingTokenCallout({
  token,
  onDismiss,
}: {
  token: PairingToken;
  onDismiss: () => void;
}) {
  return (
    <div className="rounded-lg border border-den-warn-border bg-den-warn-bg p-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="font-medium text-den-warn">One-time pairing URL</div>
          <div className="text-xs text-den-warn mt-1">
            This URL is shown exactly once. Paste it into the module’s “Den connection” settings within 15 minutes.
          </div>
          <code className="mt-3 block break-all rounded bg-den-surface p-2 text-xs">
            {token.pairing_url}
          </code>
          <div className="text-xs text-den-warn mt-1">
            Expires {new Date(token.expires_at).toLocaleString()}
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <CopyButton text={token.pairing_url} />
          <button
            type="button"
            onClick={onDismiss}
            className="text-xs text-den-warn hover:opacity-80"
          >
            Dismiss
          </button>
        </div>
      </div>
    </div>
  );
}

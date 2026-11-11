import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';
import { AlertTriangle, Loader2 } from 'lucide-react';

import { deleteModule, type Module, type RemoveModuleResult } from '@/api/modules';
import { formatBytes, type ServerDatabase } from '@/api/operations';
import { cn } from '@/lib/cn';
import { canDropDatabase } from '@/lib/modules';

/**
 * Two warnings before data is deleted: first the choice (delete the data or
 * keep it), then typing the module's short name.
 */
export function RemoveModuleDialog({
  module: m,
  database,
  backupsEnabled,
  ops,
  onClose,
  onRemoved,
}: {
  module: Module;
  database?: ServerDatabase;
  backupsEnabled: boolean;
  /** The person has the ops role (needed to delete a database). */
  ops: boolean;
  onClose: () => void;
  onRemoved: (r: RemoveModuleResult) => void;
}) {
  const canDrop = canDropDatabase(m, database, ops);
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
                      Its database <span className="font-mono">{database?.name}</span> (
                      {formatBytes(database?.size_bytes)}) and everything in it is permanently deleted.
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
                  {database && !canDrop && (
                    <span className="mt-1 block text-xs text-den-foreground/60" data-testid="no-drop-reason">
                      {!m.database_created_by_den
                        ? 'Den did not create this database, so it never deletes it.'
                        : !ops
                          ? 'Deleting the database needs the ops role.'
                          : 'This database is protected.'}
                    </span>
                  )}
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


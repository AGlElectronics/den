import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { KeyRound, Loader2, ShieldOff, X } from 'lucide-react';

import { listCredentials, revokeCredentials, type Module, type ModuleCredential } from '@/api/modules';
import { cn } from '@/lib/cn';

function when(ts: string | null) {
  return ts ? new Date(ts).toLocaleString() : '—';
}

/**
 * A module's pairing credentials. Revoking cuts the module off from Den at
 * once; it stays disconnected until an admin pairs it again (never automatic).
 */
export function ModuleCredentials({ module: m, onClose }: { module: Module; onClose: () => void }) {
  const qc = useQueryClient();
  const creds = useQuery({ queryKey: ['credentials', m.id], queryFn: () => listCredentials(m.id) });
  const [confirmAll, setConfirmAll] = useState(false);
  const revoke = useMutation({
    mutationFn: (id?: string) => revokeCredentials(m.id, id),
    onSuccess: () => {
      setConfirmAll(false);
      void qc.invalidateQueries({ queryKey: ['credentials', m.id] });
      void qc.invalidateQueries({ queryKey: ['modules'] });
    },
  });
  const active = (creds.data ?? []).filter((c) => !c.revoked_at);

  return (
    <section className="rounded-lg border border-den-border bg-den-surface p-4 text-sm" aria-label={`Credentials of ${m.display_name}`}>
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="flex items-center gap-2 font-heading text-base font-bold">
            <KeyRound size={16} /> Credentials of {m.display_name}
          </h2>
          <p className="mt-1 text-den-foreground/70">
            A module gets a credential when it is paired; pairing again replaces every older one. Revoke a credential
            when it may have leaked: the module is disconnected straight away and stays so until you pair it again.
          </p>
        </div>
        <button type="button" onClick={onClose} className="text-den-foreground/60 hover:text-den-foreground" aria-label="Close">
          <X size={16} />
        </button>
      </div>

      {creds.isLoading ? (
        <p className="mt-3 text-den-foreground/60">Loading…</p>
      ) : creds.isError ? (
        <p className="mt-3 text-den-danger">{(creds.error as Error).message}</p>
      ) : creds.data?.length === 0 ? (
        <p className="mt-3 text-den-foreground/60">No credentials yet: this module has never been paired.</p>
      ) : (
        <table className="mt-3 w-full text-xs">
          <thead className="text-left text-den-foreground/60">
            <tr>
              <th className="py-1 pr-2 font-medium">Label</th>
              <th className="py-1 pr-2 font-medium">Created</th>
              <th className="py-1 pr-2 font-medium">Last used</th>
              <th className="py-1 pr-2 font-medium">State</th>
              <th className="py-1 font-medium" />
            </tr>
          </thead>
          <tbody>
            {creds.data?.map((c: ModuleCredential) => (
              <tr key={c.id} className="border-t border-den-border">
                <td className="py-1.5 pr-2 font-mono">{c.label || c.id}</td>
                <td className="py-1.5 pr-2">{when(c.created_at)}</td>
                <td className="py-1.5 pr-2">{when(c.last_used_at)}</td>
                <td className={cn('py-1.5 pr-2', c.revoked_at ? 'text-den-foreground/50' : 'text-den-success')}>
                  {c.revoked_at ? `Revoked ${when(c.revoked_at)}` : 'Active'}
                </td>
                <td className="py-1.5 text-right">
                  {!c.revoked_at && (
                    <button
                      type="button"
                      className="text-den-danger hover:opacity-80 disabled:opacity-50"
                      disabled={revoke.isPending}
                      onClick={() => revoke.mutate(c.id)}
                    >
                      Revoke
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {active.length > 0 && (
        <div className="mt-3 flex items-center gap-2">
          {confirmAll ? (
            <>
              <span className="text-den-danger">Disconnect {m.display_name} from Den now?</span>
              <button
                type="button"
                className="inline-flex items-center gap-1 rounded-md bg-den-danger px-3 py-1.5 text-white disabled:opacity-50"
                disabled={revoke.isPending}
                onClick={() => revoke.mutate(undefined)}
              >
                {revoke.isPending && <Loader2 size={12} className="animate-spin" />} Revoke all
              </button>
              <button type="button" className="rounded-md border border-den-border px-3 py-1.5" onClick={() => setConfirmAll(false)}>
                Cancel
              </button>
            </>
          ) : (
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded-md border border-den-danger-border px-3 py-1.5 text-den-danger"
              onClick={() => setConfirmAll(true)}
            >
              <ShieldOff size={14} /> Revoke all credentials
            </button>
          )}
        </div>
      )}
      {revoke.isError && <p className="mt-2 text-den-danger">{(revoke.error as Error).message}</p>}
      {revoke.data && revoke.data.module_status === 'unpaired' && (
        <p className="mt-2 text-den-warn">
          {m.display_name} is now unpaired. Click <strong>Pair</strong> and paste the new URL into the module to connect it
          again.
        </p>
      )}
    </section>
  );
}

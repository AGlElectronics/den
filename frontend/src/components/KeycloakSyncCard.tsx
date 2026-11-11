import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { getKeycloakSync, syncKeycloakNow } from '@/api/branding';

/** How the Keycloak sign-in page gets the brand, and the optional display-name sync. */
export function KeycloakSyncCard() {
  const qc = useQueryClient();
  const status = useQuery({ queryKey: ['keycloak-sync'], queryFn: getKeycloakSync, refetchInterval: 15_000 });
  const sync = useMutation({
    mutationFn: syncKeycloakNow,
    onSuccess: (data) => qc.setQueryData(['keycloak-sync'], data),
  });
  const s = status.data;

  return (
    <section className="rounded-lg border border-den-border bg-den-surface p-4 text-sm">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="font-heading text-base font-bold">Sign-in page (Keycloak)</h2>
          <p className="text-den-foreground/70">
            The <code>conservices</code> login theme fetches the organisation name, tagline, logo, favicon, colours and
            sign-in background from Den each time the page opens, so changes show up straight away.
          </p>
        </div>
        {s?.enabled && (
          <button
            type="button"
            className="shrink-0 rounded-md border border-den-border px-3 py-1.5 font-medium hover:bg-den-muted disabled:opacity-50"
            disabled={sync.isPending}
            onClick={() => sync.mutate()}
          >
            {sync.isPending ? 'Syncing…' : 'Sync name now'}
          </button>
        )}
      </div>
      <div className="mt-2" data-testid="keycloak-sync-status">
        {status.isLoading ? (
          <p className="text-den-foreground/60">Loading…</p>
        ) : !s?.enabled ? (
          <p className="text-den-foreground/70">
            Realm name sync is off.{s?.message ? ` ${s.message}` : ''}
          </p>
        ) : s.last_error ? (
          <p className="text-den-danger">Last realm name sync failed: {s.last_error}</p>
        ) : s.synced_at ? (
          <p className="text-den-foreground/70">
            Realm <span className="font-mono">{s.realm}</span> is named <strong>{s.display_name}</strong> (used in
            Keycloak&apos;s e-mails), synced {new Date(s.synced_at).toLocaleString()}.
          </p>
        ) : (
          <p className="text-den-foreground/70">Waiting for the first realm name sync…</p>
        )}
      </div>
    </section>
  );
}

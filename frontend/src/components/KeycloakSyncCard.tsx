import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { getKeycloakSync, syncKeycloakNow } from '@/api/branding';

/** Status of the brand push into Keycloak (the sign-in page). */
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
            Den pushes the organisation name, tagline, logo, favicon, colours and sign-in background into Keycloak whenever branding or
            organisation settings change, and re-checks every few minutes.
          </p>
        </div>
        {s?.enabled && (
          <button
            type="button"
            className="shrink-0 rounded-md border border-den-border px-3 py-1.5 font-medium hover:bg-den-muted disabled:opacity-50"
            disabled={sync.isPending}
            onClick={() => sync.mutate()}
          >
            {sync.isPending ? 'Syncing…' : 'Sync now'}
          </button>
        )}
      </div>
      <div className="mt-2">
        {status.isLoading ? (
          <p className="text-den-foreground/60">Loading…</p>
        ) : !s?.enabled ? (
          <p className="text-den-foreground/70">
            Not connected. Set <code>KEYCLOAK_ADMIN_URL</code> and <code>KEYCLOAK_CLIENT_SECRET</code> on the Den backend.
          </p>
        ) : s.last_error ? (
          <p className="text-den-danger">Last sync failed: {s.last_error}</p>
        ) : s.synced_at ? (
          <p className="text-den-foreground/70">
            Realm <span className="font-mono">{s.realm}</span> is on revision {s.revision}, synced{' '}
            {new Date(s.synced_at).toLocaleString()}.
          </p>
        ) : (
          <p className="text-den-foreground/70">Waiting for the first sync…</p>
        )}
      </div>
    </section>
  );
}

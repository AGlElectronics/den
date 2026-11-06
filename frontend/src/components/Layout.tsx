import { Link, Outlet } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { LogOut, Shield } from 'lucide-react';

import { fetchDashboard } from '@/api/dashboard';
import { useMe } from '@/hooks/useMe';
import { isOrgAdmin } from '@/lib/roles';
import { useApplyBrand } from '@/lib/brand';
import { useThemeSync } from '@/lib/theme';
import { isOidcAuthMode, signOutOidc } from '@/auth/oidc';
import { ThemeToggle } from '@/components/ThemeToggle';

/** Member portal shell: a slim top bar, no admin tooling. */
export function Layout() {
  const me = useMe();
  const admin = isOrgAdmin(me.data);
  const dash = useQuery({ queryKey: ['dashboard'], queryFn: fetchDashboard });
  useApplyBrand(dash.data?.brand);
  useThemeSync('/api/v1/me/preferences');

  const convention =
    dash.data?.copy?.convention_name || dash.data?.organization?.display_name || 'Den';

  if (me.data && !me.data.memberships?.length) {
    return <NoPortalAccess name={me.data.display_name} />;
  }

  return (
    <div className="min-h-screen bg-den-muted">
      <header className="sticky top-0 z-10 border-b border-den-border bg-den-surface/90 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-5xl items-center gap-4 px-4 sm:px-0">
          <Link to="/dashboard" className="font-heading text-lg font-extrabold text-den-foreground hover:text-den-primary">
            {convention}
          </Link>
          <div className="ml-auto flex items-center gap-3 text-sm">
            {admin ? (
              <Link
                to="/admin"
                className="inline-flex items-center gap-1.5 rounded-full border border-den-border px-3 py-1.5 text-xs font-semibold text-den-foreground hover:border-den-foreground"
              >
                <Shield size={13} /> Admin console
              </Link>
            ) : null}
            <ThemeToggle compact />
            <span className="hidden text-den-foreground/70 sm:inline">{me.data?.display_name ?? '…'}</span>
            {isOidcAuthMode() && (
              <button
                type="button"
                onClick={() => void signOutOidc()}
                className="inline-flex items-center gap-1 text-den-foreground/60 hover:text-den-primary"
                title="Sign out"
              >
                <LogOut size={14} />
              </button>
            )}
          </div>
        </div>
      </header>
      <main className="px-4 py-8 sm:px-6">
        <Outlet />
      </main>
    </div>
  );
}

/** Signed in, but without the portal's member role. */
function NoPortalAccess({ name }: { name?: string }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-den-muted px-4">
      <div className="w-full max-w-md rounded-2xl border border-den-border bg-den-surface p-8 text-center shadow-sm">
        <h1 className="font-heading text-xl font-extrabold text-den-foreground">You don&apos;t have access to the portal</h1>
        <p className="mt-3 text-sm text-den-foreground/70">
          {name ? <>You&apos;re signed in as <strong>{name}</strong>, but </> : 'Your account '}
          isn&apos;t allowed to use the portal. If you think this is a mistake, contact the team that set up your account.
        </p>
        {isOidcAuthMode() && (
          <button
            type="button"
            onClick={() => void signOutOidc()}
            className="mt-6 inline-flex items-center gap-1.5 rounded-full bg-den-primary px-5 py-2 text-sm font-semibold text-den-on-primary hover:opacity-90"
          >
            <LogOut size={14} /> Sign out
          </button>
        )}
      </div>
    </div>
  );
}

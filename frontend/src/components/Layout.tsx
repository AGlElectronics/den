import { Link, Outlet } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { LogOut, Shield } from 'lucide-react';

import { fetchDashboard } from '@/api/dashboard';
import { useMe } from '@/hooks/useMe';
import { isOrgAdmin } from '@/lib/roles';
import { useApplyBrand } from '@/lib/brand';
import { isOidcAuthMode, signOutOidc } from '@/auth/oidc';

/** Member portal shell: a light top bar, no admin tooling. */
export function Layout() {
  const me = useMe();
  const admin = isOrgAdmin(me.data);
  const dash = useQuery({ queryKey: ['dashboard'], queryFn: fetchDashboard });
  useApplyBrand(dash.data?.brand);

  const convention =
    dash.data?.copy?.convention_name || dash.data?.organization?.display_name || 'Den';

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

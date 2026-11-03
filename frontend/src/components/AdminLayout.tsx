import { Link, Outlet, useRouterState } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { ArrowLeft, Boxes, Building2, Gauge, LogOut, Palette, Settings, Shield } from 'lucide-react';

import { fetchDashboard } from '@/api/dashboard';
import { useMe } from '@/hooks/useMe';
import { useApplyBrand } from '@/lib/brand';
import { cn } from '@/lib/cn';
import { isOidcAuthMode, signOutOidc } from '@/auth/oidc';

const NAV = [
  { to: '/admin', label: 'Overview', icon: Gauge, exact: true },
  { to: '/admin/modules', label: 'Modules', icon: Boxes },
  { to: '/admin/branding', label: 'Branding', icon: Palette },
  { to: '/admin/organization', label: 'Organisation', icon: Building2 },
  { to: '/admin/settings', label: 'Settings', icon: Settings },
] as const;

/**
 * Admin console shell. Deliberately looks different from the member portal
 * (dark rail + banner) so it's always obvious changes affect everyone.
 */
export function AdminLayout() {
  const me = useMe();
  const path = useRouterState({ select: (s) => s.location.pathname });
  const dash = useQuery({ queryKey: ['dashboard'], queryFn: fetchDashboard });
  useApplyBrand(dash.data?.brand);

  const convention =
    dash.data?.copy?.convention_name || dash.data?.organization?.display_name || 'Den';

  return (
    <div className="min-h-screen grid grid-cols-[15rem_1fr] bg-slate-50">
      <aside className="sticky top-0 flex h-screen flex-col gap-6 overflow-y-auto bg-slate-900 p-5 text-slate-200">
        <div>
          <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-[0.16em] text-amber-300">
            <Shield size={13} /> Admin console
          </div>
          <div className="mt-1 truncate font-heading text-lg font-bold text-white">{convention}</div>
        </div>

        <nav className="flex flex-col gap-1">
          {NAV.map(({ to, label, icon: Icon, ...rest }) => {
            const exact = 'exact' in rest && rest.exact;
            const active = exact ? path === to : path === to || path.startsWith(`${to}/`);
            return (
              <Link
                key={to}
                to={to}
                activeOptions={{ exact: !!exact }}
                className={cn(
                  'flex items-center gap-2 rounded-md px-3 py-2 text-sm',
                  active ? 'bg-white/10 font-medium text-white' : 'text-slate-300 hover:bg-white/5 hover:text-white',
                )}
              >
                <Icon size={16} />
                {label}
              </Link>
            );
          })}
        </nav>

        <div className="mt-auto space-y-3 text-xs text-slate-400">
          <Link
            to="/dashboard"
            className="flex items-center gap-1.5 rounded-md border border-white/10 px-3 py-2 text-slate-200 hover:bg-white/5 hover:text-white"
          >
            <ArrowLeft size={13} /> Back to portal
          </Link>
          <div>
            <div className="truncate text-slate-200">{me.data?.display_name ?? '…'}</div>
            <div className="truncate">{me.data?.email}</div>
          </div>
          {isOidcAuthMode() && (
            <button
              type="button"
              onClick={() => void signOutOidc()}
              className="inline-flex items-center gap-1 text-slate-300 hover:text-white"
            >
              <LogOut size={12} /> Sign out
            </button>
          )}
        </div>
      </aside>

      <div className="flex min-w-0 flex-col">
        <div className="border-b border-amber-200 bg-amber-50 px-8 py-2 text-xs text-amber-900">
          You&apos;re in the admin console — changes here apply to everyone in {convention}.
        </div>
        <main className="p-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

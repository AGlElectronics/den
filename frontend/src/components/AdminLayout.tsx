import { Link, Outlet, useRouterState } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import {
  AlertTriangle,
  ArrowLeft,
  Boxes,
  Building2,
  DatabaseBackup,
  ExternalLink,
  Gauge,
  KeyRound,
  LogOut,
  Palette,
  ScrollText,
  Settings,
  Shield,
  SlidersHorizontal,
} from 'lucide-react';

import { fetchDashboard } from '@/api/dashboard';
import { backupProblem, backupWarning, getOperationsStatus } from '@/api/operations';
import { useMe } from '@/hooks/useMe';
import { useApplyBrand } from '@/lib/brand';
import { cn } from '@/lib/cn';
import { useThemeSync } from '@/lib/theme';
import { isOidcAuthMode, signOutOidc } from '@/auth/oidc';
import { ThemeToggle } from '@/components/ThemeToggle';

const NAV = [
  { to: '/admin', label: 'Overview', icon: Gauge, exact: true },
  { to: '/admin/modules', label: 'Modules', icon: Boxes },
  { to: '/admin/branding', label: 'Branding', icon: Palette },
  { to: '/admin/organization', label: 'Organisation', icon: Building2 },
  { to: '/admin/module-settings', label: 'Module settings', icon: SlidersHorizontal },
  { to: '/admin/backups', label: 'Backups', icon: DatabaseBackup },
  { to: '/admin/audit', label: 'Audit log', icon: ScrollText },
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
  useThemeSync('/api/v1/me/preferences');
  const ops = useQuery({ queryKey: ['operations-status'], queryFn: getOperationsStatus, refetchInterval: 60_000 });
  const problem = backupProblem(ops.data);
  const warning = problem ? undefined : backupWarning(ops.data);
  const links = dash.data?.identity_links;
  const idpConsole = links?.idp_admin_console_url;

  const convention =
    dash.data?.copy?.convention_name || dash.data?.organization?.display_name || 'Den';

  return (
    <div className="min-h-screen grid grid-cols-[15rem_1fr] bg-den-canvas text-den-foreground">
      <aside className="sticky top-0 flex h-screen flex-col gap-6 overflow-y-auto bg-slate-900 p-5 text-slate-200 dark:border-r dark:border-white/10 dark:bg-black/30">
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
                {to === '/admin/backups' && (problem || warning) && (
                  <AlertTriangle size={14} className="ml-auto text-amber-300" aria-label="Needs attention" />
                )}
              </Link>
            );
          })}
          {idpConsole && (
            <a
              href={idpConsole}
              target="_blank"
              rel="noopener noreferrer"
              title="Accounts, groups and sign-in settings (Module settings → Identity links)."
              className="flex items-center gap-2 rounded-md px-3 py-2 text-sm text-slate-300 hover:bg-white/5 hover:text-white"
            >
              <KeyRound size={16} />
              {links?.sso_name ? `${links.sso_name} admin` : 'Identity provider'}
              <ExternalLink size={12} className="ml-auto opacity-60" />
            </a>
          )}
        </nav>

        <div className="mt-auto space-y-3 text-xs text-slate-400">
          <ThemeToggle tone="rail" />
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
        <div className="border-b border-den-warn-border bg-den-warn-bg px-8 py-2 text-xs text-den-warn">
          You&apos;re in the admin console — changes here apply to everyone in {convention}.
        </div>
        {problem && !path.startsWith('/admin/backups') && (
          <div className="flex items-center gap-2 border-b border-den-danger-border bg-den-danger-bg px-8 py-2 text-xs text-den-danger">
            <AlertTriangle size={13} className="shrink-0" />
            <span>{problem}</span>
            <Link to="/admin/backups" className="ml-auto font-medium underline">
              Open Backups
            </Link>
          </div>
        )}
        {warning && !path.startsWith('/admin/backups') && (
          <div className="flex items-center gap-2 border-b border-den-warn-border bg-den-warn-bg px-8 py-2 text-xs text-den-warn">
            <AlertTriangle size={13} className="shrink-0" />
            <span>{warning}</span>
            <Link to="/admin/backups" className="ml-auto font-medium underline">
              Open Backups
            </Link>
          </div>
        )}
        <main className="p-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

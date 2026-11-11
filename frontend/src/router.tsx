import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  redirect,
} from '@tanstack/react-router';

import { Layout } from '@/components/Layout';
import { AdminLayout } from '@/components/AdminLayout';
import { getUserManager, isOidcAuthMode } from '@/auth/oidc';
import { DashboardPage } from '@/pages/DashboardPage';
import { OrganizePage } from '@/pages/OrganizePage';
import { ModulesPage } from '@/pages/ModulesPage';
import { BrandingPage } from '@/pages/BrandingPage';
import { OrgSettingsPage } from '@/pages/OrgSettingsPage';
import { SettingsPage } from '@/pages/SettingsPage';
import { BackupsPage } from '@/pages/BackupsPage';
import { AuditLogPage } from '@/pages/AuditLogPage';
import { ModuleSettingsPage } from '@/pages/ModuleSettingsPage';
import { LoginPage } from '@/pages/LoginPage';
import { OidcCallbackPage } from '@/pages/OidcCallbackPage';
import { fetchMe } from '@/api/me';
import { isOrgAdmin } from '@/lib/roles';

const rootRoute = createRootRoute({
  component: () => <Outlet />,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  validateSearch: (search: Record<string, unknown>): { redirect: string } => {
    const r = search.redirect;
    if (typeof r === 'string' && r.startsWith('/')) {
      return { redirect: r };
    }
    return { redirect: '/dashboard' };
  },
  component: LoginPage,
});

const oidcCallbackRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/auth/callback',
  component: OidcCallbackPage,
});

async function requireSession() {
  // Dev mode: the API accepts X-Dev-User so nothing to gate on the SPA
  // side. OIDC mode: redirect to /login unless we have a non-expired user.
  if (!isOidcAuthMode()) {
    return;
  }
  const mgr = getUserManager();
  if (!mgr) {
    throw redirect({ to: '/login', search: { redirect: '/dashboard' } });
  }
  let user = await mgr.getUser();
  if (user?.expired) {
    try {
      user = await mgr.signinSilent();
    } catch {
      user = null;
    }
  }
  if (!user) {
    const path =
      typeof window !== 'undefined' ? `${window.location.pathname}${window.location.search}` : '/dashboard';
    const redirectTo = path.startsWith('/login') || path.startsWith('/auth/') ? '/dashboard' : path;
    throw redirect({ to: '/login', search: { redirect: redirectTo } });
  }
}

const appLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'app',
  component: Layout,
  beforeLoad: requireSession,
});

const indexRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/dashboard' });
  },
});

async function requireAdmin() {
  await requireSession();
  try {
    const me = await fetchMe();
    if (!isOrgAdmin(me)) {
      throw redirect({ to: '/dashboard' });
    }
  } catch (err) {
    if (err && typeof err === 'object' && 'to' in err) throw err;
    throw redirect({ to: '/dashboard' });
  }
}

const dashboardRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/dashboard',
  component: DashboardPage,
});

// Legacy member-shell paths → admin console.
const legacyRedirects = (
  [
    ['/organize', '/admin'],
    ['/modules', '/admin/modules'],
    ['/branding', '/admin/branding'],
    ['/settings', '/admin/settings'],
  ] as const
).map(([from, to]) =>
  createRoute({
    getParentRoute: () => appLayoutRoute,
    path: from,
    beforeLoad: () => {
      throw redirect({ to });
    },
  }),
);

const adminLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'admin',
  component: AdminLayout,
  beforeLoad: requireAdmin,
});

const adminOverviewRoute = createRoute({
  getParentRoute: () => adminLayoutRoute,
  path: '/admin',
  component: OrganizePage,
});

const adminModulesRoute = createRoute({
  getParentRoute: () => adminLayoutRoute,
  path: '/admin/modules',
  component: ModulesPage,
});

const adminBrandingRoute = createRoute({
  getParentRoute: () => adminLayoutRoute,
  path: '/admin/branding',
  component: BrandingPage,
});

const adminOrgSettingsRoute = createRoute({
  getParentRoute: () => adminLayoutRoute,
  path: '/admin/organization',
  component: OrgSettingsPage,
});

const adminBackupsRoute = createRoute({
  getParentRoute: () => adminLayoutRoute,
  path: '/admin/backups',
  component: BackupsPage,
});

const adminModuleSettingsRoute = createRoute({
  getParentRoute: () => adminLayoutRoute,
  path: '/admin/module-settings',
  component: ModuleSettingsPage,
});

const adminAuditRoute = createRoute({
  getParentRoute: () => adminLayoutRoute,
  path: '/admin/audit',
  component: AuditLogPage,
});

const adminSettingsRoute = createRoute({
  getParentRoute: () => adminLayoutRoute,
  path: '/admin/settings',
  component: SettingsPage,
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  oidcCallbackRoute,
  appLayoutRoute.addChildren([indexRoute, dashboardRoute, ...legacyRedirects]),
  adminLayoutRoute.addChildren([
    adminOverviewRoute,
    adminModulesRoute,
    adminBrandingRoute,
    adminOrgSettingsRoute,
    adminBackupsRoute,
    adminModuleSettingsRoute,
    adminAuditRoute,
    adminSettingsRoute,
  ]),
]);

export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}

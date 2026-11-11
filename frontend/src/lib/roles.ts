import type { Me } from '@/api/me';

const ADMIN_ROLES = new Set(['admin', 'owner']);

function roles(me: Me | undefined | null): string[] {
  if (!me) return [];
  return me.roles ?? me.memberships?.map((m) => m.role) ?? [];
}

/** True when the user is a convention admin (or owner). */
export function isOrgAdmin(me: Me | undefined | null): boolean {
  return roles(me).some((r) => ADMIN_ROLES.has(String(r).toLowerCase()));
}

/** True when the user may run backups, download them and drop module databases. */
export function isOps(me: Me | undefined | null): boolean {
  return roles(me).includes('ops');
}

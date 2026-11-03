import type { Me } from '@/api/me';

const ADMIN_ROLES = new Set(['admin', 'owner', 'organizer']);

/** True when the user holds an org admin/owner role on any membership. */
export function isOrgAdmin(me: Me | undefined | null): boolean {
  if (!me?.memberships?.length) return false;
  return me.memberships.some((m) => ADMIN_ROLES.has(String(m.role).toLowerCase()));
}

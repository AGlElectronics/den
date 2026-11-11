import { describe, expect, it } from 'vitest';

import type { Me } from '@/api/me';
import { isOps, isOrgAdmin } from '@/lib/roles';

const me = (roles: string[], memberships = roles): Me => ({
  id: 'u',
  display_name: 'U',
  email: '',
  issuer: 'i',
  subject: 's',
  memberships: memberships.map((role) => ({ organization_id: 'o', role })),
  roles,
});

describe('roles', () => {
  it('treats admin and owner as admins, organizer and member not', () => {
    expect(isOrgAdmin(me(['admin']))).toBe(true);
    expect(isOrgAdmin(me(['owner']))).toBe(true);
    expect(isOrgAdmin(me(['organizer']))).toBe(false);
    expect(isOrgAdmin(me(['member']))).toBe(false);
    expect(isOrgAdmin(undefined)).toBe(false);
  });

  it('reads ops from the roles list only', () => {
    expect(isOps(me(['admin', 'ops'], ['admin']))).toBe(true);
    expect(isOps(me(['admin']))).toBe(false);
  });

  it('falls back to memberships for older answers without roles', () => {
    const old = { ...me(['admin']), roles: undefined };
    expect(isOrgAdmin(old)).toBe(true);
    expect(isOps(old)).toBe(false);
  });
});

import { apiRequest } from '@/api/client';

export type AuditActor = { id: string; display_name: string; email?: string };

export type AuditEvent = {
  id: string;
  created_at: string;
  action: string;
  entity_type: string;
  entity_id: string;
  /** Null for the system (pre-registration, background syncs). */
  actor: AuditActor | null;
  note: string;
  before?: unknown;
  after?: unknown;
};

export type AuditPage = { items: AuditEvent[]; next_cursor?: string };

export type AuditFilters = {
  /** Exact action, or a prefix ending in "*", e.g. "module.*". */
  action?: string;
  entity_type?: string;
  entity_id?: string;
  since?: string;
  until?: string;
};

export function listAuditEvents(filters: AuditFilters, cursor?: string, limit = 50): Promise<AuditPage> {
  return apiRequest<AuditPage>('/api/v1/audit-events', {
    query: { ...filters, cursor, limit },
  });
}

/** Action groups offered in the filter. */
export const AUDIT_ACTION_GROUPS = [
  { value: '', label: 'Everything' },
  { value: 'module.*', label: 'Modules and credentials' },
  { value: 'pairing.*', label: 'Pairing' },
  { value: 'branding.*', label: 'Branding' },
  { value: 'org_settings.*', label: 'Organisation settings' },
  { value: 'module_settings.*', label: 'Module settings' },
  { value: 'operations.*', label: 'Backups and downloads' },
  { value: 'keycloak.*', label: 'Keycloak sync' },
] as const;

import { apiRequest } from '@/api/client';

export type PortalVisibility = 'role_holders' | 'everyone';

export type Module = {
  id: string;
  organization_id: string;
  slug: string;
  display_name: string;
  base_url: string;
  /** Address Den uses to call the module (empty: base_url). */
  internal_url: string;
  environment: string;
  enabled: boolean;
  status: 'unpaired' | 'paired' | 'degraded' | 'disabled';
  portal_visibility: PortalVisibility;
  manifest: Record<string, unknown>;
  last_seen_at?: string;
  created_at: string;
  updated_at: string;
  /** Database on the shared server (by name; it may not exist). */
  database: string;
  /** Only a database Den created can be deleted by Den. */
  database_created_by_den: boolean;
};

export type CreateModuleInput = {
  slug: string;
  display_name: string;
  base_url?: string;
  internal_url?: string;
  environment?: string;
  portal_visibility?: PortalVisibility;
  create_database?: boolean;
};

export type UpdateModuleInput = Partial<
  Pick<Module, 'display_name' | 'base_url' | 'internal_url' | 'environment' | 'enabled' | 'portal_visibility'>
>;

export type CreatedDatabase = {
  name: string;
  username: string;
  password: string;
  database_url: string;
};

export type RemoveModuleResult = {
  database_dropped: boolean;
  database?: string;
  backup_key?: string;
};

export type PairingToken = {
  token_id: string;
  module_id: string;
  token: string;
  pairing_url: string;
  expires_at: string;
};

export type ModuleCredential = {
  id: string;
  label: string;
  created_at: string;
  last_used_at: string | null;
  revoked_at: string | null;
};

export type RevokeResult = { revoked: number; module_status: Module['status'] };

export function listModules(): Promise<Module[]> {
  return apiRequest<Module[]>('/api/v1/modules');
}

export function createModule(input: CreateModuleInput): Promise<{ id: string; database?: CreatedDatabase }> {
  return apiRequest('/api/v1/modules', {
    method: 'POST',
    body: input,
  });
}

export function updateModule(id: string, input: UpdateModuleInput): Promise<{ id: string }> {
  return apiRequest(`/api/v1/modules/${id}`, { method: 'PATCH', body: input });
}

/** Unlinks a module; with dropDatabase its database is backed up and deleted. */
export function deleteModule(id: string, drop?: { confirm: string }): Promise<RemoveModuleResult> {
  return apiRequest<RemoveModuleResult>(`/api/v1/modules/${id}`, {
    method: 'DELETE',
    query: drop ? { database: 'drop', confirm: drop.confirm } : undefined,
  });
}

export function mintPairingToken(moduleId: string): Promise<PairingToken> {
  return apiRequest<PairingToken>(`/api/v1/modules/${moduleId}/pairing-tokens`, {
    method: 'POST',
    body: {},
  });
}

export function listCredentials(moduleId: string): Promise<ModuleCredential[]> {
  return apiRequest<ModuleCredential[]>(`/api/v1/modules/${moduleId}/credentials`);
}

/** Revokes one credential, or every active one when credentialId is omitted. */
export function revokeCredentials(moduleId: string, credentialId?: string): Promise<RevokeResult> {
  return apiRequest<RevokeResult>(
    `/api/v1/modules/${moduleId}/credentials${credentialId ? `/${credentialId}` : ''}`,
    { method: 'DELETE' },
  );
}

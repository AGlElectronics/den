import { apiRequest } from '@/api/client';

export type Module = {
  id: string;
  organization_id: string;
  slug: string;
  display_name: string;
  base_url: string;
  environment: string;
  enabled: boolean;
  status: 'unpaired' | 'paired' | 'degraded' | 'disabled';
  manifest: Record<string, unknown>;
  last_seen_at?: string;
  created_at: string;
  updated_at: string;
  /** Database on the shared server (by name; it may not exist). */
  database: string;
};

export type CreateModuleInput = {
  slug: string;
  display_name: string;
  base_url?: string;
  environment?: string;
  create_database?: boolean;
};

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

export function listModules(): Promise<Module[]> {
  return apiRequest<Module[]>('/api/v1/modules');
}

export function createModule(input: CreateModuleInput): Promise<{ id: string; database?: CreatedDatabase }> {
  return apiRequest('/api/v1/modules', {
    method: 'POST',
    body: input,
  });
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

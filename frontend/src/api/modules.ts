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
};

export type CreateModuleInput = {
  slug: string;
  display_name: string;
  base_url?: string;
  environment?: string;
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

export function createModule(input: CreateModuleInput): Promise<{ id: string }> {
  return apiRequest<{ id: string }>('/api/v1/modules', {
    method: 'POST',
    body: input,
  });
}

export function deleteModule(id: string): Promise<void> {
  return apiRequest<void>(`/api/v1/modules/${id}`, { method: 'DELETE' });
}

export function mintPairingToken(moduleId: string): Promise<PairingToken> {
  return apiRequest<PairingToken>(`/api/v1/modules/${moduleId}/pairing-tokens`, {
    method: 'POST',
    body: {},
  });
}

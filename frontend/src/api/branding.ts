import { apiRequest } from '@/api/client';

export type BrandingProfile = {
  revision: number;
  theme: Record<string, unknown>;
  typography: Record<string, unknown>;
  copy: Record<string, unknown>;
  updated_at: string;
};

export type BrandingAsset = {
  id: string;
  role: string;
  content_type: string;
  size_bytes: number;
  url: string;
  created_at: string;
};

export function getBrandingProfile(): Promise<BrandingProfile> {
  return apiRequest<BrandingProfile>('/api/v1/brand/profile');
}

export function putBrandingProfile(
  theme: Record<string, unknown>,
  typography: Record<string, unknown>,
  copy: Record<string, unknown>,
): Promise<{ revision: number }> {
  return apiRequest<{ revision: number }>('/api/v1/brand/profile', {
    method: 'PUT',
    body: { theme, typography, copy },
  });
}

export function listBrandingAssets(): Promise<BrandingAsset[]> {
  return apiRequest<BrandingAsset[]>('/api/v1/brand/assets');
}

export function uploadBrandingAsset(role: string, file: File): Promise<{ id: string }> {
  const form = new FormData();
  form.set('role', role);
  form.set('file', file);
  return apiRequest<{ id: string }>('/api/v1/brand/assets', { method: 'POST', body: form });
}

export function deleteBrandingAsset(id: string): Promise<void> {
  return apiRequest<void>(`/api/v1/brand/assets/${id}`, { method: 'DELETE' });
}

/** Den pushes the brand into the Keycloak realm so the sign-in page matches. */
export type KeycloakSyncStatus = {
  enabled: boolean;
  realm?: string;
  revision?: number;
  synced_at?: string;
  last_error?: string;
};

export function getKeycloakSync(): Promise<KeycloakSyncStatus> {
  return apiRequest<KeycloakSyncStatus>('/api/v1/keycloak-sync');
}

export function syncKeycloakNow(): Promise<KeycloakSyncStatus> {
  return apiRequest<KeycloakSyncStatus>('/api/v1/keycloak-sync', { method: 'POST' });
}

/** Asset roles modules and the sign-in page understand; the newest upload per role wins. */
export const ASSET_ROLES = [
  { role: 'logo', hint: 'Shown in module headers and on the sign-in page. SVG or PNG, transparent background.' },
  { role: 'favicon', hint: 'Browser tab icon. Square PNG or SVG.' },
] as const;

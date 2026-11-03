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

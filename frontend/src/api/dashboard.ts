import { apiRequest } from '@/api/client';

export type DashboardOrganization = {
  id: string;
  slug: string;
  display_name: string;
};

export type DashboardCopy = {
  convention_name?: string;
  tagline?: string;
  welcome?: string;
};

export type UsableModule = {
  id: string;
  slug: string;
  display_name: string;
  base_url: string;
  environment: string;
  status: string;
  enabled: boolean;
};

export type DashboardHome = {
  organization: DashboardOrganization | null;
  copy: DashboardCopy;
  brand?: {
    theme: Record<string, unknown>;
    typography: Record<string, unknown>;
    copy: Record<string, unknown>;
  };
  usable_modules: UsableModule[];
  organizations: number;
  users: number;
  modules: number;
  paired_modules: number;
  unpaired_modules: number;
};

export function fetchDashboard(): Promise<DashboardHome> {
  return apiRequest<DashboardHome>('/api/v1/dashboard');
}

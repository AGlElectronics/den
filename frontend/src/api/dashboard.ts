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
  /** Links from Den → Module settings → Identity; the IdP admin console only for admins. */
  identity_links?: {
    account_console_url?: string;
    sso_name?: string;
    idp_admin_console_url?: string;
  };
  /** The modules this person may use (admins: every usable module). */
  usable_modules: UsableModule[];
  /** Organisation-wide counts, only sent to admins. */
  users?: number;
  modules?: number;
  paired_modules?: number;
  unpaired_modules?: number;
};

export function fetchDashboard(): Promise<DashboardHome> {
  return apiRequest<DashboardHome>('/api/v1/dashboard');
}

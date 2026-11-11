import { apiRequest } from '@/api/client';

export type RetentionSettings = {
  offboarded_volunteers_days?: number;
  inactive_users_days?: number;
  module_data_days?: number;
};

export type SmtpSecurity = 'starttls' | 'tls' | 'none';

export type SmtpSettings = {
  host?: string;
  port?: number;
  security?: SmtpSecurity;
  username?: string;
  /** Write-only: a string sets it, null clears it, leaving it out keeps it. */
  password?: string | null;
  password_set?: boolean;
  from?: string;
  from_name?: string;
};

export type SellerSettings = {
  legal_name?: string;
  address?: string;
  country?: string;
  vat_id?: string;
  tax_number?: string;
  registration?: string;
  email?: string;
  website?: string;
};

export type LostFound =
  | { mode: 'kept_after_con'; days?: number }
  | { mode: 'deleted_after_closing'; hours?: number };

export type TasksSettings = {
  trash_days?: number;
  notification_days?: number;
  audit_days?: number;
  automation_run_days?: number;
  due_soon_days?: number;
};

export type ModuleSettingsData = {
  retention?: RetentionSettings;
  smtp?: SmtpSettings;
  finance?: { seller?: SellerSettings };
  stash?: { lost_found?: LostFound };
  wiki?: { revision_retention_days?: number };
  tasks?: TasksSettings;
  rally?: { submission_site_host?: string };
  identity?: { account_console_url?: string; sso_name?: string; idp_admin_console_url?: string };
};

export type ModuleSettings = { data: ModuleSettingsData; updated_at: string | null };

export const getModuleSettings = () => apiRequest<ModuleSettings>('/api/v1/module-settings');

export const putModuleSettings = (data: ModuleSettingsData) =>
  apiRequest<ModuleSettings>('/api/v1/module-settings', { method: 'PUT', body: { data } });

/** Drops empty strings, NaN numbers and empty sections so "unset" stays unset. */
export function compactSettings(data: ModuleSettingsData): ModuleSettingsData {
  const clean = (o: Record<string, unknown>) => {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(o)) {
      if (k === 'password_set') continue;
      if (v === undefined || v === '' || (typeof v === 'number' && Number.isNaN(v))) continue;
      if (v && typeof v === 'object' && !Array.isArray(v)) {
        const inner = clean(v as Record<string, unknown>);
        if (Object.keys(inner).length) out[k] = inner;
        continue;
      }
      out[k] = v;
    }
    return out;
  };
  const out = clean(data as Record<string, unknown>) as ModuleSettingsData;
  if (data.smtp?.password === null) out.smtp = { ...(out.smtp ?? {}), password: null };
  return out;
}

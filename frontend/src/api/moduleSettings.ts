import { apiRequest } from '@/api/client';

export type RetentionSettings = {
  pending_legal_review?: boolean;
  offboarded_volunteers_days?: number;
  inactive_users_days?: number;
  module_data_days?: number;
  backup_note?: string;
};

export type SmtpSettings = {
  host?: string;
  port?: number;
  from?: string;
  username?: string;
  /** Write-only: a string sets it, null clears it, leaving it out keeps it. */
  password?: string | null;
  password_set?: boolean;
};

export type LostAndFound =
  | { mode: 'days_after_con'; days?: number }
  | { mode: 'hours_after_closing'; hours?: number };

export type ModuleSettingsData = {
  retention?: RetentionSettings;
  smtp?: SmtpSettings;
  finance?: { seller_name?: string; seller_address?: string; vat_number?: string };
  stash?: { lost_and_found?: LostAndFound };
  rally?: { submission_site_host?: string };
  identity?: { account_console_url?: string; sso_name?: string; idp_admin_console_url?: string };
};

export type ModuleSettings = { data: ModuleSettingsData; updated_at: string | null };

export const getModuleSettings = () => apiRequest<ModuleSettings>('/api/v1/module-settings');

export const putModuleSettings = (data: ModuleSettingsData) =>
  apiRequest<ModuleSettings>('/api/v1/module-settings', { method: 'PUT', body: data });

/** Drops empty strings, NaN numbers and empty sections so "unset" stays unset. */
export function compactSettings(data: ModuleSettingsData): ModuleSettingsData {
  const clean = (o: Record<string, unknown>) => {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(o)) {
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
  if (out.smtp) delete out.smtp.password_set;
  if (out.retention) delete out.retention.pending_legal_review;
  for (const k of Object.keys(out) as (keyof ModuleSettingsData)[]) {
    if (!out[k] || !Object.keys(out[k] as object).length) delete out[k];
  }
  if (data.smtp?.password === null) out.smtp = { ...(out.smtp ?? {}), password: null };
  return out;
}

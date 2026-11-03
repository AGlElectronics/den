import { apiRequest } from '@/api/client';

export type OrgSettings = {
  data: Record<string, unknown>;
  updated_at: string | null;
};

export const getOrgSettings = () => apiRequest<OrgSettings>('/api/v1/org-settings');

export const putOrgSettings = (data: Record<string, unknown>) =>
  apiRequest<OrgSettings>('/api/v1/org-settings', { method: 'PUT', body: data });

export type OrgSettingField = { key: string; example: string; affects: string };

/** Keys Den validates; paired modules show these as "Managed in Den". */
export const ORG_SETTINGS_SCHEMA: OrgSettingField[] = [
  { key: 'organization_name', example: 'Example Con', affects: 'Name in emails (e.g. the account-deleted notice) and on the sign-in page.' },
  { key: 'org_email_domain', example: 'example.org', affects: 'HR: organisation emails are <name>@this domain.' },
  { key: 'org_email_template', example: '{first}.{last}', affects: 'HR: suggested name before the @. Use {first}, {last}, {nickname}.' },
  { key: 'username_template', example: '{nickname}', affects: 'HR: login name for new Keycloak accounts. Existing accounts keep theirs.' },
];

import { apiRequest } from '@/api/client';

export type BackupRun = {
  kind: string;
  started_at: string;
  finished_at: string;
  ok: boolean;
  error?: string;
  key?: string;
  size_bytes?: number;
  databases?: string[];
  buckets?: string[];
};

export type BackupSettings = {
  hour: number;
  keep_daily: number;
  keep_weekly: number;
  keep_monthly: number;
};

export type ServerDatabase = {
  name: string;
  owner: string;
  size_bytes: number;
  protected: boolean;
};

export type OperationsStatus =
  | { configured: false }
  | { configured: true; reachable: false; error: string }
  | {
      configured: true;
      reachable: true;
      managed_databases: boolean;
      backups_enabled: boolean;
      target?: string;
      recipients?: string[];
      file_buckets: string[];
      timezone: string;
      settings?: BackupSettings;
      last?: BackupRun;
      last_success?: BackupRun;
      next_run?: string;
      running?: string;
      problem?: string;
      same_storage_as_data?: boolean;
      warning?: string;
      databases: ServerDatabase[];
    };

export type Backup = {
  key: string;
  kind: 'scheduled' | 'manual' | 'pre-drop' | string;
  database?: string;
  size_bytes: number;
  created_at: string;
};

export const getOperationsStatus = () => apiRequest<OperationsStatus>('/api/v1/operations/status');

export const listBackups = () => apiRequest<Backup[]>('/api/v1/operations/backups');

export const backupNow = () => apiRequest<{ started: boolean }>('/api/v1/operations/backups', { method: 'POST' });

export const saveBackupSettings = (s: BackupSettings) =>
  apiRequest<BackupSettings>('/api/v1/operations/settings', { method: 'PUT', body: s });

/** Starts a browser download through a one-time link. */
export async function downloadBackup(key: string) {
  const { url } = await apiRequest<{ url: string }>('/api/v1/operations/backups/download-link', {
    method: 'POST',
    body: { key },
  });
  const a = document.createElement('a');
  a.href = url;
  a.rel = 'noopener';
  document.body.appendChild(a);
  a.click();
  a.remove();
}

export function formatBytes(n: number | undefined) {
  if (n === undefined) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 && i > 0 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

/** The backup status needs attention (failed, missing or too old). */
export function backupProblem(s: OperationsStatus | undefined): string | undefined {
  if (!s || !s.configured) return undefined;
  if (!s.reachable) return `Backups can't be checked: ${s.error}`;
  if (!s.backups_enabled) return undefined;
  return s.problem;
}

/** Backups work but are set up riskily (for example on the live data's storage). */
export function backupWarning(s: OperationsStatus | undefined): string | undefined {
  if (!s || !s.configured || !s.reachable || !s.backups_enabled) return undefined;
  return s.warning;
}

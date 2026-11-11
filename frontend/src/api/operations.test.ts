import { describe, expect, it } from 'vitest';

import { backupProblem, backupWarning, type OperationsStatus } from './operations';

const ready = (extra: Partial<Extract<OperationsStatus, { reachable: true }>> = {}): OperationsStatus => ({
  configured: true,
  reachable: true,
  managed_databases: true,
  backups_enabled: true,
  file_buckets: [],
  timezone: 'UTC',
  databases: [],
  ...extra,
});

describe('backup status helpers', () => {
  it('reports the same-storage warning separately from problems', () => {
    const s = ready({ same_storage_as_data: true, warning: 'same storage' });
    expect(backupProblem(s)).toBeUndefined();
    expect(backupWarning(s)).toBe('same storage');
  });

  it('has no warning when backups are off or the operator is unreachable', () => {
    expect(backupWarning(ready({ backups_enabled: false, warning: 'same storage' }))).toBeUndefined();
    expect(backupWarning({ configured: true, reachable: false, error: 'down' })).toBeUndefined();
    expect(backupWarning({ configured: false })).toBeUndefined();
  });
});

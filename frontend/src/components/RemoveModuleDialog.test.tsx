import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { Module } from '@/api/modules';
import type { ServerDatabase } from '@/api/operations';
import { RemoveModuleDialog } from '@/components/RemoveModuleDialog';
import { canDropDatabase } from '@/lib/modules';
import { renderWithQuery } from '@/test/utils';

const mod = (createdByDen: boolean): Module => ({
  id: 'm1',
  organization_id: 'o',
  slug: 'stash',
  display_name: 'Stash',
  base_url: 'https://stash.example.org',
  internal_url: '',
  environment: 'production',
  enabled: true,
  status: 'paired',
  portal_visibility: 'role_holders',
  manifest: {},
  created_at: '',
  updated_at: '',
  database: 'stash',
  database_created_by_den: createdByDen,
});

const db: ServerDatabase = { name: 'stash', owner: 'stash', size_bytes: 2048, protected: false };

function dialog(m: Module, ops: boolean, database: ServerDatabase | undefined = db) {
  renderWithQuery(
    <RemoveModuleDialog module={m} database={database} backupsEnabled ops={ops} onClose={() => {}} onRemoved={() => {}} />,
  );
}

describe('canDropDatabase', () => {
  it('needs a Den-created, existing, unprotected database and the ops role', () => {
    expect(canDropDatabase(mod(true), db, true)).toBe(true);
    expect(canDropDatabase(mod(false), db, true)).toBe(false);
    expect(canDropDatabase(mod(true), db, false)).toBe(false);
    expect(canDropDatabase(mod(true), undefined, true)).toBe(false);
    expect(canDropDatabase(mod(true), { ...db, protected: true }, true)).toBe(false);
  });
});

describe('RemoveModuleDialog', () => {
  it('preselects deleting the data only for a database Den created', () => {
    dialog(mod(true), true);
    expect((screen.getByRole('radio', { name: /delete its data/i }) as HTMLInputElement).checked).toBe(true);
  });

  it('does not offer deleting a database Den did not create', () => {
    dialog(mod(false), true);
    expect(screen.queryByRole('radio', { name: /delete its data/i })).toBeNull();
    expect((screen.getByRole('radio', { name: /keep its data/i }) as HTMLInputElement).checked).toBe(true);
    expect(screen.getByTestId('no-drop-reason').textContent).toMatch(/did not create/);
  });

  it('explains that deleting needs the ops role', () => {
    dialog(mod(true), false);
    expect(screen.queryByRole('radio', { name: /delete its data/i })).toBeNull();
    expect(screen.getByTestId('no-drop-reason').textContent).toMatch(/ops role/);
  });
});

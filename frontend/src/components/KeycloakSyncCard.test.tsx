import { screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { KeycloakSyncCard } from '@/components/KeycloakSyncCard';
import { mockFetch, renderWithQuery } from '@/test/utils';

describe('KeycloakSyncCard', () => {
  it('shows why the realm name sync is off in identity mode existing', async () => {
    mockFetch([
      {
        path: '/api/v1/keycloak-sync',
        body: { enabled: false, mode: 'pull', message: 'Off because IDENTITY_STAFF_MODE is existing.' },
      },
    ]);
    renderWithQuery(<KeycloakSyncCard />);
    await waitFor(() => expect(screen.getByTestId('keycloak-sync-status').textContent).toMatch(/IDENTITY_STAFF_MODE is existing/));
    expect(screen.queryByRole('button', { name: /sync/i })).toBeNull();
  });

  it('shows the synced realm name and offers a manual sync', async () => {
    mockFetch([
      {
        path: '/api/v1/keycloak-sync',
        body: { enabled: true, mode: 'pull', realm: 'conservices', display_name: 'Example Con', synced_at: '2026-11-11T12:00:00Z' },
      },
    ]);
    renderWithQuery(<KeycloakSyncCard />);
    await waitFor(() => expect(screen.getByTestId('keycloak-sync-status').textContent).toMatch(/Example Con/));
    expect(screen.getByRole('button', { name: /sync name now/i })).toBeTruthy();
  });
});

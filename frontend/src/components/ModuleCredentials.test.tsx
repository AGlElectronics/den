import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { Module } from '@/api/modules';
import { ModuleCredentials } from '@/components/ModuleCredentials';
import { mockFetch, renderWithQuery } from '@/test/utils';

const mod = { id: 'm1', display_name: 'Stash', slug: 'stash' } as Module;

describe('ModuleCredentials', () => {
  it('lists credentials and revokes one', async () => {
    const calls = mockFetch([
      {
        path: '/api/v1/modules/m1/credentials',
        body: [
          { id: 'c2', label: 'pairing-new', created_at: '2026-11-11T10:00:00Z', last_used_at: null, revoked_at: null },
          { id: 'c1', label: 'pairing-old', created_at: '2026-11-01T10:00:00Z', last_used_at: null, revoked_at: '2026-11-11T10:00:00Z' },
        ],
      },
      { method: 'DELETE', path: '/api/v1/modules/m1/credentials/c2', body: { revoked: 1, module_status: 'unpaired' } },
    ]);
    renderWithQuery(<ModuleCredentials module={mod} onClose={() => {}} />);
    await waitFor(() => expect(screen.getByText('pairing-new')).toBeTruthy());
    expect(screen.getByText(/Revoked/)).toBeTruthy();

    const revokeButtons = screen.getAllByRole('button', { name: 'Revoke' });
    expect(revokeButtons).toHaveLength(1);
    fireEvent.click(revokeButtons[0]);
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url === '/api/v1/modules/m1/credentials/c2')).toBe(true));
    await waitFor(() => expect(screen.getByText(/is now unpaired/)).toBeTruthy());
  });

  it('asks before revoking every credential', async () => {
    const calls = mockFetch([
      {
        path: '/api/v1/modules/m1/credentials',
        body: [{ id: 'c2', label: 'pairing-new', created_at: '2026-11-11T10:00:00Z', last_used_at: null, revoked_at: null }],
      },
      { method: 'DELETE', path: '/api/v1/modules/m1/credentials', body: { revoked: 1, module_status: 'unpaired' } },
    ]);
    renderWithQuery(<ModuleCredentials module={mod} onClose={() => {}} />);
    fireEvent.click(await screen.findByRole('button', { name: /revoke all credentials/i }));
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: /^revoke all$/i }));
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url === '/api/v1/modules/m1/credentials')).toBe(true));
  });
});

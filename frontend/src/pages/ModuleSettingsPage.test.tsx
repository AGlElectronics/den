import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { ModuleSettingsPage } from '@/pages/ModuleSettingsPage';
import { renderWithQuery } from '@/test/utils';

const docs = 'https://aglelectronics.github.io/con-services/modules/den/configuration/#shared-smtp-password';

function stubSettings(passwordSet: boolean) {
  const calls: { method: string; body?: string }[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (_input: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET';
      calls.push({ method, body: typeof init?.body === 'string' ? init.body : undefined });
      const data = passwordSet
        ? { smtp: { host: 'smtp.example.org', username: 'noreply', password_set: true } }
        : { retention: { inactive_users_days: 30 } };
      return new Response(JSON.stringify({ data, updated_at: '2026-11-11T12:00:00Z' }), { status: 200 });
    }),
  );
  return calls;
}

describe('ModuleSettingsPage SMTP password', () => {
  it('says the password is configured in the secret and does not offer an input', async () => {
    const calls = stubSettings(true);
    renderWithQuery(<ModuleSettingsPage />);
    expect(await screen.findByText("Password: configured in Den's secret (DEN_SMTP_PASSWORD)")).toBeTruthy();
    expect(screen.queryByLabelText('Password')).toBeNull();
    expect(document.querySelector('input[type="password"]')).toBeNull();
    const link = screen.getByRole('link', { name: 'How to set it' });
    expect(link.getAttribute('href')).toBe(docs);

    fireEvent.change(await screen.findByLabelText('Server'), { target: { value: 'smtp.example.org' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT');
    expect(put?.body ?? '').not.toContain('password');
  });

  it('says the password is not set', async () => {
    stubSettings(false);
    renderWithQuery(<ModuleSettingsPage />);
    expect(await screen.findByText('Password: not set')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'How to set it' }).getAttribute('href')).toBe(docs);
    expect(screen.queryByLabelText('Password')).toBeNull();
  });
});

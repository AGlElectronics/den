import { describe, expect, it } from 'vitest';

import { compactSettings } from '@/api/moduleSettings';

describe('compactSettings', () => {
  it('drops empty values, empty sections and read-only flags', () => {
    expect(
      compactSettings({
        retention: { pending_legal_review: true, inactive_users_days: 30, backup_note: '' },
        smtp: { host: '', password_set: true },
        finance: { seller_name: '' },
        rally: { submission_site_host: 'submit.example.org' },
      }),
    ).toEqual({ retention: { inactive_users_days: 30 }, rally: { submission_site_host: 'submit.example.org' } });
  });

  it('keeps an explicit null password so the server clears it', () => {
    expect(compactSettings({ smtp: { host: 'smtp.example.org', password: null } })).toEqual({
      smtp: { host: 'smtp.example.org', password: null },
    });
  });

  it('keeps nested Lost & Found settings', () => {
    expect(compactSettings({ stash: { lost_and_found: { mode: 'hours_after_closing', hours: 48 } } })).toEqual({
      stash: { lost_and_found: { mode: 'hours_after_closing', hours: 48 } },
    });
  });
});

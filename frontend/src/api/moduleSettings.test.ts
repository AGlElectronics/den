import { describe, expect, it } from 'vitest';

import { compactSettings } from '@/api/moduleSettings';

describe('compactSettings', () => {
  it('drops empty values, empty sections and read-only flags', () => {
    expect(
      compactSettings({
        retention: { inactive_users_days: 30, module_data_days: Number.NaN },
        smtp: { host: '', password_set: true },
        finance: { seller: { legal_name: '' } },
        rally: { submission_site_host: 'submit.example.org' },
      }),
    ).toEqual({ retention: { inactive_users_days: 30 }, rally: { submission_site_host: 'submit.example.org' } });
  });

  it('keeps an explicit null password so the server clears it', () => {
    expect(compactSettings({ smtp: { host: 'smtp.example.org', password: null } })).toEqual({
      smtp: { host: 'smtp.example.org', password: null },
    });
  });

  it('keeps nested module sections', () => {
    expect(
      compactSettings({
        finance: { seller: { legal_name: 'Example Con e.V.', vat_id: 'ATU12345678' } },
        stash: { lost_found: { mode: 'deleted_after_closing', hours: 48 } },
        tasks: { retention: { trash_days: 30 }, reminders: { due_soon_days: 2 } },
        wiki: { revision_retention_days: 365 },
      }),
    ).toEqual({
      finance: { seller: { legal_name: 'Example Con e.V.', vat_id: 'ATU12345678' } },
      stash: { lost_found: { mode: 'deleted_after_closing', hours: 48 } },
      tasks: { retention: { trash_days: 30 }, reminders: { due_soon_days: 2 } },
      wiki: { revision_retention_days: 365 },
    });
  });
});

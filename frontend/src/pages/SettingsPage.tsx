import { useState } from 'react';

import { getDevUser, setDevUser } from '@/auth/dev';
import { isOidcAuthMode } from '@/auth/oidc';
import { runtimeConfig } from '@/runtime-config';

export function SettingsPage() {
  const [devUser, setDevUserLocal] = useState(getDevUser());
  const [saved, setSaved] = useState(false);

  const oidc = isOidcAuthMode();

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-heading font-bold">Settings</h1>
        <p className="text-sm text-den-foreground/70 mt-1">
          Build-time and local-only switches for this Den instance.
        </p>
      </div>

      <section className="bg-den-surface border border-den-border rounded-lg p-4 space-y-3">
        <h2 className="font-heading font-bold">Auth</h2>
        <div className="text-sm">
          Mode: <code>{oidc ? 'oidc' : 'dev'}</code>
        </div>
        {oidc ? (
          <div className="text-sm text-den-foreground/70">
            This SPA was built with <code>VITE_AUTH_MODE=oidc</code>. Users sign in with
            your identity provider; API calls carry a Bearer ID token.
          </div>
        ) : (
          <div className="space-y-2">
            <label className="block text-sm">
              Dev user (sent as <code>X-Dev-User</code>)
              <input
                className="mt-1 w-full md:w-80 rounded-md border border-den-border px-2 py-1"
                value={devUser}
                onChange={(e) => {
                  setDevUserLocal(e.target.value);
                  setSaved(false);
                }}
              />
            </label>
            <button
              type="button"
              className="bg-den-primary text-den-on-primary px-3 py-2 rounded-md hover:bg-den-primary-hover"
              onClick={() => {
                setDevUser(devUser.trim() || 'dev-admin');
                setSaved(true);
              }}
            >
              Save
            </button>
            {saved && <span className="ml-2 text-sm text-den-success">Saved locally.</span>}
          </div>
        )}
      </section>

      <section className="bg-den-surface border border-den-border rounded-lg p-4 space-y-2 text-sm">
        <h2 className="font-heading font-bold">About</h2>
        <div>
          Version: <code>{process.env.DEN_VERSION ?? 'dev'}</code>
        </div>
        <div>
          API base: <code>{runtimeConfig().apiBaseUrl || '(same origin)'}</code>
        </div>
      </section>
    </div>
  );
}

import { useSearch, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';

import { isOidcAuthMode, signInWithOidc } from '@/auth/oidc';
import { getDevUser, setDevUser } from '@/auth/dev';

export function LoginPage() {
  const { redirect } = useSearch({ from: '/login' });
  const navigate = useNavigate();
  const [name, setName] = useState(getDevUser());

  if (isOidcAuthMode()) {
    return (
      <div className="min-h-screen grid place-items-center bg-den-muted">
        <div className="bg-den-surface border border-den-border rounded-lg p-8 w-full max-w-md shadow-sm">
          <h1 className="text-2xl font-heading font-bold">Sign in</h1>
          <p className="text-sm text-den-foreground/70 mt-2">
            Den is configured for OIDC. Continue to your identity provider to sign in.
          </p>
          <button
            type="button"
            className="mt-6 w-full bg-den-primary text-white font-medium rounded-md py-2 hover:bg-den-primary-hover"
            onClick={() => void signInWithOidc(redirect)}
          >
            Continue with SSO
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen grid place-items-center bg-den-muted">
      <div className="bg-den-surface border border-den-border rounded-lg p-8 w-full max-w-md shadow-sm">
        <h1 className="text-2xl font-heading font-bold">Dev sign in</h1>
        <p className="text-sm text-den-foreground/70 mt-2">
          Den is running in dev mode (<code>AUTH_MODE=dev</code>). Pick a dev user name —
          it is sent to the backend as <code>X-Dev-User</code> and auto-provisioned.
        </p>
        <label className="block mt-6 text-sm">
          Dev user
          <input
            className="mt-1 w-full rounded-md border border-den-border px-3 py-2"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <button
          type="button"
          className="mt-6 w-full bg-den-primary text-white font-medium rounded-md py-2 hover:bg-den-primary-hover"
          onClick={() => {
            setDevUser(name.trim() || 'dev-admin');
            void navigate({ to: redirect });
          }}
        >
          Continue
        </button>
      </div>
    </div>
  );
}

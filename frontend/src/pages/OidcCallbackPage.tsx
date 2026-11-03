import { useEffect, useState } from 'react';
import { useNavigate } from '@tanstack/react-router';

import { getUserManager } from '@/auth/oidc';
import { oidcCallbackUrl } from '@/auth/captureCallbackUrl';

export function OidcCallbackPage() {
  const navigate = useNavigate();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const mgr = getUserManager();
    if (!mgr) {
      setError('OIDC is not configured.');
      return;
    }
    mgr
      .signinRedirectCallback(oidcCallbackUrl())
      .then((user) => {
        const target = (user?.state as string | undefined) ?? '/dashboard';
        void navigate({ to: target.startsWith('/') ? target : '/dashboard' });
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
      });
  }, [navigate]);

  return (
    <div className="min-h-screen grid place-items-center bg-den-muted">
      <div className="bg-den-surface border border-den-border rounded-lg p-6 max-w-md text-center">
        {error ? (
          <>
            <h1 className="font-heading font-bold text-xl">Sign-in failed</h1>
            <p className="text-sm text-den-foreground/70 mt-2">{error}</p>
          </>
        ) : (
          <>
            <h1 className="font-heading font-bold text-xl">Signing you in…</h1>
            <p className="text-sm text-den-foreground/70 mt-2">One moment.</p>
          </>
        )}
      </div>
    </div>
  );
}

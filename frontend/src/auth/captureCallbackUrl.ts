// Captured at module-evaluation time (before the router initializes) so OIDC
// query params (code, state, session_state) survive TanStack Router's URL
// normalization, which can otherwise strip unvalidated search params.
const _captured =
  typeof window !== 'undefined' && window.location.pathname === '/auth/callback'
    ? window.location.href
    : '';

/** Returns the original callback URL with OIDC response params intact. */
export function oidcCallbackUrl(): string {
  return _captured || (typeof window !== 'undefined' ? window.location.href : '');
}

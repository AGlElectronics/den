import { InMemoryWebStorage, UserManager, WebStorageStateStore, type User } from 'oidc-client-ts';
import { announceSignOut } from '@/auth/channel';
import { runtimeConfig } from '@/runtime-config';

// Mirrors Stash's OIDC bootstrap: a lazily-constructed UserManager wrapped
// in helpers the router can call to gate app routes. Den does NOT issue
// tokens — it only verifies. See docs/adr/0001-idp-first-ecosystem.md.

/** True when the SPA was built for OIDC (Bearer tokens to the API). */
export function isOidcAuthMode(): boolean {
  return runtimeConfig().authMode === 'oidc';
}

let manager: UserManager | null | undefined;

export function getUserManager(): UserManager | null {
  if (manager !== undefined) {
    return manager;
  }
  const authority = runtimeConfig().oidcAuthority;
  const client_id = runtimeConfig().oidcClientId;
  if (!authority || !client_id) {
    manager = null;
    return null;
  }
  if (typeof window === 'undefined') {
    manager = null;
    return null;
  }
  const scope =
    runtimeConfig().oidcScope;
  dropStoredTokens();
  manager = new UserManager({
    authority,
    client_id,
    redirect_uri: `${window.location.origin}/auth/callback`,
    post_logout_redirect_uri: `${window.location.origin}/login`,
    response_type: 'code',
    scope,
    automaticSilentRenew: true,
    silent_redirect_uri: `${window.location.origin}/silent-callback.html`,
    monitorSession: true,
    // Tokens live only in this tab's memory, never in localStorage or
    // sessionStorage where injected script could read them. After a reload
    // restoreSession() gets new ones from the Keycloak session.
    userStore: new WebStorageStateStore({ store: new InMemoryWebStorage() }),
  });
  return manager;
}

/** Removes tokens that Den releases before 1.0 kept in localStorage. */
function dropStoredTokens(): void {
  try {
    for (const key of Object.keys(window.localStorage)) {
      if (key.startsWith('oidc.user:')) window.localStorage.removeItem(key);
    }
  } catch {
    // storage blocked: nothing was stored either
  }
}

let restoring: Promise<User | null> | null = null;

/**
 * The signed-in user, renewed through a silent sign-in (hidden iframe against
 * the Keycloak session) when there is none in memory yet or it has expired.
 * Concurrent callers share one attempt. Resolves to null when Keycloak has no
 * session for this browser.
 */
export async function restoreSession(): Promise<User | null> {
  const mgr = getUserManager();
  if (!mgr) return null;
  const user = await mgr.getUser();
  if (user && !user.expired) return user;
  restoring ??= mgr
    .signinSilent()
    .catch(() => null)
    .finally(() => {
      restoring = null;
    });
  return restoring;
}

/**
 * The access token Den sends to its own API, or null when signed out / not in
 * OIDC mode. Den accepts access tokens with audience `den-api` only; ID
 * tokens are refused.
 */
export async function getOidcBearer(): Promise<string | null> {
  if (!isOidcAuthMode()) return null;
  const user = await restoreSession();
  return user?.access_token ?? null;
}

export async function signInWithOidc(returnPath: string): Promise<void> {
  const mgr = getUserManager();
  if (!mgr) {
    throw new Error('OIDC is not configured (set oidcAuthority and oidcClientId in /config.json).');
  }
  const safePath = returnPath.startsWith('/') ? returnPath : '/dashboard';
  try {
    await mgr.signinRedirect({ state: safePath });
  } catch (err) {
    // fetch() rejects with a bare TypeError ("Failed to fetch") when the login
    // server is down or its certificate isn't trusted.
    if (err instanceof TypeError) {
      throw new Error(
        `Can't reach the login server (${new URL(mgr.settings.authority).host}). It may be down or its certificate may have expired.`,
      );
    }
    throw err;
  }
}

export async function signOutOidc(): Promise<void> {
  const mgr = getUserManager();
  if (!mgr) {
    return;
  }
  announceSignOut();
  await mgr.signoutRedirect();
}

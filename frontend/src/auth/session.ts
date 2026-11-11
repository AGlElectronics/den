import type { User } from 'oidc-client-ts';

import { setUnauthorizedHandler } from '@/api/client';
import { announceSignOut, announceUser, onAuthMessage } from '@/auth/channel';
import { getUserManager } from '@/auth/oidc';

/*
 * Keeps every tab on the identity Keycloak currently has. Each tab holds its
 * tokens in memory and tells the others (see channel.ts) who is signed in:
 *  - another tab signs out            -> this tab goes to the login page
 *  - another tab signs in as someone  -> this tab reloads as that person
 *  - Keycloak session ends elsewhere   -> session monitor signs this tab out
 *  - the API answers 401               -> back to the login page
 * Without this, a tab kept a stale admin token after a user switch.
 */

export function goToLogin(): void {
  const path = `${window.location.pathname}${window.location.search}`;
  if (path.startsWith('/login') || path.startsWith('/auth/')) return;
  window.location.assign(`/login?redirect=${encodeURIComponent(path)}`);
}

let installed = false;

export function installSessionSync(): void {
  const mgr = getUserManager();
  if (!mgr || installed || typeof window === 'undefined') return;
  installed = true;

  let currentSub: string | undefined;
  mgr.events.addUserLoaded((u: User) => {
    const changed = u.profile.sub !== currentSub;
    currentSub = u.profile.sub;
    if (changed) announceUser(u.profile.sub);
  });

  onAuthMessage((msg) => {
    if (msg.type === 'signed-out') {
      void mgr.removeUser().finally(goToLogin);
    } else if (currentSub !== undefined && msg.sub !== currentSub) {
      window.location.reload();
    }
  });

  const endSession = () => {
    announceSignOut();
    void mgr.removeUser().finally(goToLogin);
  };
  mgr.events.addUserSignedOut(endSession);
  // Same person, new Keycloak session (signed out and back in elsewhere): adopt
  // it silently instead of reloading.
  mgr.events.addUserSessionChanged(() => {
    void mgr.signinSilent().catch(endSession);
  });
  mgr.events.addSilentRenewError((err) => {
    if (/login_required|invalid_grant|session not active/i.test(String(err?.message ?? err))) endSession();
  });

  let redirecting = false;
  setUnauthorizedHandler(() => {
    if (redirecting) return;
    redirecting = true;
    endSession();
  });
}

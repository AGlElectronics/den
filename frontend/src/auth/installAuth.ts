import { setAuthHeadersResolver } from '@/api/client';
import { installDevAuth } from '@/auth/dev';
import { getUserManager, isOidcAuthMode } from '@/auth/oidc';
import { installSessionSync } from '@/auth/session';

/** Wire API auth: dev header or OIDC Bearer token. Call once at startup. */
export function installAuth(): void {
  if (isOidcAuthMode()) {
    setAuthHeadersResolver(async (): Promise<Record<string, string>> => {
      const mgr = getUserManager();
      if (!mgr) {
        return {};
      }
      const user = await mgr.getUser();
      if (!user) {
        return {};
      }
      // Den's backend will verify via go-oidc's IDTokenVerifier (same as
      // Stash). Prefer the ID token so `aud` lines up with the configured
      // client; access tokens from Keycloak often have a different `aud`.
      const bearer = user.id_token ?? user.access_token;
      if (!bearer || user.expired) {
        return {};
      }
      return { Authorization: `Bearer ${bearer}` };
    });
    installSessionSync();
    return;
  }
  installDevAuth();
}

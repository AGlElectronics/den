import { setAuthHeadersResolver } from '@/api/client';
import { installDevAuth } from '@/auth/dev';
import { getOidcBearer, isOidcAuthMode } from '@/auth/oidc';
import { installSessionSync } from '@/auth/session';

/** Wire API auth: dev header or OIDC Bearer token. Call once at startup. */
export function installAuth(): void {
  if (isOidcAuthMode()) {
    setAuthHeadersResolver(async (): Promise<Record<string, string>> => {
      const bearer = await getOidcBearer();
      return bearer ? { Authorization: `Bearer ${bearer}` } : {};
    });
    installSessionSync();
    return;
  }
  installDevAuth();
}

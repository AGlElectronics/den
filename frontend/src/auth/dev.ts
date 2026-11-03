// Dev auth shim. When the backend runs with AUTH_MODE=dev it accepts
// `X-Dev-User: <subject>`. The frontend sends the value of
// localStorage["den.devUser"] or "dev-admin" by default. The settings page
// lets the operator change it.

import { setAuthHeadersResolver } from '@/api/client';

const STORAGE_KEY = 'den.devUser';

export function getDevUser(): string {
  if (typeof window === 'undefined') return 'dev-admin';
  return window.localStorage.getItem(STORAGE_KEY) ?? 'dev-admin';
}

export function setDevUser(name: string) {
  window.localStorage.setItem(STORAGE_KEY, name || 'dev-admin');
}

export function installDevAuth() {
  setAuthHeadersResolver(async () => ({
    'X-Dev-User': getDevUser(),
  }));
}

/*
 * Tells the other tabs of this app who is signed in. Tokens stay in each
 * tab's memory; only the subject (or "signed out") crosses tabs.
 */

export type AuthMessage = { type: 'signed-out' } | { type: 'user'; sub: string };

const NAME = 'den-auth';

let channel: BroadcastChannel | null | undefined;

function getChannel(): BroadcastChannel | null {
  if (channel === undefined) {
    channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(NAME);
  }
  return channel;
}

export function announceSignOut(): void {
  getChannel()?.postMessage({ type: 'signed-out' } satisfies AuthMessage);
}

export function announceUser(sub: string): void {
  getChannel()?.postMessage({ type: 'user', sub } satisfies AuthMessage);
}

export function onAuthMessage(handler: (msg: AuthMessage) => void): void {
  getChannel()?.addEventListener('message', (e: MessageEvent<AuthMessage>) => handler(e.data));
}

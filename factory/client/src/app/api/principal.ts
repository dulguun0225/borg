// The People key the human at the screen says they are, carried on every call
// as X-Factory-Principal. Nothing verifies it: seam 5 is where authentication
// attaches, and this milestone attaches none.
//
// It is held in localStorage rather than in a signal alone so that reopening
// an address after a reload sends the same key, and it is a plain module
// rather than a provider because the profile allows the client two providers
// and this is neither of them.
const KEY = 'factory.principal';

export function principalKey(): string {
  return globalThis.localStorage.getItem(KEY) ?? '';
}

export function setPrincipalKey(key: string): void {
  globalThis.localStorage.setItem(KEY, key.trim());
}

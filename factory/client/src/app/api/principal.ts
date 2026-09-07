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

type Listener = (key: string) => void;

// Who reacts to a declaration or a change of the key, api/stream.ts among
// them: it holds no subscription while the key is empty and re-opens every
// one it holds once this fires. A plain callback registry rather than a
// signal, because setPrincipalKey is the one place the key changes from —
// nothing else in this client writes localStorage's copy of it — so a
// listener told synchronously here needs no reactivity graph to reach it.
const listeners = new Set<Listener>();

export function setPrincipalKey(key: string): void {
  const trimmed = key.trim();
  globalThis.localStorage.setItem(KEY, trimmed);
  for (const listen of listeners) {
    listen(trimmed);
  }
}

// Registers to be told the key every time setPrincipalKey runs. Returns the
// function that ends it, called when the holder closes.
export function onPrincipalDeclared(listener: Listener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

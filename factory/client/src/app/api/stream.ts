import { Injectable, Signal, signal } from '@angular/core';
import { HEADER_PRINCIPAL, HEADER_VERSION, factoryVersion } from './version';
import { onPrincipalDeclared, principalKey } from './principal';

export type StreamState = 'connected' | 'disconnected';

// The backoff a retry the reader drives itself waits: it starts at one
// second and doubles up to a cap of thirty, so a connection that keeps
// failing to open costs one request every thirty seconds and not a tight
// loop.
const INITIAL_RETRY_MS = 1000;
const MAX_RETRY_MS = 30000;

// One subscription, on one address. A screen holds exactly one and closes it
// when it leaves the address.
export interface AddressStream {
  // 'disconnected' once the subscription has dropped. Before the first
  // connection there is nothing stale to report and the screen is loading,
  // so the initial reading is 'connected'; what nothing here checks is a
  // transport that accepts the request and never delivers, which reads as
  // connected for as long as it lasts.
  readonly state: Signal<StreamState>;
  // Increments on every change the address reports, once more on every
  // reconnection, and once on every declaration, because re-establishing a
  // subscription re-reads the address whole rather than resuming from what
  // the screen held. A declaration is one of those: the subscription it
  // re-opens is a new one, and the screen holding a read made before it — a
  // read the factory refused for want of a key, most of the time — has to
  // make that read again.
  readonly changed: Signal<number>;
  close(): void;
}

@Injectable({ providedIn: 'root' })
export class StreamReader {
  // A list address — home, work, ops, factory, people — has one of each, so
  // it carries no id and "-" stands in its place, the way
  // ../../../../screens/stream.go reads it.
  subscribe(kind: string, id: string): AddressStream {
    const state = signal<StreamState>('connected');
    const changed = signal(0);

    let source: EventSource | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let retryDelay = INITIAL_RETRY_MS;
    let closed = false;

    const clearRetry = (): void => {
      if (retryTimer !== null) {
        clearTimeout(retryTimer);
        retryTimer = null;
      }
    };

    // (Re)opens the connection: closes whatever was open, and opens nothing
    // while no principal is declared, because the server refuses that with
    // no header this transport can carry a request header on, and a request
    // refused outright is never delivered to retry from.
    const open = (): void => {
      clearRetry();
      source?.close();
      source = null;
      const key = principalKey();
      if (key === '') {
        // Nothing to open a connection as. The state is left exactly as it
        // was rather than moved to 'disconnected': there is nothing stale to
        // report yet, and the screen already says what a human has to do
        // before anything can be read.
        return;
      }
      const address = new URL(`/api/stream/${kind}/${id}`, globalThis.location.origin);
      address.searchParams.set(HEADER_VERSION, factoryVersion());
      address.searchParams.set(HEADER_PRINCIPAL, key);

      const opened = new EventSource(address.toString());
      source = opened;
      opened.addEventListener('open', () => {
        retryDelay = INITIAL_RETRY_MS;
        if (state() === 'disconnected') {
          changed.update((n) => n + 1);
        }
        state.set('connected');
      });
      opened.addEventListener('error', () => {
        state.set('disconnected');
        // A connection that opened and then dropped is left to the
        // browser's own retry, which keeps readyState at CONNECTING between
        // attempts. One that never opened at all — a 400, 409, or 500 the
        // server answered the request with — settles at CLOSED and the
        // browser does not retry it; nothing else here would ever open it
        // again, so the reader retries it itself.
        if (opened.readyState === EventSource.CLOSED) {
          scheduleRetry();
        }
      });
      opened.addEventListener('changed', () => {
        changed.update((n) => n + 1);
      });
    };

    const scheduleRetry = (): void => {
      if (closed) {
        return;
      }
      clearRetry();
      retryTimer = setTimeout(() => {
        open();
      }, retryDelay);
      retryDelay = Math.min(retryDelay * 2, MAX_RETRY_MS);
    };

    const unlisten = onPrincipalDeclared(() => {
      open();
      // A declaration re-establishes this subscription, and re-establishing
      // one re-reads the address whole. It is reported here rather than left
      // to the connection's own 'open' event, which reports a reconnection
      // only where the state had already gone to 'disconnected': a screen
      // mounted before any key was declared is not disconnected — its first
      // read was refused for want of one — and without this it would hold
      // that refusal until a human read it again.
      changed.update((n) => n + 1);
    });

    open();

    return {
      state: state.asReadonly(),
      changed: changed.asReadonly(),
      close: () => {
        closed = true;
        clearRetry();
        unlisten();
        source?.close();
      },
    };
  }
}

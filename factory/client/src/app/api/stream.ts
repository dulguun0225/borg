import { Injectable, Signal, signal } from '@angular/core';
import { HEADER_PRINCIPAL, HEADER_VERSION, factoryVersion } from './version';
import { principalKey } from './principal';

export type StreamState = 'connected' | 'disconnected';

// One subscription, on one address. A screen holds exactly one and closes it
// when it leaves the address.
export interface AddressStream {
  // 'disconnected' once the subscription has dropped. Before the first
  // connection there is nothing stale to report and the screen is loading,
  // so the initial reading is 'connected'; what nothing here checks is a
  // transport that accepts the request and never delivers, which reads as
  // connected for as long as it lasts.
  readonly state: Signal<StreamState>;
  // Increments on every change the address reports and once more on every
  // reconnection, because re-establishing re-reads the address whole rather
  // than resuming from what the screen held.
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
    // EventSource cannot set a request header, so the two values every call
    // carries are on the query string here, under the same names the headers
    // use: ../../../../screens/version.go reads each from the header and,
    // where the header is absent, from the query string.
    const address = new URL(`/api/stream/${kind}/${id}`, globalThis.location.origin);
    address.searchParams.set(HEADER_VERSION, factoryVersion());
    address.searchParams.set(HEADER_PRINCIPAL, principalKey());

    const source = new EventSource(address.toString());
    source.addEventListener('open', () => {
      if (state() === 'disconnected') {
        changed.update((n) => n + 1);
      }
      state.set('connected');
    });
    source.addEventListener('error', () => {
      state.set('disconnected');
    });
    source.addEventListener('changed', () => {
      changed.update((n) => n + 1);
    });

    return {
      state: state.asReadonly(),
      changed: changed.asReadonly(),
      close: () => {
        source.close();
      },
    };
  }
}

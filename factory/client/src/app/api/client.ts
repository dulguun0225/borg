import { Injectable, PendingTasks, inject, signal } from '@angular/core';
import { HEADER_PRINCIPAL, HEADER_VERSION, factoryVersion } from './version';
import { principalKey } from './principal';

// What a read or a call answers. Three outcomes and no exception: a caller
// decides which of its declared states to move to by reading the outcome,
// which is what makes the state machine in ../state/screen-state.ts closed.
//
// 'reload' is the version refusal, which is never a failed action: the shell
// renders it as a required reload for the whole client, so the screen that
// made the call moves nowhere.
//
// 'absent' is the 404 an address, or a record a call names by id, resolves to
// nothing with. It is not a failure: for a read it is the screen's empty
// state, and for a call it is a record that is not there to be written.
export type ApiResult<T> =
  | { readonly outcome: 'value'; readonly value: T }
  | { readonly outcome: 'reload' }
  | { readonly outcome: 'absent' }
  | { readonly outcome: 'failed'; readonly message: string };

interface VersionRefusal {
  reload_required: boolean;
  expected: string;
}

interface ErrorBody {
  error: string;
}

@Injectable({ providedIn: 'root' })
export class ApiClient {
  private readonly tasks = inject(PendingTasks);
  private readonly refused = signal(false);

  // True once any call has been refused for its version. It never goes back:
  // the client that was built for the old shape is the thing at fault, and
  // only a reload replaces it.
  readonly reloadRequired = this.refused.asReadonly();

  async get<T>(path: string): Promise<ApiResult<T>> {
    return this.request<T>(path, { method: 'GET' });
  }

  // name is a method of package screens' Calls with a lowercased first
  // letter, which is the address ../../../../screens/call.go switches over.
  async call<T>(name: string, args: object): Promise<ApiResult<T | null>> {
    return this.request<T | null>(`/api/call/${name}`, {
      method: 'POST',
      body: JSON.stringify(args),
    });
  }

  // Every call blocks the application's reported stability while it is in
  // flight, so nothing reads a screen as settled with a read still open.
  private async request<T>(path: string, init: RequestInit): Promise<ApiResult<T>> {
    const done = this.tasks.add();
    try {
      return await this.answer<T>(path, init);
    } finally {
      done();
    }
  }

  private async answer<T>(path: string, init: RequestInit): Promise<ApiResult<T>> {
    // The server refuses a call with no principal, and its message names a
    // header rather than the thing a human has to do. Refusing here says what
    // that is and spends no request on it.
    if (principalKey() === '') {
      return {
        outcome: 'failed',
        message: 'no People key is declared: say who you are at the top of the screen',
      };
    }
    let response: Response;
    try {
      response = await fetch(path, {
        ...init,
        headers: {
          'Content-Type': 'application/json',
          [HEADER_VERSION]: factoryVersion(),
          [HEADER_PRINCIPAL]: principalKey(),
        },
      });
    } catch (cause: unknown) {
      return { outcome: 'failed', message: `the factory could not be reached: ${message(cause)}` };
    }
    if (response.status === 409) {
      const refusal = await body<VersionRefusal>(response);
      if (refusal?.reload_required === true) {
        this.refused.set(true);
        return { outcome: 'reload' };
      }
      return { outcome: 'failed', message: 'the factory refused the call' };
    }
    if (response.status === 404) {
      return { outcome: 'absent' };
    }
    if (response.status === 204) {
      return { outcome: 'value', value: null as T };
    }
    if (!response.ok) {
      const failure = await body<ErrorBody>(response);
      return {
        outcome: 'failed',
        message: failure?.error ?? `the factory answered ${response.status}`,
      };
    }
    const value = await body<T>(response);
    if (value === undefined) {
      return { outcome: 'failed', message: 'the factory answered a body this client cannot read' };
    }
    return { outcome: 'value', value };
  }
}

// undefined where the body is not the JSON the caller expected, which is a
// failure and not a value.
async function body<T>(response: Response): Promise<T | undefined> {
  try {
    return (await response.json()) as T;
  } catch {
    return undefined;
  }
}

function message(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}

// The two things a screen reaches the factory through, faked: the fetch the
// client calls and the EventSource the stream reader opens. Test-only, and the
// one thing outside api/ and state/ that a screen's spec imports.

export interface Recorded {
  path: string;
  method: string;
  body: string;
  // Every header the call carried, by name, the way a spec checks that the
  // version and the principal are on every call and not only the ones a
  // screen happens to assert a body for.
  headers: Record<string, string>;
}

export class FakeFetch {
  private readonly bodies = new Map<string, unknown>();
  private readonly failures = new Map<string, string>();
  private readonly refusals = new Set<string>();
  private readonly errors = new Map<string, { status: number; message: string }>();
  private readonly held: ((answer: Response) => void)[] = [];
  private holds = false;

  // Every request the screen made, in the order it made them.
  readonly recorded: Recorded[] = [];

  reset(): void {
    this.release();
    this.bodies.clear();
    this.failures.clear();
    this.refusals.clear();
    this.errors.clear();
    this.holds = false;
    this.recorded.length = 0;
  }

  // A 200 with this body.
  answering(path: string, body: unknown): void {
    this.bodies.set(path, body);
  }

  // A rejected fetch, which is the network failure the client reports.
  failing(path: string, message: string): void {
    this.failures.set(path, message);
  }

  // The version refusal: a 409 whose body says a reload is required.
  refusing(path: string, expected: string): void {
    this.refusals.add(path);
    this.bodies.set(path, { reload_required: true, expected });
  }

  // Any other refusal the server writes as {"error": "..."}.
  erroring(path: string, status: number, message: string): void {
    this.errors.set(path, { status, message });
  }

  // Nothing settles after this, which is how a screen is held in loading.
  holding(): void {
    this.holds = true;
  }

  // Settles everything holding() was holding, so a fixture driven into its
  // loading state can be left without a request outstanding.
  release(): void {
    this.holds = false;
    for (const settle of this.held) {
      settle(jsonResponse({}, 200));
    }
    this.held.length = 0;
  }

  sent(path: string): Recorded[] {
    return this.recorded.filter((each) => each.path === path);
  }

  readonly fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const address = addressOf(input);
    const path = address.split('?')[0] ?? address;
    this.recorded.push({
      path,
      method: init?.method ?? 'GET',
      body: typeof init?.body === 'string' ? init.body : '',
      headers: headersOf(init?.headers),
    });
    if (this.holds) {
      return new Promise<Response>((settle) => {
        // Settles only on release(): the caller stays in its loading state.
        this.held.push(settle);
      });
    }
    const failure = this.failures.get(path);
    if (failure !== undefined) {
      throw new Error(failure);
    }
    const error = this.errors.get(path);
    if (error !== undefined) {
      return jsonResponse({ error: error.message }, error.status);
    }
    if (this.refusals.has(path)) {
      return jsonResponse(this.bodies.get(path), 409);
    }
    if (this.bodies.has(path)) {
      return jsonResponse(this.bodies.get(path), 200);
    }
    return jsonResponse({ error: `no fake answer for ${path}` }, 404);
  };
}

// The client sends headers as a plain object literal, never a Headers
// instance or an array of pairs, so reading them as one is enough here; the
// other two shapes RequestInit['headers'] allows are not this client's.
function headersOf(headers: HeadersInit | undefined): Record<string, string> {
  if (headers === undefined || headers instanceof Headers || Array.isArray(headers)) {
    return {};
  }
  return { ...headers };
}

// The three things fetch takes as its first argument, each named rather than
// stringified: a Request stringifies to [object Request].
function addressOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') {
    return input;
  }
  if (input instanceof URL) {
    return input.toString();
  }
  return input.url;
}

function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

type Listener = (event: Event) => void;

export class FakeEventSource {
  // The three values the real EventSource's readyState takes, mirrored here
  // because the stream reader reads it off the constructor
  // (`EventSource.CLOSED`) and off the instance to tell a connection the
  // browser is still retrying on its own from one that failed to open and
  // never will again.
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;

  private readonly listeners = new Map<string, Listener[]>();
  closed = false;
  readyState: number = FakeEventSource.CONNECTING;

  constructor(readonly url: string) {}

  addEventListener(kind: string, listener: Listener): void {
    const at = this.listeners.get(kind) ?? [];
    at.push(listener);
    this.listeners.set(kind, at);
  }

  close(): void {
    this.closed = true;
    this.readyState = FakeEventSource.CLOSED;
  }

  // The subscription dropped the way a network interruption does: the
  // browser is left retrying on its own, so readyState stays CONNECTING and
  // the reader's own retry never fires.
  drop(): void {
    this.readyState = FakeEventSource.CONNECTING;
    this.fire('error');
  }

  // The connection never opened at all — a 400, 409, or 500 — which the
  // browser does not retry from: readyState settles at CLOSED and the
  // reader's own bounded-backoff retry is what has to open it again.
  dropClosed(): void {
    this.readyState = FakeEventSource.CLOSED;
    this.fire('error');
  }

  // The subscription came back, which re-reads the address whole.
  reconnect(): void {
    this.readyState = FakeEventSource.OPEN;
    this.fire('open');
  }

  // The address reported a change.
  change(): void {
    this.fire('changed');
  }

  private fire(kind: string): void {
    for (const listener of this.listeners.get(kind) ?? []) {
      listener(new Event(kind));
    }
  }
}

interface Fakes {
  net: FakeFetch;
  sources: FakeEventSource[];
}

const real = {
  fetch: globalThis.fetch,
  eventSource: globalThis.EventSource,
};

export function installFakes(): Fakes {
  const net = new FakeFetch();
  const sources: FakeEventSource[] = [];
  globalThis.fetch = net.fetch;
  // The stream reader holds the browser's own constructor, so the fake has to
  // stand in that constructor's place rather than be handed to it.
  globalThis.EventSource = class extends FakeEventSource {
    constructor(url: string | URL) {
      super(String(url));
      sources.push(this);
    }
  } as unknown as typeof EventSource;
  // Every call carries a principal, and the server refuses a call with none.
  globalThis.localStorage.setItem('factory.principal', 'owner');
  return { net, sources };
}

export function restoreFakes(): void {
  globalThis.fetch = real.fetch;
  globalThis.EventSource = real.eventSource;
}

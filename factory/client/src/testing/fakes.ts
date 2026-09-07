// The two things a screen reaches the factory through, faked: the fetch the
// client calls and the EventSource the stream reader opens. Test-only, and the
// one thing outside api/ and state/ that a screen's spec imports.

export interface Recorded {
  path: string;
  method: string;
  body: string;
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
  private readonly listeners = new Map<string, Listener[]>();
  closed = false;

  constructor(readonly url: string) {}

  addEventListener(kind: string, listener: Listener): void {
    const at = this.listeners.get(kind) ?? [];
    at.push(listener);
    this.listeners.set(kind, at);
  }

  close(): void {
    this.closed = true;
  }

  // The subscription dropped.
  drop(): void {
    this.fire('error');
  }

  // The subscription came back, which re-reads the address whole.
  reconnect(): void {
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

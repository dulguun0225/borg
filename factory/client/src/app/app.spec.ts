import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { Shell } from './app';
import { Home } from './api/types';
import { routes } from './app.routes';
import { factoryVersion } from './api/version';
import { predicateViolations } from './state/predicates';
import { FakeEventSource, FakeFetch, installFakes, restoreFakes } from '../testing/fakes';

const HOME: Home = {
  Badge: {
    Total: 2,
    PendingGates: 2,
    UATAssignments: 0,
    InterviewQuestions: 0,
    Escalations: 0,
    FactoryHoldsForAHuman: 0,
    ConstraintCausedStops: 0,
  },
  LastChecks: [],
  Readiness: [],
  Digest: null,
};

describe('Shell', () => {
  let net: FakeFetch;
  let sources: FakeEventSource[];

  beforeEach(() => {
    const fakes = installFakes();
    net = fakes.net;
    sources = fakes.sources;
  });

  afterEach(() => {
    net.release();
    TestBed.resetTestingModule();
    restoreFakes();
  });

  it('says nothing can be read until a key is declared, and sends no call', async () => {
    globalThis.localStorage.removeItem('factory.principal');
    net.answering('/api/home', HOME);
    const fixture = await shell();
    expect(net.sent('/api/home')).toEqual([]);
    expect((fixture.nativeElement as HTMLElement).textContent).toContain(
      'Nothing can be read until you say who you are',
    );
    expect(predicateViolations(fixture.nativeElement as Element)).toEqual([]);
    fixture.destroy();
  });

  it('carries the version and the principal on every call', async () => {
    net.answering('/api/home', HOME);
    const fixture = await shell();
    const sent = net.sent('/api/home');
    expect(sent.length).toBe(1);
    for (const call of sent) {
      expect(call.headers['X-Factory-Version']).toBe(factoryVersion());
      expect(call.headers['X-Factory-Principal']).toBe('owner');
    }
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('2 waiting on a human');
    fixture.destroy();
  });

  it('renders a version refusal as a required reload and nothing else', async () => {
    net.refusing('/api/home', 'a later factory');
    const fixture = await shell();
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('This screen was built for an earlier factory');
    expect(text).not.toContain('waiting on a human');
    expect(predicateViolations(fixture.nativeElement as Element)).toEqual([]);
    fixture.destroy();
  });

  it('says so when the badge subscription drops', async () => {
    net.answering('/api/home', HOME);
    const fixture = await shell();
    sources[0]?.drop();
    await fixture.whenStable();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain(
      'The subscription to the badge has dropped',
    );
    fixture.destroy();
  });

  it('decides every predicate over the shell it renders', async () => {
    net.answering('/api/home', HOME);
    const fixture = await shell();
    expect(predicateViolations(fixture.nativeElement as Element)).toEqual([]);
    fixture.destroy();
  });

  it('opens no subscription while no key is declared, and opens one once one is', async () => {
    globalThis.localStorage.removeItem('factory.principal');
    net.answering('/api/home', HOME);
    const fixture = await shell();
    expect(sources.length).toBe(0);

    const root = fixture.nativeElement as HTMLElement;
    const key = only(root.querySelector<HTMLInputElement>('#principal'), '#principal');
    key.value = 'someone';
    key.dispatchEvent(new Event('input'));
    key.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    only(root.querySelector('form.who'), 'form.who').dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(sources.length).toBe(1);
    fixture.destroy();
  });

  it('retries a subscription that failed to open, with a growing bounded backoff', async () => {
    net.answering('/api/home', HOME);
    const fixture = await shell();
    jasmine.clock().install();
    try {
      // The badge subscription failed to open outright — a 400, a 409, or a
      // 500 — which the browser does not retry from, so the reader does.
      sources[0]?.dropClosed();
      expect(sources.length).toBe(1);

      jasmine.clock().tick(999);
      expect(sources.length).toBe(1);
      jasmine.clock().tick(1);
      expect(sources.length).toBe(2);

      // The second failure waits twice as long, not the same second again.
      sources[1]?.dropClosed();
      jasmine.clock().tick(1999);
      expect(sources.length).toBe(2);
      jasmine.clock().tick(1);
      expect(sources.length).toBe(3);
    } finally {
      jasmine.clock().uninstall();
    }
    fixture.destroy();
  });

  async function shell(): Promise<ComponentFixture<Shell>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    sources.length = 0;
    const fixture = TestBed.createComponent(Shell);
    await fixture.whenStable();
    return fixture;
  }
});

// The Factory address dispatch's stop and a safeguard's rows carry — see
// ../../../cmd/factory/views.go's liftedAt and work/work.spec.ts's
// WORK_WITH_ROWS — resolves to a real route here and not the '**' redirect
// to Work: this is the whole app's routes, so it is the one place able to
// prove that rather than assume it of factory.routes.ts alone.
describe('the app routes', () => {
  afterEach(() => {
    TestBed.resetTestingModule();
  });

  it('resolves a stop link at the fleet to a real route rather than the catch-all', async () => {
    TestBed.configureTestingModule({ providers: [provideRouter(routes)] });
    const router = TestBed.inject(Router);
    const navigated = await router.navigateByUrl('/factory/fleet');
    expect(navigated).toBeTrue();
    expect(router.url).toBe('/factory/fleet');
  });

  it('resolves a stop link at constraints to a real route rather than the catch-all', async () => {
    TestBed.configureTestingModule({ providers: [provideRouter(routes)] });
    const router = TestBed.inject(Router);
    const navigated = await router.navigateByUrl('/factory/constraints');
    expect(navigated).toBeTrue();
    expect(router.url).toBe('/factory/constraints');
  });

  it('resolves one constraint at its own address', async () => {
    TestBed.configureTestingModule({ providers: [provideRouter(routes)] });
    const router = TestBed.inject(Router);
    const navigated = await router.navigateByUrl('/factory/constraints/C1');
    expect(navigated).toBeTrue();
    expect(router.url).toBe('/factory/constraints/C1');
  });
});

// The element a selector was written for, or a failure naming it: a spec that
// silently matched nothing would pass by asserting over an empty DOM.
function only<E extends Element>(found: E | null, what: string): E {
  if (found === null) {
    throw new Error(`nothing matched ${what}`);
  }
  return found;
}

import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { Shell } from './app';
import { Home } from './api/types';
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
    expect(net.sent('/api/home').length).toBe(1);
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

  async function shell(): Promise<ComponentFixture<Shell>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    sources.length = 0;
    const fixture = TestBed.createComponent(Shell);
    await fixture.whenStable();
    return fixture;
  }
});

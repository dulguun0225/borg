import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { ApiClient } from '../api/client';
import { Ops, Service } from '../api/types-ops';
import { ScreenState, malformations } from '../state/screen-state';
import { predicateViolations } from '../state/predicates';
import { FakeEventSource, FakeFetch, installFakes, restoreFakes } from '../../testing/fakes';
import { OpsScreen, opsMachine } from './ops';
import { ServiceScreen } from './service';

const BOARD: Ops = {
  Services: [
    {
      ServiceID: 'payments',
      EnvironmentID: 'production',
      CurrentRelease: 4,
      Health: 'watched with no way to reach passed',
    },
  ],
};

const SERVICE_ADDRESS = '/api/service/payments/on/production';

const SERVICE: Service = {
  ServiceID: 'payments',
  EnvironmentID: 'production',
  Targets: [
    {
      TargetID: 'eu-1',
      ReleaseNumber: 4,
      DeployID: 'dep-1',
      CompletedAt: '2026-09-04T10:00:00Z',
      Deploying: 'not_reached',
    },
    { TargetID: 'eu-2', ReleaseNumber: 3, DeployID: 'dep-2', CompletedAt: '', Deploying: '' },
  ],
  DriftMismatch: 'release 2',
  ContractsPublished: ['payments.v1'],
  OpenIncidents: [{ ID: 'inc-1', Quantity: 'error rate', OpenedAt: '2026-09-04T11:00:00Z' }],
  Rollouts: [{ ReleaseNumber: 4, ControlTargetID: '' }],
  LastChecks: [
    {
      Component: 'health monitor',
      Checks: 'payments',
      LastPass: '2026-09-04T04:10:00Z',
      IntervalSeconds: 300,
      FurtherPassOwed: false,
    },
  ],
  Windows: [
    {
      Quantity: 'error rate',
      Size: 0.02,
      FinestSizeReached: 0.05,
      Confidence: 0.95,
      Power: 0.8,
      PassedReachable: false,
      AverageRunLength: 400,
      AdmittedCrossings: 0.0025,
    },
  ],
  EmissionVersion: 'emission/2',
  Unmeasured: true,
  Mitigation: { ID: 'mit-1', TargetID: 'eu-1', Operation: 'shift_traffic', StandingHours: 3.5 },
};

describe('Ops screen', () => {
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

  it('declares a well-formed machine', () => {
    expect(malformations(opsMachine)).toEqual([]);
  });

  it('decides every predicate over every state the board declares', async () => {
    for (const state of opsMachine.states) {
      const fixture = await driveBoard(state);
      expect(predicateViolations(fixture.nativeElement as Element))
        .withContext(`the predicates over ${state}`)
        .toEqual([]);
      fixture.destroy();
      net.release();
    }
  });

  it('decides every predicate over every state a service on an environment declares', async () => {
    for (const state of opsMachine.states) {
      const fixture = await driveService(state);
      expect(predicateViolations(fixture.nativeElement as Element))
        .withContext(`the predicates over ${state} at a service`)
        .toEqual([]);
      fixture.destroy();
      net.release();
    }
  });

  it('shows every last check on the board and not only the stale ones', async () => {
    const fixture = await driveBoard('ready');
    const text = (fixture.nativeElement as HTMLElement).textContent;
    // FurtherPassOwed is false on this record, and the row is on the screen
    // anyway: Ops reads every one of them.
    expect(text).toContain('the health monitor has not checked payments since');
    expect(text).not.toContain('a further pass is owed');
    fixture.destroy();
  });

  it('shows the drift mismatch over the deploy record and a rollout without a control', async () => {
    const fixture = await driveBoard('ready');
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('the drift detector reads release 2 here, over the deploy');
    expect(text).toContain('without a control');
    fixture.destroy();
  });

  it('withholds the last check ages while the subscription is down', async () => {
    const fixture = await driveBoard('disconnected');
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('The subscription to this address has dropped');
    expect(text).not.toContain('has not checked payments since');
    fixture.destroy();
  });

  it('reports the window parameters, the emission version and unmeasured', async () => {
    const fixture = await driveService('ready');
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('not reachable at all');
    expect(text).toContain('emission/2');
    expect(text).toContain('Unmeasured');
    expect(text).toContain('shift_traffic on eu-1, standing 3.5 hours');
    fixture.destroy();
  });

  it('refuses a rollback with no reason and sends one with a reason', async () => {
    const fixture = await driveService('ready');
    const root = fixture.nativeElement as HTMLElement;
    const undo = only(root.querySelector('form'), 'the undo form');

    undo.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/rollBack')).toEqual([]);
    expect(root.textContent).toContain('an undo carries the reason it was undone');

    const reason = only(undo.querySelector('textarea'), 'the reason');
    reason.value = 'the error rate doubled on eu-1';
    reason.dispatchEvent(new Event('input'));
    reason.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    undo.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    const sent = net.sent('/api/call/rollBack');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, string>;
    expect(args['ServiceID']).toBe('payments');
    expect(args['Reason']).toBe('the error rate doubled on eu-1');
    fixture.destroy();
  });

  it('ends the mitigation standing by its id, taken off the view and not typed', async () => {
    const fixture = await driveService('ready');
    const root = fixture.nativeElement as HTMLElement;
    const button = only(
      [...root.querySelectorAll('button')].find((each) => each.textContent.trim() === 'End it') ??
        null,
      'the end-it button',
    );
    button.click();
    await fixture.whenStable();

    const sent = net.sent('/api/call/endMitigation');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, string>;
    expect(args['MitigationID']).toBe('mit-1');
    fixture.destroy();
  });

  it('marks a rollback not caused by the release from a button per target', async () => {
    const fixture = await driveService('ready');
    const root = fixture.nativeElement as HTMLElement;
    const reason = only(
      root.querySelector<HTMLTextAreaElement>('#not-caused-reason'),
      'the not-caused reason',
    );
    reason.value = 'the release did not touch this path';
    reason.dispatchEvent(new Event('input'));
    reason.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    const button = only(
      [...root.querySelectorAll('button')].find((each) => each.textContent.trim() === 'Mark it') ??
        null,
      'the mark-it button',
    );
    button.click();
    await fixture.whenStable();

    const sent = net.sent('/api/call/markRollbackNotCaused');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, string>;
    expect(args['DeployID']).toBe('dep-1');
    expect(args['Reason']).toBe('the release did not touch this path');
    fixture.destroy();
  });

  it('shows a deploy still widening on a target beside its running release', async () => {
    const fixture = await driveService('ready');
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('a deploy is still widening: not_reached');
    fixture.destroy();
  });

  it('refuses every action while the subscription is down', async () => {
    const fixture = await driveService('ready');
    const root = fixture.nativeElement as HTMLElement;
    const reason = only(root.querySelector('textarea'), 'the reason');
    reason.value = 'the error rate doubled on eu-1';
    reason.dispatchEvent(new Event('input'));
    reason.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    sources[0]?.drop();
    await fixture.whenStable();

    only(root.querySelector('form'), 'the undo form').dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/rollBack')).toEqual([]);
    expect(root.textContent).toContain('the subscription is down, so nothing was sent');
    fixture.destroy();
  });

  it('renders a version refusal as a required reload and never as a failed action', async () => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    net.refusing(SERVICE_ADDRESS, 'a later factory');
    const fixture = TestBed.createComponent(ServiceScreen);
    fixture.componentRef.setInput('serviceId', 'payments');
    fixture.componentRef.setInput('environmentId', 'production');
    await fixture.whenStable();

    expect(TestBed.inject(ApiClient).reloadRequired()).toBeTrue();
    expect((fixture.nativeElement as HTMLElement).textContent).not.toContain('could not be read');
    fixture.destroy();
  });

  async function driveBoard(state: ScreenState): Promise<ComponentFixture<OpsScreen>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    if (state === 'failed') {
      net.failing('/api/ops', 'the store could not be reached');
    } else if (state === 'empty') {
      net.answering('/api/ops', { Services: [] });
    } else {
      net.answering('/api/ops', BOARD);
      net.answering(SERVICE_ADDRESS, SERVICE);
    }
    if (state === 'loading') {
      net.holding();
    }
    const fixture = TestBed.createComponent(OpsScreen);
    if (state === 'loading') {
      // The read is outstanding, so stability is never reached and the render
      // is taken directly rather than waited for.
      fixture.detectChanges();
      return fixture;
    }
    await fixture.whenStable();
    if (state === 'disconnected') {
      sources[0]?.drop();
      await fixture.whenStable();
    }
    return fixture;
  }

  async function driveService(state: ScreenState): Promise<ComponentFixture<ServiceScreen>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    if (state === 'failed') {
      net.failing(SERVICE_ADDRESS, 'the store could not be reached');
    } else if (state === 'empty') {
      net.erroring(SERVICE_ADDRESS, 404, 'screens: no record at that address');
    } else {
      net.answering(SERVICE_ADDRESS, SERVICE);
    }
    if (state === 'loading') {
      net.holding();
    }
    const fixture = TestBed.createComponent(ServiceScreen);
    fixture.componentRef.setInput('serviceId', 'payments');
    fixture.componentRef.setInput('environmentId', 'production');
    if (state === 'loading') {
      fixture.detectChanges();
      return fixture;
    }
    await fixture.whenStable();
    if (state === 'disconnected') {
      sources[0]?.drop();
      await fixture.whenStable();
    }
    return fixture;
  }
});

// The element a selector was written for, or a failure naming it: a spec that
// silently matched nothing would pass by asserting over an empty DOM.
function only<E extends Element>(found: E | null, what: string): E {
  if (found === null) {
    throw new Error(`nothing matched ${what}`);
  }
  return found;
}

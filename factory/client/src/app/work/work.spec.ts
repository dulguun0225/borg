import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { ApiClient } from '../api/client';
import { AwaitingAdmission, Decision, Home, Item, Work } from '../api/types';
import { ScreenState, malformations } from '../state/screen-state';
import { predicateViolations } from '../state/predicates';
import { FakeEventSource, FakeFetch, installFakes, restoreFakes } from '../../testing/fakes';
import { WorkScreen, workMachine } from './work';
import { DecisionScreen } from './decision';
import { ItemScreen } from './item';

// An install where an owner placed neither safeguard on the report store:
// nothing waits for an admission, which is every home view but the one below.
const NOTHING_AWAITING: AwaitingAdmission = { Reports: null, Intents: null };

const HOME_WITH_ROWS: Home = {
  Badge: {
    Total: 3,
    PendingGates: 1,
    UATAssignments: 1,
    InterviewQuestions: 1,
    Escalations: 0,
    FactoryHoldsForAHuman: 0,
    ConstraintCausedStops: 0,
    Admissions: 0,
  },
  LastChecks: [
    {
      Component: 'health monitor',
      Checks: 'payments',
      LastPass: '2026-09-01T04:10:00Z',
      IntervalSeconds: 300,
      FurtherPassOwed: true,
    },
  ],
  Readiness: [
    {
      Role: 'implementer',
      EntryCovers: false,
      RolePromptInForce: false,
      OldestUnmatchedHoldAgeSeconds: 7200,
    },
  ],
  Awaiting: NOTHING_AWAITING,
  Digest: null,
};

const HOME_AT_ZERO: Home = {
  Badge: {
    Total: 0,
    PendingGates: 0,
    UATAssignments: 0,
    InterviewQuestions: 0,
    Escalations: 0,
    FactoryHoldsForAHuman: 0,
    ConstraintCausedStops: 0,
    Admissions: 0,
  },
  LastChecks: [],
  Readiness: [],
  Awaiting: NOTHING_AWAITING,
  Digest: { Releases: 4, Decisions: 9, AutoApprovals: 7 },
};

const WORK_WITH_ROWS: Work = {
  Rows: [
    { ItemID: 'I1', Stage: 'spec', Priority: 2, Waiting: 'a human is holding', Stop: null },
    {
      ItemID: 'I2',
      Stage: 'implementation',
      Priority: 1,
      Waiting: '',
      Stop: {
        Cause: 'no fleet entry covers this role',
        Since: '2026-09-02T09:00:00Z',
        // The real value ../../../../cmd/factory/views.go's liftedAt emits for
        // this cause — see app.spec.ts for the proof that this address
        // resolves to a route and not the '**' redirect to Work.
        LiftedAt: '/factory/fleet',
      },
    },
  ],
};

const PENDING_DECISION: Decision = {
  OpenEventID: 'D1',
  ItemID: 'I1',
  GateRow: 'Spec',
  OpenedAt: '2026-09-03T08:00:00Z',
  OpenedInWorkAt: '',
  Vector: { blast: 'wide', novelty: 'high' },
  Score: null,
  VersionsInForce: ['spec v3'],
  RoutedToDuty: 6,
  RoutedToHuman: 'owner',
  Acknowledgements: [{ HumanKey: 'owner', At: '2026-09-03T08:05:00Z' }],
  Closed: null,
  Abandoned: null,
  Deliveries: [
    { Channel: 'mail', Recipient: 'owner@example.com', Accepted: true, FirstAcceptedAt: '2026-09-03T08:01:00Z' },
  ],
};

const ITEM_BASE: Item = {
  ID: 'I1',
  IntentID: '',
  IntentStatement: 'let a customer export their invoices',
  Reports: null,
  IntentAwaitsAdmission: false,
  Versions: [],
  Decisions: [],
  ImplementationDiff: '',
  Release: null,
  Deploys: [],
  Windows: [],
  PartlyDelivered: false,
  Stop: null,
  FactoryHold: 'a legal hold stands on this service',
  ApprovableThrough: false,
};

const ITEM_APPROVABLE: Item = { ...ITEM_BASE, ApprovableThrough: true };

// One report waiting on a human and one already admitted, which is every state
// a report is rendered in: the harm mark and the notice on the first, neither
// on the second.
describe('Work screen', () => {
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
    expect(malformations(workMachine)).toEqual([]);
  });

  it('decides every predicate over every state it declares', async () => {
    for (const state of workMachine.states) {
      const fixture = await driveHome(state);
      expect(predicateViolations(fixture.nativeElement as Element))
        .withContext(`the predicates over ${state}`)
        .toEqual([]);
      fixture.destroy();
      net.release();
    }
  });

  it('names each last check by what it checks and never by a duration', async () => {
    const fixture = await driveHome('ready');
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('the health monitor has not checked payments since');
    fixture.destroy();
  });

  it('withholds the last check rows while the subscription is down', async () => {
    const fixture = await driveHome('disconnected');
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('The subscription to this address has dropped');
    expect(text).not.toContain('has not checked payments since');
    fixture.destroy();
  });

  it('shows the digest only where nothing waits', async () => {
    const zero = await driveHome('empty');
    expect((zero.nativeElement as HTMLElement).textContent).toContain('4 releases shipped');
    zero.destroy();

    const busy = await driveHome('ready');
    expect((busy.nativeElement as HTMLElement).textContent).not.toContain('releases shipped');
    busy.destroy();
  });

  it('renders the required reload for a version refusal and sends nothing', async () => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    net.refusing('/api/decision/D1', 'a later factory');
    const fixture = TestBed.createComponent(DecisionScreen);
    fixture.componentRef.setInput('id', 'D1');
    await fixture.whenStable();

    expect(TestBed.inject(ApiClient).reloadRequired()).toBeTrue();
    // A version refusal is a required reload and never a failed action: the
    // screen reports no failure of its own.
    expect((fixture.nativeElement as HTMLElement).textContent).not.toContain('could not be read');
    expect(net.sent('/api/call/acknowledge')).toEqual([]);
    fixture.destroy();
  });

  it('sends one call when "I have this row" is clicked twice while the first is in flight', async () => {
    const fixture = await driveDecision(PENDING_DECISION);
    const root = fixture.nativeElement as HTMLElement;
    const button = only(root.querySelector('button'), 'button');
    // Both dispatches run synchronously, back to back: the first sets the
    // screen's busy signal before it ever awaits anything, so the second
    // reaches the same guard and sends nothing.
    button.dispatchEvent(new Event('click'));
    button.dispatchEvent(new Event('click'));
    await fixture.whenStable();

    expect(net.sent('/api/call/acknowledge').length).toBe(1);
    fixture.destroy();
  });

  it('refuses an action while the subscription is down and marks the view stale', async () => {
    const fixture = await driveDecision(PENDING_DECISION);
    sources[0]?.drop();
    await fixture.whenStable();

    const root = fixture.nativeElement as HTMLElement;
    expect(root.textContent).toContain('The subscription to this address has dropped');

    only(root.querySelector('button'), 'button').click();
    await fixture.whenStable();

    expect(net.sent('/api/call/acknowledge')).toEqual([]);
    expect(root.textContent).toContain('the subscription is down, so nothing was sent');
    fixture.destroy();
  });

  it('refuses a reject with no reason and writes one with a reason', async () => {
    const fixture = await driveDecision(PENDING_DECISION);
    const root = fixture.nativeElement as HTMLElement;

    const verdict = only(root.querySelector('select'), 'select');
    verdict.value = 'reject';
    verdict.dispatchEvent(new Event('input'));
    verdict.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    const form = only(root.querySelector('form'), 'form');
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/decide')).toEqual([]);
    expect(root.textContent).toContain('a reject and a hold each carry a reason');

    const reason = only(root.querySelector('textarea'), 'textarea');
    reason.value = 'the criteria omit the unwanted condition';
    reason.dispatchEvent(new Event('input'));
    reason.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    const sent = net.sent('/api/call/decide');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, string>;
    expect(args['Verdict']).toBe('reject');
    expect(args['Reason']).toBe('the criteria omit the unwanted condition');
    // The one field no caller has ever filled: when the actor opened the row
    // here, in UTC.
    expect(args['OpenedInWorkAt']).toMatch(/^\d{4}-\d{2}-\d{2}T[\d:.]+Z$/);
    fixture.destroy();
  });

  it('decides every predicate over every state the decision address declares', async () => {
    for (const state of workMachine.states) {
      const fixture = await driveDecisionState(state);
      expect(predicateViolations(fixture.nativeElement as Element))
        .withContext(`the predicates over ${state} at a decision`)
        .toEqual([]);
      fixture.destroy();
      net.release();
    }
  });

  it('reads a 404 at an address as empty and not as a failure', async () => {
    const fixture = await driveDecisionState('empty');
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('There is no decision at this address');
    expect(text).not.toContain('could not be read');
    fixture.destroy();
  });

  it('sends nothing where the row was closed since it was drawn', async () => {
    const fixture = await driveDecision(PENDING_DECISION);
    net.answering('/api/decision/D1', {
      ...PENDING_DECISION,
      Closed: { Verdict: 'approve', Reason: '', At: '2026-09-03T09:00:00Z', Actor: 'sibling' },
    });

    const root = fixture.nativeElement as HTMLElement;
    only(root.querySelector('button'), 'button').click();
    await fixture.whenStable();

    expect(net.sent('/api/call/acknowledge')).toEqual([]);
    expect(root.textContent).toContain('this row was closed since it was drawn');
    fixture.destroy();
  });

  it('puts every entry with no time of its own last, sorted stably by label', async () => {
    const item: Item = {
      ...ITEM_BASE,
      Versions: [{ ID: 'v1', Kind: 'spec', AuthoredAt: '2026-09-01T00:00:00Z' }],
      Windows: [{ ID: 'B', Exit: '' }],
      Release: { ServiceID: 'aaa', Number: 9 },
    };
    const fixture = await driveItem(item);
    const root = fixture.nativeElement as HTMLElement;
    const rows = [...root.querySelectorAll('.row')].map((row) => row.textContent);

    expect(rows.length).toBe(3);
    expect(rows[0]).toContain('spec version v1');
    // Neither the window nor the release carries a time of its own: both
    // sort after the version, and between themselves by label — "aaa
    // release 9" before "window B is open" — rather than by whichever the
    // array happened to hold first.
    expect(rows[1]).toContain('aaa release 9');
    expect(rows[2]).toContain('window B is open');
    fixture.destroy();
  });

  it('offers approving through a factory hold only where it is approvable, and posts it', async () => {
    const held = await driveItem(ITEM_BASE);
    expect((held.nativeElement as HTMLElement).textContent).toContain(
      'a legal hold stands on this service',
    );
    expect((held.nativeElement as HTMLElement).querySelector('#approve-through-reason')).toBeNull();
    held.destroy();

    const fixture = await driveItem(ITEM_APPROVABLE);
    const root = fixture.nativeElement as HTMLElement;
    const reason = only(
      root.querySelector<HTMLTextAreaElement>('#approve-through-reason'),
      '#approve-through-reason',
    );
    reason.value = 'the customer cannot wait for the regular row';
    reason.dispatchEvent(new Event('input'));
    reason.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    only(reason.closest('form'), 'the approve-through-hold form').dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    const sent = net.sent('/api/call/approveThroughHold');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, string>;
    expect(args['ItemID']).toBe('I1');
    expect(args['Reason']).toBe('the customer cannot wait for the regular row');
    expect(args['OpenedInWorkAt']).toMatch(/^\d{4}-\d{2}-\d{2}T[\d:.]+Z$/);
    fixture.destroy();
  });



  async function driveHome(state: ScreenState): Promise<ComponentFixture<WorkScreen>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    if (state === 'failed') {
      net.failing('/api/home', 'the store could not be reached');
      net.answering('/api/work', WORK_WITH_ROWS);
    } else if (state === 'empty') {
      net.answering('/api/home', HOME_AT_ZERO);
      net.answering('/api/work', { Rows: [] });
    } else {
      net.answering('/api/home', HOME_WITH_ROWS);
      net.answering('/api/work', WORK_WITH_ROWS);
    }
    if (state === 'loading') {
      net.holding();
    }
    const fixture = TestBed.createComponent(WorkScreen);
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

  async function driveDecisionState(state: ScreenState): Promise<ComponentFixture<DecisionScreen>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    if (state === 'failed') {
      net.failing('/api/decision/D1', 'the store could not be reached');
    } else if (state === 'empty') {
      net.erroring('/api/decision/D1', 404, 'screens: no record at that address');
    } else {
      net.answering('/api/decision/D1', PENDING_DECISION);
    }
    if (state === 'loading') {
      net.holding();
    }
    const fixture = TestBed.createComponent(DecisionScreen);
    fixture.componentRef.setInput('id', 'D1');
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

  async function driveDecision(row: Decision): Promise<ComponentFixture<DecisionScreen>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    net.answering('/api/decision/D1', row);
    const fixture = TestBed.createComponent(DecisionScreen);
    fixture.componentRef.setInput('id', 'D1');
    await fixture.whenStable();
    return fixture;
  }

  async function driveItem(item: Item): Promise<ComponentFixture<ItemScreen>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    net.answering('/api/item/I1', item);
    const fixture = TestBed.createComponent(ItemScreen);
    fixture.componentRef.setInput('id', 'I1');
    await fixture.whenStable();
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

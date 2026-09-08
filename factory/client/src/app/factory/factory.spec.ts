import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { ApiClient } from '../api/client';
import { Home } from '../api/types';
import { Factory } from '../api/types-factory';
import { ScreenState, malformations } from '../state/screen-state';
import { predicateViolations } from '../state/predicates';
import { FakeEventSource, FakeFetch, installFakes, restoreFakes } from '../../testing/fakes';
import { FactoryScreen, factoryMachine } from './factory';

const NOTHING_WAITS: Home = {
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
  Readiness: [
    { Role: 'implementer', EntryCovers: false, RolePromptInForce: true, OldestUnmatchedHoldAgeSeconds: 7200 },
  ],
  Awaiting: { Reports: null, Intents: null },
  Digest: null,
};

const NOTHING_READY: Home = { ...NOTHING_WAITS, Readiness: [] };

const AUTHORED: Factory = {
  Parameters: [
    { Name: 'risk_threshold', Subject: 'payments', Value: '0.7', Source: 'clamped by a safeguard' },
  ],
  Safeguards: [
    {
      ID: 'saf-1',
      Parameter: 'risk_threshold',
      Subject: 'service payments',
      Direction: 'no higher than',
      Bound: '0.7',
      RoutedToDuty: 9,
      RoutedToHuman: '',
    },
  ],
  Halts: [{ ID: 'hal-1', Reason: 'the provider is down', At: '2026-09-05T08:00:00Z' }],
  LegalHolds: [
    {
      ID: 'leg-1',
      Subject: 'service payments',
      Reason: 'a regulator asked',
      At: '2026-09-05T09:00:00Z',
    },
  ],
  Environments: [{ ID: 'production', Kind: 'production', Targets: ['eu-1', 'eu-2'] }],
  FleetEntries: [
    {
      ID: 'fle-1',
      ModelVersion: 'a-model/3',
      Effort: 'high',
      Role: 'implementer',
      ScopeProjectID: '',
      ScopeServiceID: '',
      ScopeAreaID: '',
      Credential: 'the-account',
      ProcessingLocation: 'eu',
      MaterialClasses: ['repository', 'constraints'],
      ReadAtOnceBound: 200000,
      DispatchesBetweenEvalRuns: 50,
    },
  ],
  LentCredentials: [
    {
      Name: 'the-account',
      LenderKey: 'owner',
      LenderName: 'The Owner',
      Kind: 'organisation',
      TakenBack: false,
    },
    {
      Name: 'a-taken-back-account',
      LenderKey: 'owner',
      LenderName: 'The Owner',
      Kind: 'organisation',
      TakenBack: true,
    },
  ],
  RolePrompts: [{ Role: 'implementer', VersionInForce: 'ver-9', AwaitingGateVersion: 'ver-10' }],
  FleetProposals: [],
  Constraints: [
    {
      ID: 'con-1',
      Kind: 'regulation',
      Reach: 'area',
      Statement: 'payments data stays in the union',
      SuppliedAt: '2026-09-01T07:00:00Z',
      BindsFrom: '2026-10-01',
      ReviewDate: '',
      Zone: 'Europe/Berlin',
      WithdrawnAt: '',
      Replaces: '',
    },
    {
      ID: 'con-2',
      Kind: 'law',
      Reach: 'factory',
      Statement: 'no personal data reaches a model',
      SuppliedAt: '2026-08-01T07:00:00Z',
      BindsFrom: '',
      ReviewDate: '2027-01-01',
      Zone: 'Europe/Berlin',
      WithdrawnAt: '',
      Replaces: '',
    },
  ],
  Projects: [{ ID: 'prj-1', Name: 'the-shop' }],
  Areas: [
    { ID: 'are-1', Name: 'payments', Inside: 'the-shop', ProjectID: 'prj-1', Severity: 'regulated' },
  ],
  Numbers: {
    ThroughputPerService: { payments: 4 },
    ReworkRate: 0.25,
    GateRejectionRate: { Spec: 0.1 },
    CostPerFeature: [{ ModelVersion: 'a-model/3', Amount: 12000, Currency: '', IsTotal: false }],
    CostMeasured: false,
    IntentOutcomes: [{ IntentID: 'int-1', Source: 'detector', Outcome: '' }],
  },
  ReportChannel: { Ungrouped: 2, RefusedOverTheChannel: 5, Services: [], OnAnOldWayIn: [] },
  StoppedAtDispatch: [{ Cause: 'no fleet entry covers this role', Count: 2 }],
  ResolvedFactorGates: [{ Factor: 'novelty', Count: 3 }],
  HumanLoad: [
    {
      Duty: 6,
      HumanKey: 'owner',
      WaitingNow: 1,
      MedianWaitSeconds: 7200,
      MedianOpenInFrontSeconds: 600,
    },
  ],
  ApproveUndone: [
    { HumanKey: '', Approved: 40, Undone: 3 },
    { HumanKey: 'owner', Approved: 12, Undone: 1 },
  ],
  SelfApprovalCounts: [{ HumanKey: 'owner', Count: 2 }],
  AutoPassRates: [{ FactorSet: 'set-1', Threshold: 0.7, Realized: 0.8, Recorded: 0.75 }],
  HeldOutBands: [
    { FactorSet: 'set-1', Band: '0.6 to 0.8', FailedShare: 0.2, ResolvedCount: 5 },
  ],
  SpendCeilings: [
    {
      Credential: 'the-account',
      Ceiling: 0,
      Currency: '',
      BurnRate: 0,
      ProjectedExhaustion: '',
      Unbounded: true,
      UnpricedRuns: 0,
    },
  ],
  PageChannel: {
    PagesPerService: { payments: 2 },
    PagesPerHuman: { owner: 2 },
    ShareUnacknowledgedAtWiden: 0.5,
    AcknowledgedToAnsweredSeconds: 900,
    HumanFiredPages: 1,
  },
  LoadSplits: [
    {
      Duty: 6,
      HumanKey: 'owner',
      BeforeFirstDeliverySeconds: 60,
      BetweenDeliveryAndAcknowledgementSeconds: 1800,
      AfterFirstAcknowledgementSeconds: 300,
    },
  ],
  RecordDecidingRows: [
    { Kind: 'a_safeguards_withdrawal', RecordID: 'saf-1', OpenedAt: '2026-09-06T08:00:00Z' },
  ],
  RolePromptGateRow: {
    Role: 'implementer',
    VersionID: 'ver-10',
    OpenedAt: '2026-09-06T09:00:00Z',
  },
  Seam5Enforced: false,
};

const NOTHING_AUTHORED: Factory = {
  ...AUTHORED,
  Parameters: [],
  Safeguards: [],
  Halts: [],
  LegalHolds: [],
  Environments: [],
  FleetEntries: [],
  RolePrompts: [],
  Constraints: [],
  Projects: [],
  Areas: [],
  StoppedAtDispatch: [],
  RecordDecidingRows: [],
  RolePromptGateRow: null,
};

describe('Factory screen', () => {
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
    expect(malformations(factoryMachine)).toEqual([]);
  });

  it('decides every predicate over every state it declares', async () => {
    for (const state of factoryMachine.states) {
      const fixture = await drive(state);
      expect(predicateViolations(fixture.nativeElement as Element))
        .withContext(`the predicates over ${state}`)
        .toEqual([]);
      fixture.destroy();
      net.release();
    }
  });

  it('names every section the design puts on this screen', async () => {
    const fixture = await drive('ready');
    const headings = [...(fixture.nativeElement as HTMLElement).querySelectorAll('h2')].map(
      (each) => each.textContent.trim(),
    );
    expect(headings).toEqual([
      'Readiness',
      'Items stopped at dispatch',
      'The fleet',
      'Role prompts',
      'Fleet proposals',
      'Rows that decide a record',
      'Gate policy',
      'Environments',
      'Projects and areas',
      'Constraints in force',
      'The report channel',
      'The numbers',
      'Gates a resolved factor put a human at',
      "The human's load",
      'The auto-pass rate against the rate the threshold was authored at',
      'Spend ceilings',
      'The page channel',
    ]);
    fixture.destroy();
  });

  it('says the factory does not measure cost where a rate is missing', async () => {
    const fixture = await drive('ready');
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('The factory does not measure cost');
    expect(text).toContain('Units per intent');
    expect(text).toContain('unbounded');
    fixture.destroy();
  });

  it('lists the constraints in force by reach, widest first', async () => {
    const fixture = await drive('ready');
    const rows = [
      ...(fixture.nativeElement as HTMLElement).querySelectorAll('table'),
    ].flatMap((table) => [...table.querySelectorAll('tbody tr')]);
    const reaches = rows
      .map((row) => row.querySelector('td')?.textContent.trim() ?? '')
      .filter((each) => each === 'factory' || each === 'area');
    expect(reaches).toEqual(['factory', 'area']);
    fixture.destroy();
  });

  it("shows an area's project, and shows and posts a gate-row safeguard's service", async () => {
    const fixture = await drive('ready');
    const root = fixture.nativeElement as HTMLElement;
    expect(root.textContent).toContain('prj-1');
    expect(root.querySelector('#safeguard-service')).toBeNull();
    await fill(fixture, '#safeguard-kind', 'gate_row');
    await fill(fixture, '#safeguard-parameter', 'risk_threshold');
    await fill(fixture, '#safeguard-subject', 'implementation');
    await fill(fixture, '#safeguard-service', 'payments');
    await fill(fixture, '#safeguard-bound', '0.7');
    const form = only(root.querySelector('#safeguard-parameter'), '#safeguard-parameter').closest('form');
    form?.dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    const args = JSON.parse(net.sent('/api/call/placeSafeguard')[0]?.body ?? '{}') as Record<string, string>;
    expect(args['ServiceName']).toBe('payments');
    fixture.destroy();
  });

  it('says no fleet proposal is waiting, nothing writing one yet', async () => {
    const fixture = await drive('ready');
    expect((fixture.nativeElement as HTMLElement).textContent).toContain(
      'No fleet proposal is waiting on a disposition',
    );
    fixture.destroy();
  });

  it('refuses a fleet entry whose reads-at-once is zero and sends one above zero', async () => {
    const fixture = await drive('ready');
    const root = fixture.nativeElement as HTMLElement;
    const entry = only(only(root.querySelector('#entry-model'), '#entry-model').closest('form'), 'the entry form');

    await fill(fixture, '#entry-model', 'a-model/3');
    await fill(fixture, '#entry-role', 'implementer');
    await fill(fixture, '#entry-credential', 'the-account');
    await fill(fixture, '#entry-location', 'eu');
    await fill(fixture, '#entry-dispatches', '50');

    entry.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/writeFleetEntry')).toEqual([]);
    expect(root.textContent).toContain('how much the model reads at once is above zero');

    await fill(fixture, '#entry-reads', '200000');
    entry.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    const sent = net.sent('/api/call/writeFleetEntry');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, unknown>;
    expect(args['ReadAtOnceBound']).toBe(200000);
    expect(args['Role']).toBe('implementer');
    fixture.destroy();
  });

  it('sends the fleet entry scope as three fields and the chosen credential name', async () => {
    const fixture = await drive('ready');
    const root = fixture.nativeElement as HTMLElement;
    const entry = only(
      only(root.querySelector('#entry-model'), '#entry-model').closest('form'),
      'the entry form',
    );

    await fill(fixture, '#entry-model', 'a-model/3');
    await fill(fixture, '#entry-role', 'implementer');
    await fill(fixture, '#entry-scope-project', 'prj-1');
    await fill(fixture, '#entry-scope-service', 'payments');
    await fill(fixture, '#entry-scope-area', 'are-1');
    await fill(fixture, '#entry-credential', 'the-account');
    await fill(fixture, '#entry-location', 'eu');
    await fill(fixture, '#entry-reads', '200000');
    await fill(fixture, '#entry-dispatches', '50');

    entry.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    const sent = net.sent('/api/call/writeFleetEntry');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, unknown>;
    expect(args['ScopeProjectName']).toBe('prj-1');
    expect(args['ScopeServiceName']).toBe('payments');
    expect(args['ScopeAreaName']).toBe('are-1');
    expect(args['Credential']).toBe('the-account');
    fixture.destroy();
  });

  it('sends edit in place on the role-prompt row to editRecordRow and not decideRecordRow', async () => {
    const fixture = await drive('ready');
    const root = fixture.nativeElement as HTMLElement;

    await fill(fixture, '#row-verdict', 'edit_in_place');
    const text = only(root.querySelector<HTMLTextAreaElement>('#row-text'), '#row-text');
    text.value = 'ver-11';
    text.dispatchEvent(new Event('input'));
    text.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    const form = only(
      only(root.querySelector('#row-verdict'), '#row-verdict').closest('form'),
      'the row form',
    );
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/decideRecordRow')).toEqual([]);
    const sent = net.sent('/api/call/editRecordRow');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, string>;
    expect(args['Row']).toBe('a_role_prompt_or_a_skill');
    expect(args['RecordID']).toBe('ver-10');
    expect(args['Version']).toBe('ver-11');
    fixture.destroy();
  });

  it('keeps a half-written form through a change on the address', async () => {
    const fixture = await drive('ready');
    const root = fixture.nativeElement as HTMLElement;
    await fill(fixture, '#entry-model', 'a-model/3');

    // The address reported a change, which re-reads it whole and takes the
    // screen through its loading state. The sections must not be destroyed by
    // that: what a human has half written is not a record.
    sources[0]?.change();
    await fixture.whenStable();

    const model = only(root.querySelector<HTMLInputElement>('#entry-model'), '#entry-model');
    expect(model.value).toBe('a-model/3');
    fixture.destroy();
  });

  it('refuses every action while the subscription is down', async () => {
    const fixture = await drive('ready');
    const root = fixture.nativeElement as HTMLElement;
    sources[0]?.drop();
    await fixture.whenStable();

    only(root.querySelector('button'), 'the first action').click();
    await fixture.whenStable();

    expect(net.sent('/api/call/withdrawFleetEntry')).toEqual([]);
    expect(root.textContent).toContain('the subscription is down, so nothing was sent');
    fixture.destroy();
  });

  it('renders a version refusal as a required reload and never as a failed action', async () => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    net.refusing('/api/factory', 'a later factory');
    net.refusing('/api/home', 'a later factory');
    const fixture = TestBed.createComponent(FactoryScreen);
    await fixture.whenStable();

    expect(TestBed.inject(ApiClient).reloadRequired()).toBeTrue();
    expect((fixture.nativeElement as HTMLElement).textContent).not.toContain('could not be read');
    fixture.destroy();
  });

  async function drive(state: ScreenState): Promise<ComponentFixture<FactoryScreen>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    if (state === 'failed') {
      net.failing('/api/factory', 'the store could not be reached');
      net.answering('/api/home', NOTHING_WAITS);
    } else if (state === 'empty') {
      net.answering('/api/factory', NOTHING_AUTHORED);
      net.answering('/api/home', NOTHING_READY);
    } else {
      net.answering('/api/factory', AUTHORED);
      net.answering('/api/home', NOTHING_WAITS);
    }
    if (state === 'loading') {
      net.holding();
    }
    const fixture = TestBed.createComponent(FactoryScreen);
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

  async function fill(
    fixture: ComponentFixture<FactoryScreen>,
    selector: string,
    value: string,
  ): Promise<void> {
    const root = fixture.nativeElement as HTMLElement;
    const control = only(
      root.querySelector<HTMLInputElement | HTMLSelectElement>(selector),
      selector,
    );
    control.value = value;
    control.dispatchEvent(new Event('input'));
    control.dispatchEvent(new Event('change'));
    await fixture.whenStable();
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

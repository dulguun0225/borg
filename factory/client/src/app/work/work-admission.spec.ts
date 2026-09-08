// The two admissions a safeguard on the report store adds a human with: the
// report and the intent each holds, where each is rendered, and the call each
// control sends. Split from work.spec.ts by subject at the length a file is
// held to.
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { AwaitingAdmission, Home, IntentAwaitingAdmission, Item, ReportSummary } from '../api/types';
import { predicateViolations } from '../state/predicates';
import { FakeEventSource, FakeFetch, installFakes, restoreFakes } from '../../testing/fakes';
import { atInstant } from './format';
import { WorkScreen } from './work';
import { ItemScreen } from './item';

const NOTHING_AWAITING: AwaitingAdmission = { Reports: null, Intents: null };

const ITEM_BASE: Item = {
  ID: 'I1',
  IntentID: 'int_1',
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
  FactoryHold: '',
  ApprovableThrough: false,
};

const REPORTS: ReportSummary[] = [
  {
    ID: 'rep_1', Kind: 'bug', HarmMarked: true, CollectedAt: '2026-09-04T07:00:00Z',
    NoticeID: 'con_1', Admitted: false, Text: 'the export button does nothing',
  },
  {
    ID: 'rep_2', Kind: 'complaint', HarmMarked: false, CollectedAt: '2026-09-04T08:00:00Z',
    NoticeID: '', Admitted: true, Text: 'exporting invoices takes too long',
  },
];

const ITEM_WITH_REPORTS: Item = { ...ITEM_BASE, Reports: REPORTS };

// The report and the intent a safeguard on the report store holds, and the
// home view that shows both. Neither has an item behind it, which is why they
// are rows of the home view and of no board.
const AWAITING_REPORT: ReportSummary = {
  ID: 'rep_waiting',
  Kind: 'bug',
  HarmMarked: true,
  CollectedAt: '2026-09-04T07:00:00Z',
  NoticeID: '',
  Admitted: false,
  Text: 'saving the form hangs and never finishes',
};

const AWAITING_INTENT: IntentAwaitingAdmission = {
  IntentID: 'in_waiting',
  Statement: '2 end-user report(s) grouped as one problem',
  ArrivedAt: '2026-09-04T07:05:00Z',
  Reports: 2,
  HarmMarked: false,
};

const HOME_AWAITING_ADMISSION: Home = {
  Badge: {
    Total: 2,
    PendingGates: 0,
    UATAssignments: 0,
    InterviewQuestions: 0,
    Escalations: 0,
    FactoryHoldsForAHuman: 0,
    ConstraintCausedStops: 0,
    Admissions: 2,
  },
  LastChecks: [],
  Readiness: [],
  Awaiting: { Reports: [AWAITING_REPORT], Intents: [AWAITING_INTENT] },
  Digest: null,
};

// A home view with something else waiting and neither safeguard placed, which
// is every install that never asked for the wait. The readiness row is what
// keeps the screen off its empty state, where the badge is not rendered at all.
const HOME_AT_REST: Home = {
  Badge: {
    Total: 1,
    PendingGates: 0,
    UATAssignments: 0,
    InterviewQuestions: 0,
    Escalations: 0,
    FactoryHoldsForAHuman: 1,
    ConstraintCausedStops: 0,
    Admissions: 0,
  },
  LastChecks: [],
  Readiness: [
    {
      Role: 'implementer',
      EntryCovers: false,
      RolePromptInForce: false,
      OldestUnmatchedHoldAgeSeconds: 0,
    },
  ],
  Awaiting: NOTHING_AWAITING,
  Digest: null,
};

describe('The two admissions on the report store', () => {
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

  it('renders each report under the intent and admits one of them', async () => {
    const fixture = await driveItem(ITEM_WITH_REPORTS);
    const root = fixture.nativeElement as HTMLElement;
    const rows = [...root.querySelectorAll('ul .row')].map((row) => row.textContent);

    expect(rows.length).toBe(2);
    expect(rows[0]).toContain('the export button does nothing');
    expect(rows[0]).toContain('harming a person');
    expect(rows[0]).toContain('It was shown notice con_1');
    // The arrival is rendered in the viewer's zone by format.ts and never as
    // the UTC the record carries.
    expect(rows[0]).toContain(atInstant('2026-09-04T07:00:00Z'));
    expect(rows[0]).not.toContain('2026-09-04T07:00:00Z');
    expect(rows[1]).toContain('No notice was in force');
    expect(rows[1]).toContain('A human has admitted this report');
    expect(predicateViolations(root))
      .withContext('the predicates over an item carrying reports in both states')
      .toEqual([]);

    only(root.querySelector<HTMLButtonElement>('ul .row button'), 'the admit control').click();
    await fixture.whenStable();
    expect(net.sent('/api/call/admitReport')[0]?.body).toBe('{"ReportID":"rep_1"}');
    fixture.destroy();
  });

  it('offers admitting the intent only while the intent itself waits', async () => {
    // The two admissions are separate: a group whose every report is admitted
    // is still an intent nobody has admitted, and the control follows the
    // intent's own field rather than a reading over the reports.
    const settled = await driveItem({
      ...ITEM_WITH_REPORTS,
      Reports: REPORTS.map((each) => ({ ...each, Admitted: true })),
    });
    expect((settled.nativeElement as HTMLElement).textContent).not.toContain('Admit this intent');
    settled.destroy();

    const fixture = await driveItem({ ...ITEM_WITH_REPORTS, IntentAwaitsAdmission: true });
    const root = fixture.nativeElement as HTMLElement;
    const admitting = [...root.querySelectorAll<HTMLButtonElement>('button')].find((each) =>
      each.textContent.includes('Admit this intent'),
    );
    only(admitting ?? null, 'the control admitting the intent').click();
    await fixture.whenStable();
    expect(net.sent('/api/call/admitIntent')[0]?.body).toBe('{"IntentID":"int_1"}');
    fixture.destroy();
  });

  it('shows nothing waiting for an admission where no safeguard holds one', async () => {
    const fixture = await driveHome(HOME_AT_REST);
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('0 waiting for an admission');
    expect(text).not.toContain('Reports waiting to be admitted');
    expect(text).not.toContain('Intents waiting to be admitted');
    fixture.destroy();
  });

  it('renders the report and the intent a safeguard holds, and admits each', async () => {
    const fixture = await driveHome(HOME_AWAITING_ADMISSION);
    const root = fixture.nativeElement as HTMLElement;
    const text = root.textContent;

    expect(text).toContain('2 waiting for an admission');
    expect(text).toContain('saving the form hangs and never finishes');
    expect(text).toContain('harming a person');
    // The arrival is rendered in the viewer's zone by format.ts and never as
    // the UTC the record carries.
    expect(text).toContain(atInstant('2026-09-04T07:00:00Z'));
    expect(text).not.toContain('2026-09-04T07:00:00Z');
    expect(text).toContain('2 report(s) grouped');
    expect(text).toContain('2 end-user report(s) grouped as one problem');
    expect(predicateViolations(root))
      .withContext('the predicates over a home view holding both admissions')
      .toEqual([]);

    const buttons = [...root.querySelectorAll<HTMLButtonElement>('button')];
    only(
      buttons.find((each) => each.textContent.includes('Admit this report')) ?? null,
      'the control admitting the report',
    ).click();
    await fixture.whenStable();
    expect(net.sent('/api/call/admitReport')[0]?.body).toBe('{"ReportID":"rep_waiting"}');

    only(
      buttons.find((each) => each.textContent.includes('Admit this intent')) ?? null,
      'the control admitting the intent',
    ).click();
    await fixture.whenStable();
    expect(net.sent('/api/call/admitIntent')[0]?.body).toBe('{"IntentID":"in_waiting"}');
    fixture.destroy();
  });

  async function driveHome(home: Home): Promise<ComponentFixture<WorkScreen>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    net.answering('/api/home', home);
    net.answering('/api/work', { Rows: [] });
    const fixture = TestBed.createComponent(WorkScreen);
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

import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { Home } from '../api/types';
import { Factory } from '../api/types-factory';
import { FakeEventSource, FakeFetch, installFakes, restoreFakes } from '../../testing/fakes';
import { FactoryScreen } from './factory';
import { spansTyped } from './factory-reports';

// Tests for factory-reports.ts — the report channel's numbers and the erasure
// an owner performs here — live in this file rather than in factory.spec.ts:
// that file is at the 500-line bound, and ./README.md names the two files this
// screen's specs are split into.

const CHANNEL: Factory = {
  Parameters: [],
  Safeguards: [],
  Halts: [],
  LegalHolds: [],
  Environments: [],
  FleetEntries: [],
  LentCredentials: [],
  RolePrompts: [],
  FleetProposals: [],
  Constraints: [],
  Projects: [],
  Areas: [],
  Numbers: {
    ThroughputPerService: {},
    ReworkRate: 0,
    GateRejectionRate: {},
    CostPerFeature: [],
    CostMeasured: false,
    IntentOutcomes: [
      {
        IntentID: 'int-1',
        Source: 'reports',
        Outcome: 'the report rate: 3.00 a day before the release, 0.20 a day after',
      },
      { IntentID: 'int-2', Source: 'detector', Outcome: '' },
    ],
  },
  ReportChannel: {
    Ungrouped: 2,
    RefusedOverTheChannel: 5,
    Services: [
      {
        ServiceID: 'svc-1',
        ServiceName: 'payments',
        Refused: 3,
        NoNoticeInForce: true,
      },
      {
        ServiceID: 'svc-2',
        ServiceName: 'invoicing',
        Refused: 0,
        NoNoticeInForce: false,
      },
    ],
    OnAnOldWayIn: [
      {
        ServiceID: 'svc-1',
        ServiceName: 'payments',
        Identity: 'an-earlier-release',
        FactoryIdentity: 'unstamped',
      },
    ],
  },
  StoppedAtDispatch: [],
  ResolvedFactorGates: [],
  HumanLoad: [],
  ApproveUndone: [],
  SelfApprovalCounts: [],
  AutoPassRates: [],
  HeldOutBands: [],
  SpendCeilings: [],
  PageChannel: {
    PagesPerService: {},
    PagesPerHuman: {},
    ShareUnacknowledgedAtWiden: 0,
    AcknowledgedToAnsweredSeconds: 0,
    HumanFiredPages: 0,
  },
  LoadSplits: [],
  RecordDecidingRows: [],
  RolePromptGateRow: null,
  Seam5Enforced: false,
};

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
  Readiness: [],
  Awaiting: { Reports: null, Intents: null },
  Digest: null,
};

describe('The report channel at Factory', () => {
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

  it('reports the ungrouped count and the refusals', async () => {
    const fixture = await drive();
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('2 report(s) waiting to be grouped');
    expect(text).toContain('5 refused over the whole channel');
    expect(text).toContain('payments');
    expect(text).not.toContain('Submissions the store could not read');
    fixture.destroy();
  });

  it('lists the services under no notice and those serving an old way in', async () => {
    const fixture = await drive();
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('none in force, so the way in shows none');
    expect(text).toContain('built under an-earlier-release, where this factory is unstamped');
    // The service whose project has a notice is not on the list under it.
    expect(text).not.toContain('invoicing: 0 refused');
    fixture.destroy();
  });

  it("reads each intent's outcome beside cost per feature, and none as an answer", async () => {
    const fixture = await drive();
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('the report rate: 3.00 a day before the release, 0.20 a day after');
    expect(text).toContain('none: the factory raised it');
    fixture.destroy();
  });

  it('erases only on a second click over the same typed spans', async () => {
    const fixture = await drive();
    await fill(fixture, '#erasure-report', 'rep_1');
    await fill(fixture, '#erasure-spans', '12-20 44-51');
    await fill(fixture, '#erasure-reason', 'the words name a person');

    const form = erasureForm(fixture);
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    expect(net.sent('/api/call/performErasure')).toEqual([]);
    expect((fixture.nativeElement as HTMLElement).textContent).toContain(
      'nothing puts them back',
    );

    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    const sent = net.sent('/api/call/performErasure');
    expect(sent.length).toBe(1);
    expect(JSON.parse(sent[0]?.body ?? '{}')).toEqual({
      ReportID: 'rep_1',
      Spans: [
        { Start: 12, End: 20 },
        { Start: 44, End: 51 },
      ],
      Reason: 'the words name a person',
    });
    fixture.destroy();
  });

  it('un-arms the erasure when a span is edited after it was armed', async () => {
    const fixture = await drive();
    await fill(fixture, '#erasure-report', 'rep_1');
    await fill(fixture, '#erasure-spans', '12-20');
    await fill(fixture, '#erasure-reason', 'the words name a person');

    const form = erasureForm(fixture);
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain(
      'nothing puts them back',
    );

    await fill(fixture, '#erasure-spans', '30-40');
    expect((fixture.nativeElement as HTMLElement).textContent).not.toContain(
      'nothing puts them back',
    );
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    expect(net.sent('/api/call/performErasure')).toEqual([]);
    fixture.destroy();
  });

  it('sends nothing where the spans are not byte ranges', async () => {
    const fixture = await drive();
    await fill(fixture, '#erasure-report', 'rep_1');
    await fill(fixture, '#erasure-spans', 'the second sentence');
    await fill(fixture, '#erasure-reason', 'the words name a person');

    const form = erasureForm(fixture);
    form.dispatchEvent(new Event('submit'));
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    expect(net.sent('/api/call/performErasure')).toEqual([]);
    expect((fixture.nativeElement as HTMLElement).textContent).toContain(
      'the spans are start-end byte ranges',
    );
    fixture.destroy();
  });

  it('reads a span only as a half-open range of bytes', () => {
    expect(spansTyped('0-4')).toEqual([{ Start: 0, End: 4 }]);
    expect(spansTyped('  3-9   11-12 ')).toEqual([
      { Start: 3, End: 9 },
      { Start: 11, End: 12 },
    ]);
    // Nothing, a range that removes nothing, a backwards range, and anything
    // that is not two numbers are each refused rather than sent.
    expect(spansTyped('')).toBeNull();
    expect(spansTyped('4-4')).toBeNull();
    expect(spansTyped('9-3')).toBeNull();
    expect(spansTyped('12')).toBeNull();
    expect(spansTyped('a-b')).toBeNull();
  });

  async function drive(): Promise<ComponentFixture<FactoryScreen>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    net.answering('/api/factory', CHANNEL);
    net.answering('/api/home', NOTHING_WAITS);
    const fixture = TestBed.createComponent(FactoryScreen);
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

function erasureForm(fixture: ComponentFixture<FactoryScreen>): HTMLFormElement {
  const root = fixture.nativeElement as HTMLElement;
  const control = only(root.querySelector<HTMLInputElement>('#erasure-report'), '#erasure-report');
  return only(control.closest('form'), 'the erasure form');
}

async function fill(
  fixture: ComponentFixture<FactoryScreen>,
  selector: string,
  value: string,
): Promise<void> {
  const root = fixture.nativeElement as HTMLElement;
  const control = only(root.querySelector<HTMLInputElement>(selector), selector);
  control.value = value;
  control.dispatchEvent(new Event('input'));
  control.dispatchEvent(new Event('change'));
  await fixture.whenStable();
}

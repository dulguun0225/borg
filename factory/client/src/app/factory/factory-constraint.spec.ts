import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { Constraint, Home } from '../api/types';
import { Factory } from '../api/types-factory';
import { ScreenState } from '../state/screen-state';
import { predicateViolations } from '../state/predicates';
import { FakeEventSource, FakeFetch, installFakes, restoreFakes } from '../../testing/fakes';
import { FactoryScreen, factoryMachine } from './factory';
import { FactoryConstraintScreen } from './factory-constraint';

// Tests for factory-constraint.ts, and for the two child routes that scroll
// factory.ts to a section, live here rather than in factory.spec.ts: that
// file is at the 500-line bound, and ./README.md names this file as the
// screen's one departure from a spec per screen directory.

const CONSTRAINT: Constraint = {
  ID: 'C1',
  Kind: 'document',
  Reach: 'factory',
  Statement: 'no release touches a payment provider on a Friday',
  SuppliedAt: '2026-09-01T00:00:00Z',
  BindsFrom: '',
  ReviewDate: '2027-01-01',
  Zone: 'UTC',
  WithdrawnAt: '',
  Replaces: '',
};

// Nothing is authored anywhere; the screen is in its empty state, which
// still renders every section's form — a section holds no state machine of
// its own, and a busy signal set while sending a call is exactly as needed
// there as in the ready state.
const NOTHING: Factory = {
  Parameters: [],
  Safeguards: [],
  Halts: [],
  LegalHolds: [],
  Environments: [],
  FleetEntries: [],
  LentCredentials: [],
  RolePrompts: [],
  FleetProposals: [],
  Constraints: [{ ...CONSTRAINT }],
  Projects: [],
  Areas: [],
  Numbers: {
    ThroughputPerService: {},
    ReworkRate: 0,
    GateRejectionRate: {},
    CostPerFeature: [],
    CostMeasured: false,
    IntentOutcomes: [],
  },
  ReportChannel: { Ungrouped: 0, RefusedOverTheChannel: 0, Services: [], OnAnOldWayIn: [] },
  StoppedAtDispatch: [],
  ResolvedFactorGates: [],
  HumanLoad: [],
  ApproveUndone: [],
  SelfApprovalCounts: [],
  AutoPassRates: [],
  HeldOutBands: [],
  SpendCeilings: [
    {
      Credential: 'the-account',
      Ceiling: 100,
      Currency: 'USD',
      BurnRate: 40,
      ProjectedExhaustion: '',
      Unbounded: false,
      UnpricedRuns: 3,
    },
  ],
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

describe('Factory screen sections and the constraint address', () => {
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

  it('scrolls to the section a child route names, once, when the screen is ready', async () => {
    const fixture = await drive();
    const root = fixture.nativeElement as HTMLElement;
    const target = only(root.querySelector('#fleet'), '#fleet');
    const spy = spyOn(target, 'scrollIntoView');

    fixture.componentRef.setInput('section', 'fleet');
    await fixture.whenStable();
    expect(spy).toHaveBeenCalledTimes(1);

    // A change on the address re-reads it, which the section is not scrolled
    // to again for: it is shown once, not on every re-read.
    sources[0]?.change();
    await fixture.whenStable();
    expect(spy).toHaveBeenCalledTimes(1);
    fixture.destroy();
  });

  it('sends one call when a form is submitted twice while the first is in flight', async () => {
    const fixture = await drive();
    const root = fixture.nativeElement as HTMLElement;
    const name = only(root.querySelector<HTMLInputElement>('#project-name'), '#project-name');
    name.value = 'invoices';
    name.dispatchEvent(new Event('input'));
    name.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    const form = only(name.closest('form'), 'the project form');
    // Both dispatches run synchronously, back to back: the first sets the
    // screen's busy signal before it ever awaits anything, so the second
    // reaches the same guard and sends nothing.
    form.dispatchEvent(new Event('submit'));
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/createProject').length).toBe(1);
    fixture.destroy();
  });

  it('retires a service only on a second click confirming the same name', async () => {
    const fixture = await drive();
    const root = fixture.nativeElement as HTMLElement;
    const id = only(root.querySelector<HTMLInputElement>('#retire-service-id'), '#retire-service-id');
    id.value = 'payments';
    id.dispatchEvent(new Event('input'));
    id.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    const form = only(id.closest('form'), 'the retire form');
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/retireService')).toEqual([]);
    expect(root.textContent).toContain('Confirm retiring payments');

    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    const sent = net.sent('/api/call/retireService');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, string>;
    expect(args['ServiceID']).toBe('payments');
    fixture.destroy();
  });

  it('un-arms a retirement confirm when the name is edited', async () => {
    const fixture = await drive();
    const root = fixture.nativeElement as HTMLElement;
    const id = only(root.querySelector<HTMLInputElement>('#retire-service-id'), '#retire-service-id');
    id.value = 'payments';
    id.dispatchEvent(new Event('input'));
    id.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    const form = only(id.closest('form'), 'the retire form');
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    expect(root.textContent).toContain('Confirm retiring payments');

    id.value = 'invoicing';
    id.dispatchEvent(new Event('input'));
    id.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/retireService')).toEqual([]);
    expect(root.textContent).toContain('Confirm retiring invoicing');
    fixture.destroy();
  });

  it('ends a project only on a second click confirming the same name', async () => {
    const fixture = await drive();
    const root = fixture.nativeElement as HTMLElement;
    const name = only(root.querySelector<HTMLInputElement>('#end-project-name'), '#end-project-name');
    name.value = 'invoices';
    name.dispatchEvent(new Event('input'));
    name.dispatchEvent(new Event('change'));
    await fixture.whenStable();

    const form = only(name.closest('form'), 'the end-project form');
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/endProject')).toEqual([]);
    expect(root.textContent).toContain('Confirm ending invoices');

    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    const sent = net.sent('/api/call/endProject');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, string>;
    expect(args['ProjectName']).toBe('invoices');
    fixture.destroy();
  });

  it('links a constraint in force to its own address', async () => {
    const fixture = await drive();
    const root = fixture.nativeElement as HTMLElement;
    const link = only(root.querySelector<HTMLAnchorElement>('a[href="/factory/constraints/C1"]'), 'the constraint link');
    expect(link.textContent).toContain('C1');
    fixture.destroy();
  });

  it('renders seam 5 as not enforced and turns it on with one click', async () => {
    const fixture = await drive();
    const root = fixture.nativeElement as HTMLElement;
    expect(root.textContent).toContain('Seam 5 is not enforced');
    const button = only(
      root.querySelector<HTMLButtonElement>('#seam5-turn-on'),
      '#seam5-turn-on',
    );
    button.click();
    button.click();
    await fixture.whenStable();

    const sent = net.sent('/api/call/setSeam5Enforced');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, boolean>;
    expect(args['Enforced']).toBe(true);
    fixture.destroy();
  });

  it('sends whether a supplied constraint requires seam 5 enforced', async () => {
    const fixture = await drive();
    const root = fixture.nativeElement as HTMLElement;
    await fill(fixture, '#constraint-statement', 'no release on a Friday');
    const checkbox = only(
      root.querySelector<HTMLInputElement>('#constraint-requires-seam5'),
      '#constraint-requires-seam5',
    );
    checkbox.click();
    await fixture.whenStable();

    const form = only(checkbox.closest('form'), 'the constraint form');
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    const sent = net.sent('/api/call/supplyConstraint');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, unknown>;
    expect(args['RequiresSeam5Enforced']).toBe(true);
    fixture.destroy();
  });

  it('sends the kind a supplied constraint was chosen with', async () => {
    const fixture = await drive();
    await fill(fixture, '#constraint-statement', 'no release on a Friday');
    await fill(fixture, '#constraint-kind', 'notice');
    await fill(fixture, '#constraint-reach-name', 'checkout');

    const root = fixture.nativeElement as HTMLElement;
    const form = only(
      root.querySelector<HTMLElement>('#constraint-kind')?.closest('form') ?? null,
      'the constraint form',
    );
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    const sent = net.sent('/api/call/supplyConstraint');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, unknown>;
    expect(args['Kind']).toBe('notice');
    fixture.destroy();
  });

  it('shows unpriced runs as a lower bound on the burn rate', async () => {
    const fixture = await drive();
    const root = fixture.nativeElement as HTMLElement;
    expect(root.textContent).toContain('3 runs this period could not be priced');
    expect(root.textContent).toContain('lower bound');
    fixture.destroy();
  });

  it('decides every predicate over every state the constraint address declares', async () => {
    for (const state of factoryMachine.states) {
      const fixture = await driveConstraint(state);
      expect(predicateViolations(fixture.nativeElement as Element))
        .withContext(`the predicates over ${state} at a constraint`)
        .toEqual([]);
      fixture.destroy();
      net.release();
    }
  });

  it('reads a 404 at a constraint address as empty and not as a failure', async () => {
    const fixture = await driveConstraint('empty');
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('There is no constraint at this address');
    expect(text).not.toContain('could not be read');
    fixture.destroy();
  });

  it('shows a constraint the same way it is listed', async () => {
    const fixture = await driveConstraint('ready');
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain(CONSTRAINT.Statement);
    expect(text).toContain('factory');
    fixture.destroy();
  });

  async function drive(): Promise<ComponentFixture<FactoryScreen>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    net.answering('/api/factory', NOTHING);
    net.answering('/api/home', NOTHING_WAITS);
    const fixture = TestBed.createComponent(FactoryScreen);
    await fixture.whenStable();
    return fixture;
  }

  async function driveConstraint(state: ScreenState): Promise<ComponentFixture<FactoryConstraintScreen>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    if (state === 'failed') {
      net.failing('/api/constraint/C1', 'the store could not be reached');
    } else if (state === 'empty') {
      net.erroring('/api/constraint/C1', 404, 'screens: no record at that address');
    } else {
      net.answering('/api/constraint/C1', CONSTRAINT);
    }
    if (state === 'loading') {
      net.holding();
    }
    const fixture = TestBed.createComponent(FactoryConstraintScreen);
    fixture.componentRef.setInput('id', 'C1');
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

async function fill(
  fixture: ComponentFixture<FactoryScreen>,
  selector: string,
  value: string,
): Promise<void> {
  const root = fixture.nativeElement as HTMLElement;
  const control = only(
    root.querySelector<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>(selector),
    selector,
  );
  control.value = value;
  control.dispatchEvent(new Event('input'));
  control.dispatchEvent(new Event('change'));
  await fixture.whenStable();
}

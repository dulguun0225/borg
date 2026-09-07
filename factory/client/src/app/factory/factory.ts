import { Component, OnDestroy, computed, effect, inject, input, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ApiClient } from '../api/client';
import { StreamReader } from '../api/stream';
import { Home } from '../api/types';
import { Factory } from '../api/types-factory';
import { ScreenMachine, ScreenState, screenState } from '../state/screen-state';
import { spanOf } from './format';
import { CallRequest } from './request';
import { FactoryFleetSection } from './factory-fleet';
import { FactoryPolicySection } from './factory-policy';
import { FactoryPlacesSection } from './factory-places';
import { FactoryNumbersSection } from './factory-numbers';

// The state machine this screen declares. Empty, loading and failed are what
// the Spec gate requires of every screen the factory builds, disconnected is
// the fourth the subscription adds, and the machine is closed: a transition it
// does not declare is forbidden. Nothing writes it into the screenstatemachine
// record — the factory writes no spec version for itself, so the client's own
// build holds it.
export const factoryMachine: ScreenMachine = {
  states: ['loading', 'ready', 'empty', 'failed', 'disconnected'],
  initial: 'loading',
  terminal: [],
  transitions: [
    { from: 'loading', on: 'read answered rows', to: 'ready' },
    { from: 'loading', on: 'read answered nothing', to: 'empty' },
    { from: 'loading', on: 'read failed', to: 'failed' },
    { from: 'loading', on: 'subscription dropped', to: 'disconnected' },
    { from: 'ready', on: 'address changed', to: 'loading' },
    { from: 'ready', on: 'subscription dropped', to: 'disconnected' },
    { from: 'empty', on: 'address changed', to: 'loading' },
    { from: 'empty', on: 'subscription dropped', to: 'disconnected' },
    { from: 'failed', on: 'read again', to: 'loading' },
    { from: 'failed', on: 'subscription dropped', to: 'disconnected' },
    { from: 'disconnected', on: 'subscription re-established', to: 'loading' },
  ],
};

// The machine itself: the readiness reading and the items stopped at dispatch,
// the fleet and the role prompts, the rows that decide a record rather than an
// item, gate policy, environments, projects and areas, the constraints in
// force, and the factory's own numbers.
//
// Two reads answer it. The readiness reading per role is a field of the home
// view rather than of the Factory view, and it is on this screen because the
// record that covers a role — a fleet entry, a role prompt version — is
// authored here; the one subscription on this address keeps both true.
//
// The screen holds the one path every call takes, and the three sections below
// hold the rows and the forms: a section emits the call it wants made and
// performs none itself, so the rule that an action re-reads the address first
// and is refused while the subscription is down is written once.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md,
// ../../../../../end-goal/how-the-factory-works/11-screens/04-what-the-factory-auto-approved-and-what-was-undone.md
// and
// ../../../../../end-goal/how-the-factory-works/11-screens/05-the-page-channel-and-what-reached-a-human.md.
@Component({
  selector: 'factory-factory-screen',
  imports: [
    RouterLink,
    FactoryFleetSection,
    FactoryPolicySection,
    FactoryPlacesSection,
    FactoryNumbersSection,
  ],
  templateUrl: './factory.html',
  styleUrl: './factory.css',
})
export class FactoryScreen implements OnDestroy {
  private readonly api = inject(ApiClient);
  private readonly streams = inject(StreamReader);

  private readonly stream = this.streams.subscribe('factory', '-');
  private readonly reading = signal(true);
  private readonly failure = signal('');
  private readonly refusal = signal('');
  private readonly view = signal<Factory | null>(null);
  private readonly home = signal<Home | null>(null);
  // Set for the span of the one path every call takes, so a second click on
  // any section's form while one is already in flight has nothing to send.
  private readonly working = signal(false);
  // The section a dead link elsewhere in the client names in its child
  // route's data — "fleet" or "constraints" — and scrolls to once this
  // screen is ready. Empty for the screen's own address, which scrolls
  // nowhere.
  readonly section = input('');
  private readonly scrolledTo = signal('');

  // The instant a row deciding a record was opened here, in UTC, carried on
  // every verdict written from this screen. It is taken once, when the screen
  // is created, because what a close event records is when the actor started
  // reading and not when the button was pressed.
  protected readonly openedInWorkAt = new Date().toISOString();

  protected readonly machine = factoryMachine;
  protected readonly message = this.failure.asReadonly();
  protected readonly refused = this.refusal.asReadonly();
  protected readonly busy = this.working.asReadonly();
  protected readonly factory = this.view.asReadonly();
  protected readonly readiness = computed(() => this.home()?.Readiness ?? []);
  protected readonly stopped = computed(() => this.view()?.StoppedAtDispatch ?? []);
  protected readonly disconnected = computed(() => this.stream.state() === 'disconnected');

  // A fresh install has none of these, and the screen is empty until an owner
  // has authored one of them or dispatch has stopped something.
  private readonly authored = computed(() => {
    const view = this.view();
    if (view === null) {
      return 0;
    }
    return (
      (view.Parameters ?? []).length +
      (view.Safeguards ?? []).length +
      (view.Halts ?? []).length +
      (view.LegalHolds ?? []).length +
      (view.Environments ?? []).length +
      (view.FleetEntries ?? []).length +
      (view.RolePrompts ?? []).length +
      (view.Projects ?? []).length +
      (view.Areas ?? []).length +
      (view.Constraints ?? []).length +
      (view.RecordDecidingRows ?? []).length +
      (view.StoppedAtDispatch ?? []).length +
      this.readiness().length
    );
  });

  protected readonly state = computed<ScreenState>(() =>
    screenState(
      this.reading(),
      this.failure() !== '',
      this.disconnected(),
      this.authored() === 0,
    ),
  );

  constructor() {
    effect(() => {
      // Read so that every change on this address, and every reconnection,
      // re-reads it whole rather than resuming from what the screen held.
      this.stream.changed();
      void this.read();
    });
    effect(() => {
      // A dead link elsewhere named a section of this screen rather than a
      // route of its own; once this screen is ready and has rendered it,
      // this is where it is shown. Scrolled to once per declared section,
      // not on every re-read the subscription's own effect above causes.
      const target = this.section();
      if (target === '' || this.state() !== 'ready' || this.scrolledTo() === target) {
        return;
      }
      document.getElementById(target)?.scrollIntoView();
      this.scrolledTo.set(target);
    });
  }

  ngOnDestroy(): void {
    this.stream.close();
  }

  protected span(seconds: number): string {
    return spanOf(seconds);
  }

  protected async read(): Promise<void> {
    this.reading.set(true);
    const factory = await this.api.get<Factory>('/api/factory');
    const home = await this.api.get<Home>('/api/home');
    this.reading.set(false);
    if (factory.outcome === 'failed') {
      this.failure.set(factory.message);
      return;
    }
    if (home.outcome === 'failed') {
      this.failure.set(home.message);
      return;
    }
    if (factory.outcome === 'absent' || home.outcome === 'absent') {
      // Nothing is at this address, which is the screen's empty state.
      this.failure.set('');
      this.view.set(null);
      this.home.set(null);
      return;
    }
    if (factory.outcome !== 'value' || home.outcome !== 'value') {
      // The version refusal, which the shell renders as a required reload.
      // The screen moves nowhere.
      return;
    }
    this.failure.set('');
    this.view.set(factory.value);
    this.home.set(home.value);
  }

  // The one path every call from this screen takes: refused while the
  // subscription is down, re-reading the address before it sends, and
  // re-reading it after. A second invocation while one is already in flight,
  // from any section's form, is ignored: the template disables every
  // submitting control while busy() is true.
  protected async send(request: CallRequest): Promise<void> {
    if (this.working()) {
      return;
    }
    this.working.set(true);
    try {
      this.refusal.set('');
      if (this.disconnected()) {
        this.refusal.set('the subscription is down, so nothing was sent');
        return;
      }
      const fresh = await this.api.get<Factory>('/api/factory');
      if (fresh.outcome === 'failed') {
        this.refusal.set(`the screen could not be re-read, so nothing was sent: ${fresh.message}`);
        return;
      }
      if (fresh.outcome === 'absent') {
        this.refusal.set('the factory holds nothing at this address, so nothing was sent');
        this.view.set(null);
        return;
      }
      if (fresh.outcome !== 'value') {
        return;
      }
      this.view.set(fresh.value);
      const result = await this.api.call(request.name, request.args);
      if (result.outcome === 'absent') {
        this.refusal.set('the factory holds no record at that address, so nothing changed');
        return;
      }
      if (result.outcome === 'failed') {
        this.refusal.set(result.message);
        return;
      }
      this.refusal.set('the factory took it');
      await this.read();
    } finally {
      this.working.set(false);
    }
  }

  protected requested(request: CallRequest): void {
    void this.send(request);
  }
}

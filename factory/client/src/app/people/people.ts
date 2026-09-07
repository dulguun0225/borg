import { Component, OnDestroy, computed, effect, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ApiClient } from '../api/client';
import { StreamReader } from '../api/stream';
import { People } from '../api/types-people';
import { ScreenMachine, ScreenState, screenState } from '../state/screen-state';
import { atDate } from './format';
import { CallRequest } from './request';
import { PeopleFormsSection } from './people-forms';

// The state machine this screen declares. Empty, loading and failed are what
// the Spec gate requires of every screen the factory builds, disconnected is
// the fourth the subscription adds, and the machine is closed: a transition it
// does not declare is forbidden. Nothing writes it into the screenstatemachine
// record — the factory writes no spec version for itself, so the client's own
// build holds it.
export const peopleMachine: ScreenMachine = {
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

// People: every row of the declaration — who holds which duty, what obligation
// a row holds outside the twelve, the credentials each person or organisation
// lent with the account kind beside each, the spend ceiling and the rates
// authored on those credentials, and whether the row acts anywhere at all.
//
// Declared, not enforced: the declaration is what routes work, and it is the
// seam authentication attaches to later. The one field the factory does
// enforce is the ceiling, which holds work when reached.
//
// The screen holds the one path every call takes, and ./people-forms.ts holds
// the ten forms: a section emits the call it wants made and performs none
// itself, so the rule that an action re-reads the address first and is refused
// while the subscription is down is written once.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.
@Component({
  selector: 'factory-people-screen',
  imports: [RouterLink, PeopleFormsSection],
  templateUrl: './people.html',
  styleUrl: './people.css',
})
export class PeopleScreen implements OnDestroy {
  private readonly api = inject(ApiClient);
  private readonly streams = inject(StreamReader);

  private readonly stream = this.streams.subscribe('people', '-');
  private readonly reading = signal(true);
  private readonly failure = signal('');
  private readonly refusal = signal('');
  private readonly view = signal<People | null>(null);

  protected readonly machine = peopleMachine;
  protected readonly message = this.failure.asReadonly();
  protected readonly refused = this.refusal.asReadonly();
  protected readonly declaration = this.view.asReadonly();
  protected readonly rows = computed(() => this.view()?.Rows ?? []);
  protected readonly disconnected = computed(() => this.stream.state() === 'disconnected');
  protected readonly credentials = computed(() =>
    this.rows().flatMap((row) => (row.Credentials ?? []).map((each) => each.Name)),
  );
  protected readonly state = computed<ScreenState>(() =>
    screenState(this.reading(), this.failure() !== '', this.disconnected(), this.rows().length === 0),
  );

  constructor() {
    effect(() => {
      // Read so that every change on this address, and every reconnection,
      // re-reads it whole rather than resuming from what the screen held.
      this.stream.changed();
      void this.read();
    });
  }

  ngOnDestroy(): void {
    this.stream.close();
  }

  protected onDate(value: string): string {
    return atDate(value);
  }

  protected async read(): Promise<void> {
    this.reading.set(true);
    const result = await this.api.get<People>('/api/people');
    this.reading.set(false);
    if (result.outcome === 'value') {
      this.failure.set('');
      this.view.set(result.value);
      return;
    }
    if (result.outcome === 'absent') {
      // Nothing is at this address, which is the screen's empty state.
      this.failure.set('');
      this.view.set(null);
      return;
    }
    if (result.outcome === 'failed') {
      this.failure.set(result.message);
    }
  }

  // The one path every call from this screen takes: refused while the
  // subscription is down, re-reading the declaration before it sends, and
  // re-reading it after. A refusal the factory wrote — a mapping a legal hold
  // reaches, among them — is shown as the message the factory sent.
  protected requested(request: CallRequest): void {
    void this.send(request);
  }

  private async send(request: CallRequest): Promise<void> {
    this.refusal.set('');
    if (this.disconnected()) {
      this.refusal.set('the subscription is down, so nothing was sent');
      return;
    }
    const fresh = await this.api.get<People>('/api/people');
    if (fresh.outcome === 'failed') {
      this.refusal.set(
        `the declaration could not be re-read, so nothing was sent: ${fresh.message}`,
      );
      return;
    }
    if (fresh.outcome === 'absent') {
      this.refusal.set('the factory holds no declaration, so nothing was sent');
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
  }
}

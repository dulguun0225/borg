import { Component, OnDestroy, computed, effect, inject, signal } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { RouterLink } from '@angular/router';
import { ApiClient } from '../api/client';
import { StreamReader } from '../api/stream';
import { Home, Work } from '../api/types';
import { ScreenMachine, ScreenState, screenState } from '../state/screen-state';
import { atInstant, spanOf } from './format';

// The state machine the Work screen declares, and the one every address under
// it declares too. Empty, loading and failed are what the Spec gate requires
// of every screen the factory builds, disconnected is the fourth the
// subscription adds, and the machine is closed: a transition it does not
// declare is forbidden. Nothing writes it into the screenstatemachine record
// — the factory writes no spec version for itself, so the client's own build
// holds it, and each address's spec checks it for the three shapes the Spec
// gate rejects.
export const workMachine: ScreenMachine = {
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

// The home view: Work filtered to what waits on a human, which is the daily
// job. Two reads answer it — the badge with the last check rows, the
// readiness reading and the digest, and the board narrowed to what is waiting
// — and one subscription on the home address keeps both true.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// and
// ../../../../../end-goal/how-the-factory-works/11-screens/02-three-properties-every-screen-needs.md.
@Component({
  selector: 'factory-work-screen',
  imports: [NgTemplateOutlet, RouterLink],
  templateUrl: './work.html',
  styleUrl: './work.css',
})
export class WorkScreen implements OnDestroy {
  private readonly api = inject(ApiClient);
  private readonly streams = inject(StreamReader);

  private readonly stream = this.streams.subscribe('home', '-');
  private readonly reading = signal(true);
  private readonly failure = signal('');
  private readonly home = signal<Home | null>(null);
  private readonly waiting = signal<Work | null>(null);

  protected readonly machine = workMachine;
  protected readonly message = this.failure.asReadonly();
  protected readonly badge = computed(() => this.home()?.Badge ?? null);
  protected readonly digest = computed(() => this.home()?.Digest ?? null);
  protected readonly readiness = computed(() => this.home()?.Readiness ?? []);
  protected readonly rows = computed(() => this.waiting()?.Rows ?? []);

  // Only the rows whose component owes a further pass. A pass that merely ran
  // late is a named row here and reaches neither the badge nor the filter.
  protected readonly lastChecks = computed(() =>
    (this.home()?.LastChecks ?? []).filter((each) => each.FurtherPassOwed),
  );

  protected readonly state = computed<ScreenState>(() =>
    screenState(
      this.reading(),
      this.failure() !== '',
      this.stream.state() === 'disconnected',
      this.rows().length === 0 && this.lastChecks().length === 0 && this.readiness().length === 0,
    ),
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

  protected at(value: string): string {
    return atInstant(value);
  }

  protected span(seconds: number): string {
    return spanOf(seconds);
  }

  protected async read(): Promise<void> {
    this.reading.set(true);
    const home = await this.api.get<Home>('/api/home');
    const waiting = await this.api.get<Work>('/api/work?waiting_on_a_human=true');
    this.reading.set(false);
    if (home.outcome === 'failed') {
      this.failure.set(home.message);
      return;
    }
    if (waiting.outcome === 'failed') {
      this.failure.set(waiting.message);
      return;
    }
    if (home.outcome === 'absent' || waiting.outcome === 'absent') {
      // Nothing is at this address, which is the screen's empty state.
      this.failure.set('');
      this.home.set(null);
      this.waiting.set(null);
      return;
    }
    if (home.outcome !== 'value' || waiting.outcome !== 'value') {
      // The version refusal, which the shell renders as a required reload.
      // The screen moves nowhere.
      return;
    }
    this.failure.set('');
    this.home.set(home.value);
    this.waiting.set(waiting.value);
  }
}

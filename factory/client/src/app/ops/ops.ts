import { Component, OnDestroy, computed, effect, inject, signal } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { RouterLink } from '@angular/router';
import { ApiClient } from '../api/client';
import { StreamReader } from '../api/stream';
import { Ops, Service, ServiceSummary } from '../api/types-ops';
import { ScreenMachine, ScreenState, screenState } from '../state/screen-state';
import { atInstant } from './format';

// The state machine this screen declares, and the one the address under it
// declares too. Empty, loading and failed are what the Spec gate requires of
// every screen the factory builds, disconnected is the fourth the
// subscription adds, and the machine is closed: a transition it does not
// declare is forbidden. Nothing writes it into the screenstatemachine
// record — the factory writes no spec version for itself, so the client's own
// build holds it.
export const opsMachine: ScreenMachine = {
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

// One row of the board: the summary the list read answered with, and the
// service's own view on that environment, which is where every reading but
// health and the current release lives.
export interface Row {
  summary: ServiceSummary;
  detail: Service | null;
}

// Ops: what is running rather than what needs a human. Every service on every
// environment, with the release running per target and when each target's
// deploy completed, the drift detector's mismatch over that record where it
// disagrees, health, the open incidents, the rollouts in progress and the
// control each is measured against, and every last check record — all of
// them, and not only the ones past their interval.
//
// The board read answers only what routes to a service's own address, which
// ../../../../screens/viewops.go states of ServiceSummary, so this screen
// reads each service's view beside it: one read of /api/ops and one per row.
// One subscription still covers the address, and a change on it re-reads the
// whole of that.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.
@Component({
  selector: 'factory-ops-screen',
  imports: [NgTemplateOutlet, RouterLink],
  templateUrl: './ops.html',
  styleUrl: './ops.css',
})
export class OpsScreen implements OnDestroy {
  private readonly api = inject(ApiClient);
  private readonly streams = inject(StreamReader);

  private readonly stream = this.streams.subscribe('ops', '-');
  private readonly reading = signal(true);
  private readonly failure = signal('');
  private readonly board = signal<Row[]>([]);

  protected readonly machine = opsMachine;
  protected readonly message = this.failure.asReadonly();
  protected readonly rows = this.board.asReadonly();
  protected readonly state = computed<ScreenState>(() =>
    screenState(
      this.reading(),
      this.failure() !== '',
      this.stream.state() === 'disconnected',
      this.rows().length === 0,
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

  protected async read(): Promise<void> {
    this.reading.set(true);
    const result = await this.api.get<Ops>('/api/ops');
    if (result.outcome === 'value' || result.outcome === 'absent') {
      const summaries = result.outcome === 'value' ? (result.value.Services ?? []) : [];
      const rows: Row[] = [];
      for (const summary of summaries) {
        rows.push({ summary, detail: await this.detailOf(summary) });
      }
      this.reading.set(false);
      this.failure.set('');
      this.board.set(rows);
      return;
    }
    this.reading.set(false);
    if (result.outcome === 'failed') {
      this.failure.set(result.message);
    }
  }

  // A service whose own view could not be read leaves the row's readings
  // withheld rather than the board failed: the row is on the board because
  // the list read answered with it, and a link to its address stands.
  private async detailOf(summary: ServiceSummary): Promise<Service | null> {
    const address = `/api/service/${summary.ServiceID}/on/${summary.EnvironmentID}`;
    const result = await this.api.get<Service>(address);
    return result.outcome === 'value' ? result.value : null;
  }
}

import { Component, computed, effect, inject, input, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ApiClient } from '../api/client';
import { AddressStream, StreamReader } from '../api/stream';
import { Constraint } from '../api/types';
import { ScreenState, screenState } from '../state/screen-state';
import { atDate, atInstant } from './format';
import { factoryMachine } from './factory';

// One constraint's own address: the fourth place a human can be, reached
// from a dead link dispatch wrote — "no fleet entry covers this role" and
// "no role prompt is in force" both point at the fleet, and one that names a
// constraint requiring seam 5 points here — and from the row it is listed at
// in ../../../../../end-goal/how-the-factory-works/11-screens's own list,
// ./factory-places.html. It reuses factoryMachine: the same five states, the
// same transitions, declared once on the screen the address without an id
// belongs to.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.
@Component({
  selector: 'factory-constraint-screen',
  imports: [RouterLink],
  templateUrl: './factory-constraint.html',
  styleUrl: './factory.css',
})
export class FactoryConstraintScreen {
  private readonly api = inject(ApiClient);
  private readonly streams = inject(StreamReader);

  readonly id = input.required<string>();

  // The address this screen is on carries an id, so its one subscription is
  // held in a signal and replaced when the id changes.
  private readonly stream = signal<AddressStream | null>(null);
  private readonly reading = signal(true);
  private readonly failure = signal('');
  private readonly view = signal<Constraint | null>(null);

  protected readonly machine = factoryMachine;
  protected readonly message = this.failure.asReadonly();
  protected readonly constraint = this.view.asReadonly();
  protected readonly state = computed<ScreenState>(() =>
    screenState(
      this.reading(),
      this.failure() !== '',
      this.stream()?.state() === 'disconnected',
      this.view() === null,
    ),
  );

  constructor() {
    effect((onCleanup) => {
      const open = this.streams.subscribe('constraint', this.id());
      this.stream.set(open);
      onCleanup(() => {
        open.close();
      });
    });
    effect(() => {
      this.stream()?.changed();
      void this.read();
    });
  }

  protected at(value: string): string {
    return atInstant(value);
  }

  protected onDate(value: string): string {
    return atDate(value);
  }

  protected async read(): Promise<void> {
    this.reading.set(true);
    const result = await this.api.get<Constraint>(`/api/constraint/${this.id()}`);
    this.reading.set(false);
    if (result.outcome === 'value') {
      this.failure.set('');
      this.view.set(result.value);
      return;
    }
    if (result.outcome === 'absent') {
      // There is no constraint at this address, which is the screen's empty
      // state.
      this.failure.set('');
      this.view.set(null);
      return;
    }
    if (result.outcome === 'failed') {
      this.failure.set(result.message);
    }
  }
}

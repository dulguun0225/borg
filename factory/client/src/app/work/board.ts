import { Component, OnDestroy, computed, effect, inject, signal } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { RouterLink } from '@angular/router';
import { ApiClient } from '../api/client';
import { StreamReader } from '../api/stream';
import { Work } from '../api/types';
import { ScreenState, screenState } from '../state/screen-state';
import { atInstant } from './format';
import { workMachine } from './work';

// The board: every item on it and where each stands, which is the question
// "where is it stuck". A stop the factory caused is a row here and not an
// absence, with a link to where at Factory the record that lifts it is
// written.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.
@Component({
  selector: 'factory-board-screen',
  imports: [NgTemplateOutlet, RouterLink],
  templateUrl: './board.html',
  styleUrl: './work.css',
})
export class BoardScreen implements OnDestroy {
  private readonly api = inject(ApiClient);
  private readonly streams = inject(StreamReader);

  private readonly stream = this.streams.subscribe('work', '-');
  private readonly reading = signal(true);
  private readonly failure = signal('');
  private readonly view = signal<Work | null>(null);

  protected readonly machine = workMachine;
  protected readonly message = this.failure.asReadonly();
  protected readonly rows = computed(() => this.view()?.Rows ?? []);
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
    const result = await this.api.get<Work>('/api/work');
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
}

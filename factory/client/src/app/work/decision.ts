import { Component, computed, effect, inject, input, signal } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { RouterLink } from '@angular/router';
import { FormField, form, requiredError, submit, validate } from '@angular/forms/signals';
import { ApiClient } from '../api/client';
import { AddressStream, StreamReader } from '../api/stream';
import { Decision } from '../api/types';
import { ScreenState, screenState } from '../state/screen-state';
import { atInstant } from './format';
import { workMachine } from './work';

// The three verdicts this form writes. Refer is the fourth and is its own
// action, because it re-fires the row rather than closing it with a verdict a
// human chose between these.
const NEEDS_REASON = ['reject', 'hold'];

// One gate's own address: the open event, the vector, the versions in force at
// the firing, who it routes to, its acknowledgements, its close or its
// abandonment, and every delivery attempted for it — with the actions a human
// takes on it.
//
// Every action re-reads the open event first and sends nothing where a close
// or an abandonment has reached it since the row was drawn, which is where the
// log's refusal of a second close is met. Every verdict carries the instant
// the row was opened here, which is the one field no caller has ever filled.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md
// and
// ../../../../../end-goal/how-the-factory-works/03-gates/03-actions-at-each-gate.md.
@Component({
  selector: 'factory-decision-screen',
  imports: [NgTemplateOutlet, RouterLink, FormField],
  templateUrl: './decision.html',
  styleUrl: './work.css',
})
export class DecisionScreen {
  private readonly api = inject(ApiClient);
  private readonly streams = inject(StreamReader);

  readonly id = input.required<string>();

  // The instant the row was opened in Work, in UTC, carried on every verdict
  // written from here. It is taken once, when the screen is created, and not
  // when the button is pressed: what the close event records is when the
  // actor started reading, which is what makes the time between it and the
  // close a reading of the decision and not of the click.
  private readonly openedInWorkAt = new Date().toISOString();

  private readonly stream = signal<AddressStream | null>(null);
  private readonly reading = signal(true);
  private readonly failure = signal('');
  private readonly refusal = signal('');
  private readonly view = signal<Decision | null>(null);

  protected readonly machine = workMachine;
  protected readonly message = this.failure.asReadonly();
  protected readonly refused = this.refusal.asReadonly();
  protected readonly decision = this.view.asReadonly();
  protected readonly pending = computed(() => {
    const row = this.view();
    return row !== null && row.Closed === null && row.Abandoned === null;
  });

  protected readonly verdict = signal({ Verdict: 'approve', Reason: '' });
  protected readonly verdictForm = form(this.verdict, (path) => {
    validate(path.Reason, (field) => {
      if (!NEEDS_REASON.includes(field.valueOf(path.Verdict))) {
        return null;
      }
      if (field.value().trim() !== '') {
        return null;
      }
      return requiredError({ message: 'a reject and a hold each carry a reason' });
    });
  });

  protected readonly referral = signal({ Reason: '' });
  protected readonly referralForm = form(this.referral, (path) => {
    validate(path.Reason, (field) =>
      field.value().trim() === ''
        ? requiredError({ message: 'a referral carries the reason it was referred' })
        : null,
    );
  });

  protected readonly edit = signal({ VersionText: '' });
  protected readonly editForm = form(this.edit, (path) => {
    validate(path.VersionText, (field) =>
      field.value().trim() === ''
        ? requiredError({ message: 'an edit in place carries the version it writes' })
        : null,
    );
  });

  protected readonly takeOver = signal({ Stage: '' });
  protected readonly takeOverForm = form(this.takeOver, (path) => {
    validate(path.Stage, (field) =>
      field.value().trim() === ''
        ? requiredError({ message: 'a take-over names the stage the item returns to' })
        : null,
    );
  });

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
      const open = this.streams.subscribe('decision', this.id());
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

  protected vector(): { factor: string; value: string }[] {
    return Object.entries(this.view()?.Vector ?? {}).map(([factor, value]) => ({ factor, value }));
  }

  protected reasonErrors(): string[] {
    return this.verdictForm.Reason().errors().map((each) => each.message ?? 'invalid');
  }

  protected async read(): Promise<void> {
    this.reading.set(true);
    const result = await this.api.get<Decision>(`/api/decision/${this.id()}`);
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

  protected acknowledge(): void {
    void this.act('acknowledge', { OpenEventID: this.id() });
  }

  protected decide(event: Event): void {
    event.preventDefault();
    void submit(this.verdictForm, async () => {
      await this.act('decide', {
        OpenEventID: this.id(),
        Verdict: this.verdict().Verdict,
        Reason: this.verdict().Reason,
        OpenedInWorkAt: this.openedInWorkAt,
      });
      return null;
    });
  }

  protected refer(event: Event): void {
    event.preventDefault();
    void submit(this.referralForm, async () => {
      await this.act('refer', { OpenEventID: this.id(), Reason: this.referral().Reason });
      return null;
    });
  }

  protected editInPlace(event: Event): void {
    event.preventDefault();
    void submit(this.editForm, async () => {
      await this.act('editInPlace', {
        OpenEventID: this.id(),
        VersionText: this.edit().VersionText,
      });
      return null;
    });
  }

  protected takeItOver(event: Event): void {
    event.preventDefault();
    const row = this.view();
    if (row === null) {
      return;
    }
    void submit(this.takeOverForm, async () => {
      await this.act('takeOver', { ItemID: row.ItemID, Stage: this.takeOver().Stage });
      return null;
    });
  }

  // The one path every action takes: refused while the subscription is down,
  // and refused where the open event was closed or abandoned since the row
  // was drawn — the screen shows that state and sends nothing.
  private async act(name: string, args: object): Promise<void> {
    this.refusal.set('');
    if (this.stream()?.state() === 'disconnected') {
      this.refusal.set('the subscription is down, so nothing was sent');
      return;
    }
    const fresh = await this.api.get<Decision>(`/api/decision/${this.id()}`);
    if (fresh.outcome === 'failed') {
      this.refusal.set(`the row could not be re-read, so nothing was sent: ${fresh.message}`);
      return;
    }
    if (fresh.outcome === 'absent') {
      this.refusal.set('this row is no longer at this address, so nothing was sent');
      this.view.set(null);
      return;
    }
    if (fresh.outcome !== 'value') {
      return;
    }
    this.view.set(fresh.value);
    if (fresh.value.Closed !== null) {
      this.refusal.set('this row was closed since it was drawn, so nothing was sent');
      return;
    }
    if (fresh.value.Abandoned !== null) {
      this.refusal.set('this row was abandoned since it was drawn, so nothing was sent');
      return;
    }
    const result = await this.api.call(name, args);
    if (result.outcome === 'absent') {
      this.refusal.set('the factory holds no record at that address, so nothing changed');
      return;
    }
    if (result.outcome === 'failed') {
      this.refusal.set(result.message);
      return;
    }
    await this.read();
  }
}

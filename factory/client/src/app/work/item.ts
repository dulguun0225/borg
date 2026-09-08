import { Component, computed, effect, inject, input, signal } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { RouterLink } from '@angular/router';
import { FormField, form, requiredError, submit, validate } from '@angular/forms/signals';
import { ApiClient } from '../api/client';
import { AddressStream, StreamReader } from '../api/stream';
import { DecisionSummary, Item, ReportSummary } from '../api/types';
import { ScreenState, screenState } from '../state/screen-state';
import { atInstant } from './format';
import { workMachine } from './work';

// One entry of the timeline. Everything an item's records carry a time for is
// placed by that time and by nothing else: the order is the order the records
// say things happened, not an order this file assigns to a stage.
export interface Entry {
  kind: 'version' | 'decision' | 'deploy' | 'window' | 'release';
  at: string;
  label: string;
  decision: DecisionSummary | null;
}

// One item as one timeline, with each gate shown inline at the point where it
// fired: the vector while a human decides, the number beside the verdict once
// it is written, the acknowledgements and by whom, and at the Implementation
// gate the build's diff against master — the one place the product renders
// code.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.
@Component({
  selector: 'factory-item-screen',
  imports: [NgTemplateOutlet, RouterLink, FormField],
  templateUrl: './item.html',
  styleUrl: './work.css',
})
export class ItemScreen {
  private readonly api = inject(ApiClient);
  private readonly streams = inject(StreamReader);

  readonly id = input.required<string>();

  // The instant this screen was created, in UTC, carried on the approve-
  // through-hold call the way decision.ts carries it on every verdict: what
  // the close event records is when the actor started reading, not when the
  // button was pressed.
  private readonly openedInWorkAt = new Date().toISOString();

  private readonly reading = signal(true);
  private readonly failure = signal('');
  private readonly refusal = signal('');
  private readonly view = signal<Item | null>(null);
  // The address this screen is on carries an id, so its one subscription is
  // held in a signal and replaced when the id changes; a screen on a fixed
  // address holds its subscription in a plain field instead.
  private readonly stream = signal<AddressStream | null>(null);
  // Set for the span of the one path every action takes, so a second click
  // while one is already in flight has nothing to send.
  private readonly working = signal(false);

  protected readonly machine = workMachine;
  protected readonly message = this.failure.asReadonly();
  protected readonly refused = this.refusal.asReadonly();
  protected readonly busy = this.working.asReadonly();
  protected readonly item = this.view.asReadonly();

  protected readonly priority = signal({ Priority: 0 });
  protected readonly priorityForm = form(this.priority);

  protected readonly approval = signal({ Reason: '' });
  protected readonly approvalForm = form(this.approval, (path) => {
    validate(path.Reason, (field) =>
      field.value().trim() === ''
        ? requiredError({ message: 'approving through a hold carries the reason' })
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

  // The decision the build's diff is shown at. Empty where the item's
  // decisions name no Implementation gate, and the diff is then a row of its
  // own rather than withheld.
  protected readonly diffAt = computed(() => {
    const at = (this.view()?.Decisions ?? []).find(
      (each) => each.GateRow.toLowerCase() === 'implementation',
    );
    return at?.OpenEventID ?? '';
  });

  // The end-user reports grouped into this item's intent, rendered under the
  // intent's own entry — the statement this screen opens with — and nowhere
  // else. There is no address of their own: a browsable list of reports is
  // what the design refuses.
  protected readonly reports = computed<ReportSummary[]>(() => this.view()?.Reports ?? []);

  // True where this item's intent is itself still waiting for a human's
  // admission, which is the first of the two safeguards on the report store.
  // It is the intent's own field and not a reading over the reports: the two
  // admissions are separate, and a group whose reports are all admitted may
  // still be an intent nobody has admitted.
  protected readonly intentWaiting = computed(() => this.view()?.IntentAwaitsAdmission ?? false);

  protected readonly timeline = computed<Entry[]>(() => {
    const item = this.view();
    if (item === null) {
      return [];
    }
    const entries: Entry[] = [
      ...(item.Versions ?? []).map<Entry>((each) => ({
        kind: 'version',
        at: each.AuthoredAt,
        label: `${each.Kind} version ${each.ID}`,
        decision: null,
      })),
      ...(item.Decisions ?? []).map<Entry>((each) => ({
        kind: 'decision',
        at: each.OpenedAt,
        label: each.GateRow,
        decision: each,
      })),
      ...(item.Deploys ?? []).map<Entry>((each) => ({
        kind: 'deploy',
        at: each.CompletedAt,
        label: `${each.TargetID} on ${each.EnvironmentID}`,
        decision: null,
      })),
      ...(item.Windows ?? []).map<Entry>((each) => ({
        kind: 'window',
        at: '',
        label: each.Exit === '' ? `window ${each.ID} is open` : `window ${each.ID} ${each.Exit}`,
        decision: null,
      })),
    ];
    const release = item.Release;
    if (release !== null) {
      entries.push({
        kind: 'release',
        at: '',
        label: `${release.ServiceID} release ${release.Number}`,
        decision: null,
      });
    }
    // A record with no time of its own sorts after every record that has one:
    // it is where the timeline ends and not where it began. Two such records
    // compared against each other are neither before nor after, so they sort
    // by their label instead — a fixed, defined order rather than whatever
    // one comparison result the array happened to see first, which is what
    // "one before two, so two before one" would otherwise both answer true.
    return entries.sort((one, two) => {
      if (one.at === '' && two.at === '') {
        return one.label.localeCompare(two.label);
      }
      if (one.at === '') {
        return 1;
      }
      if (two.at === '') {
        return -1;
      }
      return one.at.localeCompare(two.at);
    });
  });

  constructor() {
    effect((onCleanup) => {
      const open = this.streams.subscribe('item', this.id());
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

  protected vectorOf(decision: DecisionSummary): { factor: string; value: string }[] {
    return Object.entries(decision.Vector ?? {}).map(([factor, value]) => ({ factor, value }));
  }

  protected async read(): Promise<void> {
    this.reading.set(true);
    const result = await this.api.get<Item>(`/api/item/${this.id()}`);
    this.reading.set(false);
    if (result.outcome === 'value') {
      this.failure.set('');
      this.view.set(result.value);
      this.priority.set({ Priority: 0 });
      return;
    }
    if (result.outcome === 'absent') {
      // There is no item at this address, which is the screen's empty state.
      this.failure.set('');
      this.view.set(null);
      return;
    }
    if (result.outcome === 'failed') {
      this.failure.set(result.message);
    }
  }

  protected setPriority(event: Event): void {
    event.preventDefault();
    void submit(this.priorityForm, async () => {
      await this.act('setPriority', { ItemID: this.id(), Priority: this.priority().Priority });
      return null;
    });
  }

  // The two admissions, each sent through the one call path every action on
  // this screen takes: one report at a time, and one action on the intent, the
  // group already being one intent.
  protected admitReport(reportID: string): void {
    void this.act('admitReport', { ReportID: reportID });
  }

  protected admitIntent(): void {
    void this.act('admitIntent', { IntentID: this.view()?.IntentID ?? '' });
  }

  protected endItem(): void {
    void this.act('endItem', { ItemID: this.id() });
  }

  protected approvalErrors(): string[] {
    return this.approvalForm.Reason().errors().map((each) => each.message ?? 'invalid');
  }

  protected approveThroughHold(event: Event): void {
    event.preventDefault();
    void submit(this.approvalForm, async () => {
      await this.act('approveThroughHold', {
        ItemID: this.id(),
        Reason: this.approval().Reason,
        OpenedInWorkAt: this.openedInWorkAt,
      });
      return null;
    });
  }

  // Every action re-reads the item first and refuses while the subscription is
  // down, because a screen holding what it can no longer be told about is not
  // a screen an action may be taken from. A second invocation while one is
  // already in flight is ignored: the template disables the control it came
  // from while busy() is true.
  private async act(name: string, args: object): Promise<void> {
    if (this.working()) {
      return;
    }
    this.working.set(true);
    try {
      this.refusal.set('');
      if (this.stream()?.state() === 'disconnected') {
        this.refusal.set('the subscription is down, so nothing was sent');
        return;
      }
      const fresh = await this.api.get<Item>(`/api/item/${this.id()}`);
      if (fresh.outcome === 'failed') {
        this.refusal.set(`the item could not be re-read, so nothing was sent: ${fresh.message}`);
        return;
      }
      if (fresh.outcome === 'absent') {
        this.refusal.set('this item is no longer at this address, so nothing was sent');
        this.view.set(null);
        return;
      }
      if (fresh.outcome !== 'value') {
        return;
      }
      this.view.set(fresh.value);
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
    } finally {
      this.working.set(false);
    }
  }
}

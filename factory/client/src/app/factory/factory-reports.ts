import { Component, computed, effect, input, output, signal } from '@angular/core';
import {
  FormField,
  ValidationError,
  form,
  requiredError,
  submit,
  validate,
} from '@angular/forms/signals';
import { Factory } from '../api/types-factory';
import { ErasureSpan } from '../api/types-reports';
import { CallRequest } from './request';

function needed(message: string) {
  return (value: string): ReturnType<typeof requiredError> | null =>
    value.trim() === '' ? requiredError({ message }) : null;
}

// The spans an owner types: half-open byte ranges of the report's text, as
// start-end pairs separated by spaces. Anything else is refused here rather
// than sent, an erasure naming a range the factory cannot read being one that
// destroys nothing.
export function spansTyped(value: string): ErasureSpan[] | null {
  const parts = value.trim().split(/\s+/).filter((part) => part !== '');
  if (parts.length === 0) {
    return null;
  }
  const spans: ErasureSpan[] = [];
  for (const part of parts) {
    const pair = /^(\d+)-(\d+)$/.exec(part);
    if (pair === null) {
      return null;
    }
    const start = Number(pair[1]);
    const end = Number(pair[2]);
    if (end <= start) {
      return null;
    }
    spans.push({ Start: start, End: end });
  }
  return spans;
}

// The report channel: what arrived and was never grouped, what the way in
// refused per service and over the whole channel, what the store could not
// read, each service serving a way in built under another release of the
// product, each service whose project has no notice for the way in to show —
// and the erasure an owner performs here as one action.
//
// The refused count and the unreadable-shape count are counters the report
// store keeps, where every other number on this screen is a query at read time:
// the record a query would count is the write the rate exists to refuse, so a
// lost counter is lost.
//
// It performs no call: it emits the call it wants made, and ./factory.ts holds
// the one path every call from this screen takes.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md,
// ../../../../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/01-constraints-and-the-design-system.md
// and
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.
@Component({
  selector: 'factory-reports',
  imports: [FormField],
  templateUrl: './factory-reports.html',
  styleUrl: './factory.css',
})
export class FactoryReportsSection {
  readonly view = input.required<Factory>();
  // Whether the screen's one path is already sending a call; the submitting
  // control here binds [disabled] to it so a second click while one is in
  // flight has nothing to send.
  readonly busy = input(false);
  readonly requested = output<CallRequest>();

  protected readonly channel = computed(() => this.view().ReportChannel);
  protected readonly services = computed(() => this.channel().Services ?? []);
  protected readonly onAnOldWayIn = computed(() => this.channel().OnAnOldWayIn ?? []);
  protected readonly underNoNotice = computed(() =>
    this.services().filter((one) => one.NoNoticeInForce),
  );

  protected readonly erasure = signal({ ReportID: '', Spans: '', Reason: '' });
  protected readonly erasureForm = form(this.erasure, (path) => {
    validate(path.ReportID, (field) =>
      needed('an erasure names the report whose words go')(field.value()),
    );
    validate(path.Reason, (field) =>
      needed('an erasure names its reason, which the record carries in place of the words')(
        field.value(),
      ),
    );
    validate(path.Spans, (field) => {
      if (spansTyped(field.value()) !== null) {
        return null;
      }
      return requiredError({
        message: 'the spans are start-end byte ranges separated by spaces, as in 12-20 44-51',
      });
    });
  });

  // Armed by a first click and sent by a second: the bytes are destroyed and
  // no second record puts them back, so one click states the intent and a
  // second, over the same typed spans, is what sends it.
  private readonly armed = signal(false);
  protected readonly confirming = this.armed.asReadonly();

  constructor() {
    // A confirm answers what it was armed on: editing the form after arming it
    // un-arms it, rather than letting a stale confirm destroy a range typed
    // after.
    effect(() => {
      this.erasure();
      this.armed.set(false);
    });
  }

  protected problems(errors: readonly ValidationError[]): string[] {
    return errors.map((each) => each.message ?? 'invalid');
  }

  protected performErasure(event: Event): void {
    event.preventDefault();
    void submit(this.erasureForm, () => {
      const spans = spansTyped(this.erasure().Spans);
      if (spans === null) {
        return Promise.resolve(null);
      }
      if (!this.armed()) {
        this.armed.set(true);
        return Promise.resolve(null);
      }
      this.requested.emit({
        name: 'performErasure',
        args: {
          ReportID: this.erasure().ReportID,
          Spans: spans,
          Reason: this.erasure().Reason,
        },
      });
      this.armed.set(false);
      return Promise.resolve(null);
    });
  }
}

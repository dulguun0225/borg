import { Component, computed, input, output, signal } from '@angular/core';
import {
  FormField,
  ValidationError,
  form,
  requiredError,
  submit,
  validate,
} from '@angular/forms/signals';
import {
  Factory,
  GATE_ROW_SUBJECT_KIND,
  LEGAL_HOLD_SUBJECT_KINDS,
  RECORD_ROW_VERDICTS,
  SAFEGUARD_SUBJECT_KINDS,
} from '../api/types-factory';
import { atInstant } from './format';
import { CallRequest } from './request';

// A reject carries a reason and a refer carries what the human could not
// judge; an approve's reason is a note nothing reads.
const NEEDS_REASON = ['reject', 'refer'];

function needed(message: string) {
  return (value: string): ReturnType<typeof requiredError> | null =>
    value.trim() === '' ? requiredError({ message }) : null;
}

// The rows that decide a record rather than an item, and gate policy: every
// parameter with its effective value and which read it came from, the
// safeguards, the one halt whose subject is the factory, and the legal holds.
//
// It performs no call: it emits the call it wants made, and ./factory.ts holds
// the one path every call from this screen takes.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.
@Component({
  selector: 'factory-gate-policy',
  imports: [FormField],
  templateUrl: './factory-policy.html',
  styleUrl: './factory.css',
})
export class FactoryPolicySection {
  readonly view = input.required<Factory>();
  readonly openedInWorkAt = input.required<string>();
  // Whether the screen's one path is already sending a call; every
  // submitting control here binds [disabled] to it so a second click while
  // one is in flight has nothing to send.
  readonly busy = input(false);
  readonly requested = output<CallRequest>();

  protected readonly verdicts = RECORD_ROW_VERDICTS;
  protected readonly safeguardSubjects = [...SAFEGUARD_SUBJECT_KINDS, GATE_ROW_SUBJECT_KIND];
  protected readonly holdSubjects = LEGAL_HOLD_SUBJECT_KINDS;
  protected readonly gateRowSubjectKind = GATE_ROW_SUBJECT_KIND;

  protected readonly rows = computed(() => this.view().RecordDecidingRows ?? []);
  protected readonly parameters = computed(() => this.view().Parameters ?? []);
  protected readonly safeguards = computed(() => this.view().Safeguards ?? []);
  protected readonly halts = computed(() => this.view().Halts ?? []);
  protected readonly legalHolds = computed(() => this.view().LegalHolds ?? []);
  protected readonly seam5Enforced = computed(() => this.view().Seam5Enforced);

  protected readonly decision = signal({ RecordID: '', Verdict: 'approve', Reason: '' });
  protected readonly decisionForm = form(this.decision, (path) => {
    validate(path.RecordID, (field) => needed('name the row being decided')(field.value()));
    validate(path.Reason, (field) => {
      if (!NEEDS_REASON.includes(field.valueOf(path.Verdict))) {
        return null;
      }
      return needed('a reject and a refer each carry a reason')(field.value());
    });
  });

  protected readonly parameter = signal({
    Parameter: '',
    Value: '',
    ServiceID: '',
    AreaID: '',
    GateRow: '',
    Stage: '',
    Quantity: '',
    ProjectName: '',
  });
  protected readonly parameterForm = form(this.parameter, (path) => {
    validate(path.Parameter, (field) => needed('name the parameter authored')(field.value()));
    validate(path.Value, (field) => needed('a parameter carries a value')(field.value()));
  });

  protected readonly safeguard = signal({
    Parameter: '',
    SubjectKind: 'service',
    SubjectName: '',
    ServiceName: '',
    Bound: '',
    RouteDuty: 0,
    RouteHuman: '',
  });
  protected readonly safeguardForm = form(this.safeguard, (path) => {
    validate(path.Parameter, (field) =>
      needed('a safeguard names the parameter it binds')(field.value()),
    );
    validate(path.SubjectName, (field) =>
      needed('a safeguard names the subject it is drawn on')(field.value()),
    );
    validate(path.ServiceName, (field) => {
      if (field.valueOf(path.SubjectKind) !== GATE_ROW_SUBJECT_KIND) {
        return null;
      }
      return needed('a gate-row safeguard names the service the row fires for')(field.value());
    });
    validate(path.Bound, (field) => needed('a safeguard carries its bound')(field.value()));
  });

  protected readonly halt = signal({ Reason: '' });
  protected readonly haltForm = form(this.halt, (path) => {
    validate(path.Reason, (field) => needed('a halt carries the reason it was set')(field.value()));
  });

  protected readonly legalHold = signal({ SubjectKind: 'service', SubjectName: '', Reason: '' });
  protected readonly legalHoldForm = form(this.legalHold, (path) => {
    validate(path.Reason, (field) =>
      needed('a legal hold carries the reason it was set')(field.value()),
    );
  });

  protected at(value: string): string {
    return atInstant(value);
  }

  protected problems(errors: readonly ValidationError[]): string[] {
    return errors.map((each) => each.message ?? 'invalid');
  }

  // One of the four rows outside every item, decided here. The row's own kind
  // travels with it: the view names it, so no vocabulary is composed here.
  protected decideRow(event: Event): void {
    event.preventDefault();
    void submit(this.decisionForm, () => {
      const chosen = this.decision();
      const row = this.rows().find((each) => each.RecordID === chosen.RecordID);
      if (row === undefined) {
        return Promise.resolve(null);
      }
      this.requested.emit({
        name: 'decideRecordRow',
        args: {
          RowKind: row.Kind,
          RecordID: row.RecordID,
          Verdict: chosen.Verdict,
          Reason: chosen.Reason,
          OpenedInWorkAt: this.openedInWorkAt(),
        },
      });
      return Promise.resolve(null);
    });
  }

  protected authorParameter(event: Event): void {
    event.preventDefault();
    void submit(this.parameterForm, () => {
      this.requested.emit({ name: 'authorParameter', args: this.parameter() });
      return Promise.resolve(null);
    });
  }

  protected placeSafeguard(event: Event): void {
    event.preventDefault();
    void submit(this.safeguardForm, () => {
      this.requested.emit({ name: 'placeSafeguard', args: this.safeguard() });
      return Promise.resolve(null);
    });
  }

  protected withdrawSafeguard(id: string): void {
    this.requested.emit({ name: 'withdrawSafeguard', args: { SafeguardID: id } });
  }

  protected setHalt(event: Event): void {
    event.preventDefault();
    void submit(this.haltForm, () => {
      this.requested.emit({ name: 'setHalt', args: this.halt() });
      return Promise.resolve(null);
    });
  }

  protected withdrawHalt(id: string): void {
    this.requested.emit({ name: 'withdrawHalt', args: { HaltID: id } });
  }

  protected setLegalHold(event: Event): void {
    event.preventDefault();
    void submit(this.legalHoldForm, () => {
      this.requested.emit({ name: 'setLegalHold', args: this.legalHold() });
      return Promise.resolve(null);
    });
  }

  protected withdrawLegalHold(id: string): void {
    this.requested.emit({ name: 'withdrawLegalHold', args: { LegalHoldID: id } });
  }

  // Seam 5 enforcement is one-way: off at install, turned on once here, and
  // never off again, so the control offers only turning it on.
  protected setSeam5Enforced(): void {
    this.requested.emit({ name: 'setSeam5Enforced', args: { Enforced: true } });
  }
}

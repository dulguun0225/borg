import { Component, input, output, signal } from '@angular/core';
import {
  FormField,
  ValidationError,
  form,
  requiredError,
  submit,
  validate,
} from '@angular/forms/signals';
import {
  ACCOUNT_KINDS,
  DUTIES,
  OBLIGATIONS,
  PERIOD_UNITS,
} from '../api/types-people';
import { viewerZone } from './format';
import { CallRequest } from './request';

function needed(message: string) {
  return (value: string): ReturnType<typeof requiredError> | null =>
    value.trim() === '' ? requiredError({ message }) : null;
}

function positive(message: string) {
  return (value: number): ReturnType<typeof requiredError> | null =>
    value > 0 ? null : requiredError({ message });
}

// What a human writes into the declaration: a duty declared or withdrawn, an
// obligation declared or withdrawn, a credential lent or taken back, the
// ceiling and the rates authored on a lent credential, and the mapping from a
// per-person key to a name — written and erased on its own, the mapping being
// the one part of the declaration kept outside the chain so an erasure can
// delete it alone.
//
// The start date a ceiling's period runs from carries the viewer's IANA zone
// beside it, because every reader of that value computes in that zone and no
// other: a period ends at that zone's midnight.
//
// It performs no call: it emits the call it wants made, and ./people.ts holds
// the one path every call from this screen takes.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// and
// ../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md.
@Component({
  selector: 'factory-people-forms',
  imports: [FormField],
  templateUrl: './people-forms.html',
  styleUrl: './people.css',
})
export class PeopleFormsSection {
  // The credentials the declaration names, which is what a ceiling and a rate
  // are authored on. Empty on an install where nothing is lent yet, and the
  // form then takes the name as text.
  readonly credentials = input.required<readonly string[]>();
  // Whether the screen's one path is already sending a call; every
  // submitting control here binds [disabled] to it so a second click while
  // one is in flight has nothing to send.
  readonly busy = input(false);
  readonly requested = output<CallRequest>();

  protected readonly duties = DUTIES;
  protected readonly obligations = OBLIGATIONS;
  protected readonly accountKinds = ACCOUNT_KINDS;
  protected readonly periodUnits = PERIOD_UNITS;
  protected readonly zone = viewerZone();

  // The duty is held as text because a select's value is text, and it is one
  // of the twelve numbers when it reaches the call.
  protected readonly duty = signal({ HumanKey: '', Duty: '1' });
  protected readonly dutyForm = form(this.duty, (path) => {
    validate(path.HumanKey, (field) => needed('name the per-person key')(field.value()));
  });

  protected readonly obligation = signal({ HumanKey: '', Obligation: 'hosting' });
  protected readonly obligationForm = form(this.obligation, (path) => {
    validate(path.HumanKey, (field) => needed('name the per-person key')(field.value()));
  });

  protected readonly lending = signal({ HumanKey: '', Credential: '', Kind: 'person' });
  protected readonly lendingForm = form(this.lending, (path) => {
    validate(path.HumanKey, (field) => needed('name the per-person key')(field.value()));
    validate(path.Credential, (field) => needed('a lent credential is named')(field.value()));
  });

  protected readonly ceiling = signal({
    Credential: '',
    Amount: 0,
    Currency: '',
    PeriodUnit: 'month',
    Length: 1,
    StartDate: '',
  });
  protected readonly ceilingForm = form(this.ceiling, (path) => {
    validate(path.Credential, (field) =>
      needed('a ceiling is authored on a lent credential')(field.value()),
    );
    validate(path.Amount, (field) => positive('a spend ceiling is above zero')(field.value()));
    validate(path.Currency, (field) =>
      needed('the currency the rates are authored in is named')(field.value()),
    );
    validate(path.Length, (field) => positive('a period is a length above zero')(field.value()));
    validate(path.StartDate, (field) =>
      needed('a period starts from a date')(field.value()),
    );
  });

  protected readonly rate = signal({
    Credential: '',
    Kind: '',
    ModelVersion: '',
    Effort: '',
    Amount: 0,
    Currency: '',
  });
  protected readonly rateForm = form(this.rate, (path) => {
    validate(path.Credential, (field) =>
      needed('a rate is authored on a lent credential')(field.value()),
    );
    validate(path.Kind, (field) => needed('a rate names the kind of unit it prices')(field.value()));
    validate(path.ModelVersion, (field) =>
      needed('a rate names the model version it prices')(field.value()),
    );
    validate(path.Currency, (field) => needed('a rate names its currency')(field.value()));
  });

  protected readonly mapping = signal({ HumanKey: '', Name: '' });
  protected readonly mappingForm = form(this.mapping, (path) => {
    validate(path.HumanKey, (field) => needed('name the per-person key')(field.value()));
    validate(path.Name, (field) => needed('a mapping carries the name it resolves to')(field.value()));
  });

  protected readonly erasure = signal({ HumanKey: '' });
  protected readonly erasureForm = form(this.erasure, (path) => {
    validate(path.HumanKey, (field) => needed('name the per-person key erased')(field.value()));
  });

  protected problems(errors: readonly ValidationError[]): string[] {
    return errors.map((each) => each.message ?? 'invalid');
  }

  private dutyArgs(): { HumanKey: string; Duty: number } {
    const held = this.duty();
    return { HumanKey: held.HumanKey, Duty: Number(held.Duty) };
  }

  protected declareDuty(event: Event): void {
    event.preventDefault();
    void submit(this.dutyForm, () => {
      this.requested.emit({ name: 'declareDuty', args: this.dutyArgs() });
      return Promise.resolve(null);
    });
  }

  // A withdrawal is the same two fields as its declaration, so it is a second
  // button on the one form rather than a second form with a second copy of
  // them. The button is not a submit: a form element fires one submit event,
  // and the Signal Forms submit below is a call and not that event.
  protected withdrawDuty(): void {
    void submit(this.dutyForm, () => {
      this.requested.emit({ name: 'withdrawDuty', args: this.dutyArgs() });
      return Promise.resolve(null);
    });
  }

  protected declareObligation(event: Event): void {
    event.preventDefault();
    void submit(this.obligationForm, () => {
      this.requested.emit({ name: 'declareObligation', args: this.obligation() });
      return Promise.resolve(null);
    });
  }

  protected withdrawObligation(): void {
    void submit(this.obligationForm, () => {
      this.requested.emit({ name: 'withdrawObligation', args: this.obligation() });
      return Promise.resolve(null);
    });
  }

  protected lendCredential(event: Event): void {
    event.preventDefault();
    void submit(this.lendingForm, () => {
      this.requested.emit({ name: 'lendCredential', args: this.lending() });
      return Promise.resolve(null);
    });
  }

  protected takeBackCredential(): void {
    void submit(this.lendingForm, () => {
      const lent = this.lending();
      this.requested.emit({
        name: 'takeBackCredential',
        args: { HumanKey: lent.HumanKey, Credential: lent.Credential },
      });
      return Promise.resolve(null);
    });
  }

  protected authorCeiling(event: Event): void {
    event.preventDefault();
    void submit(this.ceilingForm, () => {
      // The zone travels with the start date and is the viewer's own: every
      // reader of that date computes in it and in no other.
      this.requested.emit({
        name: 'authorCeiling',
        args: { ...this.ceiling(), Zone: this.zone },
      });
      return Promise.resolve(null);
    });
  }

  protected authorRate(event: Event): void {
    event.preventDefault();
    void submit(this.rateForm, () => {
      this.requested.emit({ name: 'authorRate', args: this.rate() });
      return Promise.resolve(null);
    });
  }

  protected writeMapping(event: Event): void {
    event.preventDefault();
    void submit(this.mappingForm, () => {
      this.requested.emit({ name: 'writeMapping', args: this.mapping() });
      return Promise.resolve(null);
    });
  }

  protected deleteMapping(event: Event): void {
    event.preventDefault();
    void submit(this.erasureForm, () => {
      this.requested.emit({ name: 'deleteMapping', args: this.erasure() });
      return Promise.resolve(null);
    });
  }
}

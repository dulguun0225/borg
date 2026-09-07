import { Component, computed, effect, input, output, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import {
  FormField,
  ValidationError,
  form,
  requiredError,
  submit,
  validate,
} from '@angular/forms/signals';
import { CONSTRAINT_REACHES, Factory } from '../api/types-factory';
import { atDate, atInstant, viewerZone } from './format';
import { CallRequest } from './request';

// The reaches a constraint supplied here may name, widest first, which is the
// order the design lists them in and the order the rows are read in.
const REACH_ORDER = CONSTRAINT_REACHES;

function needed(message: string) {
  return (value: string): ReturnType<typeof requiredError> | null =>
    value.trim() === '' ? requiredError({ message }) : null;
}

// The permanent places and the permanent constraints: the environments, the
// projects an owner writes and the areas declared inside them, and the
// constraints in force with the three widest reaches, listed by reach.
//
// A constraint whose reach is one intent is read and withdrawn at Work, on its
// intent, and is not here.
//
// Every calendar value a form here takes carries the viewer's IANA zone beside
// it, because every reader of such a value computes in that zone and no other.
//
// It performs no call: it emits the call it wants made, and ./factory.ts holds
// the one path every call from this screen takes.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// and
// ../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md.
@Component({
  selector: 'factory-places',
  imports: [FormField, RouterLink],
  templateUrl: './factory-places.html',
  styleUrl: './factory.css',
})
export class FactoryPlacesSection {
  readonly view = input.required<Factory>();
  // Whether the screen's one path is already sending a call; every
  // submitting control here binds [disabled] to it so a second click while
  // one is in flight has nothing to send.
  readonly busy = input(false);
  readonly requested = output<CallRequest>();

  protected readonly reaches = REACH_ORDER;
  protected readonly zone = viewerZone();
  protected readonly environments = computed(() => this.view().Environments ?? []);
  protected readonly projects = computed(() => this.view().Projects ?? []);
  protected readonly areas = computed(() => this.view().Areas ?? []);

  // Listed by reach, widest first: a constraint reaching the whole factory is
  // read before one reaching a project, and one reaching a project before one
  // reaching an area. A reach outside the three sorts last.
  protected readonly constraints = computed(() =>
    [...(this.view().Constraints ?? [])].sort((one, two) => rank(one.Reach) - rank(two.Reach)),
  );

  protected readonly project = signal({ Name: '' });
  protected readonly projectForm = form(this.project, (path) => {
    validate(path.Name, (field) => needed('a project carries a name')(field.value()));
  });

  protected readonly area = signal({ Name: '', InsideAreaID: '', ProjectName: '' });
  protected readonly areaForm = form(this.area, (path) => {
    validate(path.Name, (field) => needed('an area carries a name')(field.value()));
  });

  protected readonly constraint = signal({
    Statement: '',
    ReachKind: 'factory',
    ReachName: '',
    BindsFrom: '',
    ReviewDate: '',
    RequiresSeam5Enforced: false,
  });
  protected readonly constraintForm = form(this.constraint, (path) => {
    validate(path.Statement, (field) => needed('a constraint carries a statement')(field.value()));
  });

  protected readonly retirement = signal({ ServiceID: '', EnvironmentName: '' });
  protected readonly retirementForm = form(this.retirement, (path) => {
    validate(path.ServiceID, (field) => needed('name the service retired')(field.value()));
  });
  // Armed by a first click and sent by a second: retiring a service is not
  // undone by a further write here, so one click states the intent and a
  // second, on the same typed name, is what sends it.
  private readonly retirementArmed = signal(false);
  protected readonly retirementConfirming = this.retirementArmed.asReadonly();

  protected readonly ending = signal({ ProjectName: '' });
  protected readonly endingForm = form(this.ending, (path) => {
    validate(path.ProjectName, (field) => needed('name the project ended')(field.value()));
  });
  private readonly endingArmed = signal(false);
  protected readonly endingConfirming = this.endingArmed.asReadonly();

  constructor() {
    // A confirm answers the name it was armed on: editing either field after
    // arming it un-arms it, rather than letting a stale confirm reach a name
    // typed after.
    effect(() => {
      this.retirement();
      this.retirementArmed.set(false);
    });
    effect(() => {
      this.ending();
      this.endingArmed.set(false);
    });
  }

  protected at(value: string): string {
    return atInstant(value);
  }

  protected onDate(value: string): string {
    return atDate(value);
  }

  protected problems(errors: readonly ValidationError[]): string[] {
    return errors.map((each) => each.message ?? 'invalid');
  }

  protected createProject(event: Event): void {
    event.preventDefault();
    void submit(this.projectForm, () => {
      this.requested.emit({ name: 'createProject', args: this.project() });
      return Promise.resolve(null);
    });
  }

  protected declareArea(event: Event): void {
    event.preventDefault();
    void submit(this.areaForm, () => {
      this.requested.emit({ name: 'declareArea', args: this.area() });
      return Promise.resolve(null);
    });
  }

  protected supplyConstraint(event: Event): void {
    event.preventDefault();
    void submit(this.constraintForm, () => {
      // The zone travels with the two calendar values and is the viewer's
      // own: every reader of either computes in it and in no other.
      this.requested.emit({
        name: 'supplyConstraint',
        args: { ...this.constraint(), Zone: this.zone },
      });
      return Promise.resolve(null);
    });
  }

  protected withdrawConstraint(id: string): void {
    this.requested.emit({ name: 'withdrawConstraint', args: { ConstraintID: id } });
  }

  // The first click on this form arms it and sends nothing; the second, over
  // the same typed name, sends it. Changing the name un-arms it: a confirm
  // answers the name it was armed on and not a different one typed after.
  protected confirmRetireService(event: Event): void {
    event.preventDefault();
    void submit(this.retirementForm, () => {
      if (!this.retirementArmed()) {
        this.retirementArmed.set(true);
        return Promise.resolve(null);
      }
      this.requested.emit({ name: 'retireService', args: this.retirement() });
      this.retirementArmed.set(false);
      return Promise.resolve(null);
    });
  }

  protected disarmRetireService(): void {
    this.retirementArmed.set(false);
  }

  protected confirmEndProject(event: Event): void {
    event.preventDefault();
    void submit(this.endingForm, () => {
      if (!this.endingArmed()) {
        this.endingArmed.set(true);
        return Promise.resolve(null);
      }
      this.requested.emit({ name: 'endProject', args: this.ending() });
      this.endingArmed.set(false);
      return Promise.resolve(null);
    });
  }

  protected disarmEndProject(): void {
    this.endingArmed.set(false);
  }
}

function rank(reach: string): number {
  const at = REACH_ORDER.indexOf(reach as (typeof REACH_ORDER)[number]);
  return at === -1 ? REACH_ORDER.length : at;
}

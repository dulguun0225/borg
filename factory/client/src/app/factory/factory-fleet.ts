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
  MATERIAL_CLASSES,
  ROLES,
  ROW_ROLE_PROMPT_OR_SKILL,
  VERDICT_EDIT_IN_PLACE,
} from '../api/types-factory';
import { atInstant } from './format';
import { CallRequest } from './request';

// The verdicts the role prompt row's three actions write. Approve and reject
// are two of the four ../../../../gate/row.go names; the third, edit in
// place, authors a version instead of a verdict and is never sent as one — see
// ./decideRow below.
const NEEDS_TEXT = ['reject', VERDICT_EDIT_IN_PLACE];

// The seven classes of material a fleet entry may be handed, as the seven
// checkboxes the form offers. The keys are the class names
// ../../../../fleetentry/writer.go declares, so the array the call carries is
// this object filtered by MATERIAL_CLASSES and needs no second vocabulary.
interface MaterialClassChoice {
  intent_statement: boolean;
  reports: boolean;
  owner_answer: boolean;
  repository: boolean;
  run_output: boolean;
  constraints: boolean;
  failure_records: boolean;
}

function needed(message: string) {
  return (value: string): ReturnType<typeof requiredError> | null =>
    value.trim() === '' ? requiredError({ message }) : null;
}

function positive(message: string) {
  return (value: number): ReturnType<typeof requiredError> | null =>
    value > 0 ? null : requiredError({ message });
}

// The fleet, the role prompts, and the fleet proposals: the first section of
// the Factory screen after the readiness reading, and the one an install with
// no entry written is stopped at dispatch by.
//
// It performs no call: it emits the call it wants made, and ./factory.ts holds
// the one path every call from this screen takes.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.
@Component({
  selector: 'factory-fleet',
  imports: [FormField],
  templateUrl: './factory-fleet.html',
  styleUrl: './factory.css',
})
export class FactoryFleetSection {
  readonly view = input.required<Factory>();
  readonly openedInWorkAt = input.required<string>();
  // Whether the screen's one path is already sending a call; every
  // submitting control here binds [disabled] to it so a second click while
  // one is in flight has nothing to send.
  readonly busy = input(false);
  readonly requested = output<CallRequest>();

  protected readonly classes = MATERIAL_CLASSES;
  protected readonly entries = computed(() => this.view().FleetEntries ?? []);
  protected readonly prompts = computed(() => this.view().RolePrompts ?? []);
  protected readonly proposals = computed(() => this.view().FleetProposals ?? []);
  protected readonly gateRow = computed(() => this.view().RolePromptGateRow);

  // The roles the view lists, which is one row of the role prompt table per
  // role. Where it lists none — a fresh install has no role prompt version —
  // the closed set ../../../../dispatch/role.go declares stands in its place,
  // an owner composing an entry choosing among those and never inventing one.
  protected readonly roles = computed<readonly string[]>(() => {
    const named = this.prompts().map((each) => each.Role);
    return named.length > 0 ? named : ROLES;
  });

  // The credentials a new entry may run on: the lent credentials not yet
  // taken back.
  protected readonly credentials = computed(() =>
    (this.view().LentCredentials ?? [])
      .filter((each) => !each.TakenBack)
      .map((each) => each.Name),
  );

  protected readonly entry = signal({
    ModelVersion: '',
    Effort: '',
    Role: '',
    ScopeProjectName: '',
    ScopeServiceName: '',
    ScopeAreaName: '',
    Credential: '',
    ProcessingLocation: '',
    MaterialClasses: {
      intent_statement: false,
      reports: false,
      owner_answer: false,
      repository: false,
      run_output: false,
      constraints: false,
      failure_records: false,
    } satisfies MaterialClassChoice,
    ReadAtOnceBound: 0,
    DispatchesBetweenEvalRuns: 0,
  });
  protected readonly entryForm = form(this.entry, (path) => {
    validate(path.ModelVersion, (field) =>
      needed('an entry names the model version it runs')(field.value()),
    );
    validate(path.Role, (field) => needed('an entry names the role it is put on')(field.value()));
    validate(path.Credential, (field) =>
      needed('an entry names the credential it runs on')(field.value()),
    );
    validate(path.ProcessingLocation, (field) =>
      needed('an entry names where the model processes what it is handed')(field.value()),
    );
    validate(path.ReadAtOnceBound, (field) =>
      positive('how much the model reads at once is above zero')(field.value()),
    );
    validate(path.DispatchesBetweenEvalRuns, (field) =>
      positive('dispatches between evaluation-set runs is above zero')(field.value()),
    );
  });

  protected readonly decision = signal({ Verdict: 'approve', Text: '' });
  protected readonly decisionForm = form(this.decision, (path) => {
    validate(path.Text, (field) => {
      if (!NEEDS_TEXT.includes(field.valueOf(path.Verdict))) {
        return null;
      }
      return needed('a reject carries its feedback, and an edit in place its version')(
        field.value(),
      );
    });
  });

  protected at(value: string): string {
    return atInstant(value);
  }

  protected problems(errors: readonly ValidationError[]): string[] {
    return errors.map((each) => each.message ?? 'invalid');
  }

  protected writeEntry(event: Event): void {
    event.preventDefault();
    void submit(this.entryForm, () => {
      const written = this.entry();
      this.requested.emit({
        name: 'writeFleetEntry',
        args: {
          ModelVersion: written.ModelVersion,
          Effort: written.Effort,
          Role: written.Role,
          ScopeProjectName: written.ScopeProjectName,
          ScopeServiceName: written.ScopeServiceName,
          ScopeAreaName: written.ScopeAreaName,
          Credential: written.Credential,
          ProcessingLocation: written.ProcessingLocation,
          MaterialClasses: this.chosenClasses(written.MaterialClasses),
          ReadAtOnceBound: written.ReadAtOnceBound,
          DispatchesBetweenEvalRuns: written.DispatchesBetweenEvalRuns,
        },
      });
      return Promise.resolve(null);
    });
  }

  protected withdrawEntry(id: string): void {
    this.requested.emit({ name: 'withdrawFleetEntry', args: { FleetEntryID: id } });
  }

  // The row every version of a role prompt fires, decided here: it names a
  // version and no item, so Work has nowhere to put it. Edit in place authors
  // a version instead of a verdict, so it sends editRecordRow and not
  // decideRecordRow; the other two actions are two of
  // ../../../../gate/row.go's four verdicts.
  protected decideRow(event: Event): void {
    event.preventDefault();
    const row = this.gateRow();
    if (row === null) {
      return;
    }
    void submit(this.decisionForm, () => {
      const chosen = this.decision();
      if (chosen.Verdict === VERDICT_EDIT_IN_PLACE) {
        this.requested.emit({
          name: 'editRecordRow',
          args: {
            Row: ROW_ROLE_PROMPT_OR_SKILL,
            RecordID: row.VersionID,
            Version: chosen.Text,
            OpenedInWorkAt: this.openedInWorkAt(),
          },
        });
        return Promise.resolve(null);
      }
      this.requested.emit({
        name: 'decideRecordRow',
        args: {
          RowKind: ROW_ROLE_PROMPT_OR_SKILL,
          RecordID: row.VersionID,
          Verdict: chosen.Verdict,
          Reason: chosen.Text,
          OpenedInWorkAt: this.openedInWorkAt(),
        },
      });
      return Promise.resolve(null);
    });
  }

  private chosenClasses(choice: MaterialClassChoice): string[] {
    return MATERIAL_CLASSES.filter((name) => choice[name]);
  }
}

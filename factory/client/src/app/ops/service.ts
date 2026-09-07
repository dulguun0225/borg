import { Component, computed, effect, inject, input, signal } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { RouterLink } from '@angular/router';
import {
  FormField,
  ValidationError,
  form,
  requiredError,
  submit,
  validate,
} from '@angular/forms/signals';
import { ApiClient } from '../api/client';
import { AddressStream, StreamReader } from '../api/stream';
import { OPERATION_SET_INSTANCE_COUNT, OPERATION_SHIFT_TRAFFIC, Service } from '../api/types-ops';
import { ScreenState, screenState } from '../state/screen-state';
import { atInstant } from './format';
import { opsMachine } from './ops';

// The two forms duty 10 takes, which are the two this screen offers as one
// choice: the deployer returns production to the release below the one
// running, which is available while the build it would return to is still
// running, and the revert intent raised after, which names the release that
// failed.
const ROLL_BACK = 'rollBack';
const RAISE_REVERT = 'raiseRevert';

function needed(message: string) {
  return (value: string): ReturnType<typeof requiredError> | null =>
    value.trim() === '' ? requiredError({ message }) : null;
}

// One service on one environment: what it is actually being watched for per
// quantity — the size in force with the finest size its traffic reaches, the
// confidence and power that reading was taken at, whether passed is reachable
// at all, and the average run length with the crossings it admits on a service
// where nothing changed — the emission version its newest record carries,
// unmeasured where the deployer's four fields are missing, the mitigation
// standing on a target with the hours it has stood, and the contracts it
// publishes.
//
// An acting address, not watch-only: every action here re-reads the service
// first and is refused while the subscription is down, and each carries the
// reason the human wrote.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// and
// ../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md.
@Component({
  selector: 'factory-service-screen',
  imports: [NgTemplateOutlet, RouterLink, FormField],
  templateUrl: './service.html',
  styleUrl: './ops.css',
})
export class ServiceScreen {
  private readonly api = inject(ApiClient);
  private readonly streams = inject(StreamReader);

  readonly serviceId = input.required<string>();
  readonly environmentId = input.required<string>();

  // The address this screen is on carries an id, so its one subscription is
  // held in a signal and replaced when the id changes.
  private readonly stream = signal<AddressStream | null>(null);
  private readonly reading = signal(true);
  private readonly failure = signal('');
  private readonly refusal = signal('');
  private readonly view = signal<Service | null>(null);

  protected readonly machine = opsMachine;
  protected readonly message = this.failure.asReadonly();
  protected readonly refused = this.refusal.asReadonly();
  protected readonly service = this.view.asReadonly();
  protected readonly operations = [OPERATION_SHIFT_TRAFFIC, OPERATION_SET_INSTANCE_COUNT];

  protected readonly undo = signal({ Form: ROLL_BACK, ReleaseID: '', Reason: '' });
  protected readonly undoForm = form(this.undo, (path) => {
    validate(path.Reason, (field) => needed('an undo carries the reason it was undone')(field.value()));
    validate(path.ReleaseID, (field) => {
      if (field.valueOf(path.Form) !== RAISE_REVERT) {
        return null;
      }
      return needed('a revert names the release that failed')(field.value());
    });
  });

  protected readonly mitigation = signal({
    TargetID: '',
    Operation: OPERATION_SHIFT_TRAFFIC,
    Share: 0,
    Count: 0,
  });
  protected readonly mitigationForm = form(this.mitigation, (path) => {
    validate(path.TargetID, (field) => needed('a mitigation names the target it is on')(field.value()));
  });

  protected readonly notCaused = signal({ Reason: '' });
  protected readonly notCausedForm = form(this.notCaused, (path) => {
    validate(path.Reason, (field) =>
      needed('a rollback marked not caused by the release carries the reason')(field.value()),
    );
  });

  protected readonly page = signal({ Reason: '' });
  protected readonly pageForm = form(this.page, (path) => {
    validate(path.Reason, (field) => needed('a page carries the reason it was fired')(field.value()));
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
      const open = this.streams.subscribe('service', this.serviceId());
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

  // What a form refused, as the messages beside the field. A validator that
  // wrote no message reads as invalid rather than as nothing.
  protected problems(errors: readonly ValidationError[]): string[] {
    return errors.map((each) => each.message ?? 'invalid');
  }

  protected async read(): Promise<void> {
    this.reading.set(true);
    const result = await this.api.get<Service>(this.address());
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

  protected undoIt(event: Event): void {
    event.preventDefault();
    void submit(this.undoForm, async () => {
      const chosen = this.undo();
      if (chosen.Form === RAISE_REVERT) {
        await this.act(RAISE_REVERT, {
          ServiceID: this.serviceId(),
          ReleaseID: chosen.ReleaseID,
          Reason: chosen.Reason,
        });
        return null;
      }
      await this.act(ROLL_BACK, { ServiceID: this.serviceId(), Reason: chosen.Reason });
      return null;
    });
  }

  protected startMitigation(event: Event): void {
    event.preventDefault();
    void submit(this.mitigationForm, async () => {
      await this.act('startMitigation', this.mitigation());
      return null;
    });
  }

  protected endMitigation(id: string): void {
    void this.act('endMitigation', { MitigationID: id });
  }

  protected markNotCaused(deployId: string, event: Event): void {
    event.preventDefault();
    void submit(this.notCausedForm, async () => {
      await this.act('markRollbackNotCaused', {
        DeployID: deployId,
        Reason: this.notCaused().Reason,
      });
      return null;
    });
  }

  protected firePage(event: Event): void {
    event.preventDefault();
    void submit(this.pageForm, async () => {
      await this.act('firePage', { ServiceID: this.serviceId(), Reason: this.page().Reason });
      return null;
    });
  }

  private address(): string {
    return `/api/service/${this.serviceId()}/on/${this.environmentId()}`;
  }

  // The one path every action takes: refused while the subscription is down,
  // and re-reading the service first, because a screen holding what it can no
  // longer be told about is not a screen an action may be taken from.
  private async act(name: string, args: object): Promise<void> {
    this.refusal.set('');
    if (this.stream()?.state() === 'disconnected') {
      this.refusal.set('the subscription is down, so nothing was sent');
      return;
    }
    const fresh = await this.api.get<Service>(this.address());
    if (fresh.outcome === 'failed') {
      this.refusal.set(`the service could not be re-read, so nothing was sent: ${fresh.message}`);
      return;
    }
    if (fresh.outcome === 'absent') {
      this.refusal.set('this service is no longer at this address, so nothing was sent');
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
  }
}

import { Component, OnDestroy, computed, effect, inject, signal } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { RouterLink } from '@angular/router';
import { FormField, form, requiredError, submit, validate } from '@angular/forms/signals';
import { ApiClient } from '../api/client';
import { StreamReader } from '../api/stream';
import { CreatedAddress, Work } from '../api/types';
import { ScreenState, screenState } from '../state/screen-state';
import { viewerZone } from './format';
import { workMachine } from './work';

function needed(message: string) {
  return (value: string): ReturnType<typeof requiredError> | null =>
    value.trim() === '' ? requiredError({ message }) : null;
}

// What a human supplies at Work: the intent itself, the answers the factory's
// own interview asks for, the reading confirmed once every item of an intent
// has shipped, a commit master holds that the queue did not make, an intent
// ended for good, a constraint whose reach is one intent and the withdrawal of
// one, and an overage authorised on one credential.
//
// Every calendar value a form here takes carries the viewer's IANA zone beside
// it, because every reader of such a value computes in that zone and no other.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// and ../../../../../end-goal/what-humans-do.md.
@Component({
  selector: 'factory-intake-screen',
  imports: [NgTemplateOutlet, RouterLink, FormField],
  templateUrl: './intake.html',
  styleUrl: './work.css',
})
export class IntakeScreen implements OnDestroy {
  private readonly api = inject(ApiClient);
  private readonly streams = inject(StreamReader);

  private readonly stream = this.streams.subscribe('work', '-');
  private readonly reading = signal(true);
  private readonly failure = signal('');
  private readonly outcome = signal('');
  private readonly view = signal<Work | null>(null);

  protected readonly machine = workMachine;
  protected readonly message = this.failure.asReadonly();
  protected readonly reported = this.outcome.asReadonly();
  protected readonly rows = computed(() => this.view()?.Rows ?? []);
  protected readonly zone = viewerZone();

  protected readonly intent = signal({ Statement: '', Services: '', ProjectName: '' });
  protected readonly intentForm = form(this.intent, (path) => {
    validate(path.Statement, (field) => needed('an intent carries a statement')(field.value()));
  });

  protected readonly answer = signal({ QuestionID: '', Answer: '' });
  protected readonly answerForm = form(this.answer, (path) => {
    validate(path.QuestionID, (field) => needed('name the question answered')(field.value()));
    validate(path.Answer, (field) => needed('an answer carries an answer')(field.value()));
  });

  protected readonly reading_ = signal({ IntentID: '', Confirmed: true, Correction: '' });
  protected readonly readingForm = form(this.reading_, (path) => {
    validate(path.IntentID, (field) => needed('name the intent read')(field.value()));
  });

  protected readonly delivery = signal({ ServiceID: '', Commit: '' });
  protected readonly deliveryForm = form(this.delivery, (path) => {
    validate(path.ServiceID, (field) => needed('name the service')(field.value()));
    validate(path.Commit, (field) => needed('name the commit master holds')(field.value()));
  });

  protected readonly ending = signal({ IntentID: '' });
  protected readonly endingForm = form(this.ending, (path) => {
    validate(path.IntentID, (field) => needed('name the intent ended')(field.value()));
  });

  protected readonly constraint = signal({
    IntentID: '',
    Statement: '',
    BindsFrom: '',
    ReviewDate: '',
  });
  protected readonly constraintForm = form(this.constraint, (path) => {
    validate(path.IntentID, (field) => needed('name the intent this binds')(field.value()));
    validate(path.Statement, (field) => needed('a constraint carries a statement')(field.value()));
  });

  protected readonly withdrawal = signal({ ConstraintID: '' });
  protected readonly withdrawalForm = form(this.withdrawal, (path) => {
    validate(path.ConstraintID, (field) => needed('name the constraint withdrawn')(field.value()));
  });

  protected readonly ceiling = signal({ Credential: '' });
  protected readonly ceilingForm = form(this.ceiling, (path) => {
    validate(path.Credential, (field) => needed('name the credential')(field.value()));
  });

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

  protected supplyIntent(event: Event): void {
    event.preventDefault();
    void submit(this.intentForm, async () => {
      const services = this.intent()
        .Services.split(',')
        .map((each) => each.trim())
        .filter((each) => each !== '');
      const created = await this.create('supplyIntent', {
        Statement: this.intent().Statement,
        Services: services,
        ProjectName: this.intent().ProjectName,
      });
      if (created !== '') {
        this.intent.set({ Statement: '', Services: '', ProjectName: '' });
      }
      return null;
    });
  }

  protected answerQuestion(event: Event): void {
    event.preventDefault();
    void submit(this.answerForm, async () => {
      await this.act('answerQuestion', this.answer());
      return null;
    });
  }

  protected confirmReading(event: Event): void {
    event.preventDefault();
    void submit(this.readingForm, async () => {
      await this.act('confirmReading', this.reading_());
      return null;
    });
  }

  protected acceptDelivery(event: Event): void {
    event.preventDefault();
    void submit(this.deliveryForm, async () => {
      await this.act('acceptDelivery', this.delivery());
      return null;
    });
  }

  protected endIntent(event: Event): void {
    event.preventDefault();
    void submit(this.endingForm, async () => {
      await this.act('endIntent', this.ending());
      return null;
    });
  }

  protected supplyConstraint(event: Event): void {
    event.preventDefault();
    void submit(this.constraintForm, async () => {
      // The zone travels with the two calendar values and is the viewer's own:
      // every reader of either computes in it and in no other.
      await this.create('supplyIntentConstraint', { ...this.constraint(), Zone: this.zone });
      return null;
    });
  }

  protected withdrawConstraint(event: Event): void {
    event.preventDefault();
    void submit(this.withdrawalForm, async () => {
      await this.act('withdrawConstraint', this.withdrawal());
      return null;
    });
  }

  protected clearCeiling(event: Event): void {
    event.preventDefault();
    void submit(this.ceilingForm, async () => {
      await this.act('clearCeiling', this.ceiling());
      return null;
    });
  }

  // Every action here is refused while the subscription is down, the same rule
  // every action on an address of this screen is held to.
  private async act(name: string, args: object): Promise<void> {
    this.outcome.set('');
    if (this.stream.state() === 'disconnected') {
      this.outcome.set('the subscription is down, so nothing was sent');
      return;
    }
    const result = await this.api.call(name, args);
    if (result.outcome === 'absent') {
      this.outcome.set('the factory holds no record at that address, so nothing changed');
      return;
    }
    if (result.outcome === 'failed') {
      this.outcome.set(result.message);
      return;
    }
    if (result.outcome === 'value') {
      this.outcome.set('the factory took it');
      await this.read();
    }
  }

  private async create(name: string, args: object): Promise<string> {
    this.outcome.set('');
    if (this.stream.state() === 'disconnected') {
      this.outcome.set('the subscription is down, so nothing was sent');
      return '';
    }
    const result = await this.api.call<CreatedAddress>(name, args);
    if (result.outcome === 'absent') {
      this.outcome.set('the factory holds no record at that address, so nothing was written');
      return '';
    }
    if (result.outcome === 'failed') {
      this.outcome.set(result.message);
      return '';
    }
    if (result.outcome !== 'value' || result.value === null) {
      return '';
    }
    this.outcome.set(`the factory wrote it at ${result.value.id}`);
    await this.read();
    return result.value.id;
  }
}

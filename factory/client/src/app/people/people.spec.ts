import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { ApiClient } from '../api/client';
import { People } from '../api/types-people';
import { ScreenState, malformations } from '../state/screen-state';
import { predicateViolations } from '../state/predicates';
import { FakeEventSource, FakeFetch, installFakes, restoreFakes } from '../../testing/fakes';
import { PeopleScreen, peopleMachine } from './people';

const DECLARED: People = {
  Rows: [
    {
      Key: 'per-1',
      Name: 'the owner',
      Duties: [1, 6, 9, 10],
      Obligations: ['hosting', 'driftdetector'],
      Credentials: [
        {
          Name: 'the-account',
          Kind: 'organisation',
          Ceiling: {
            Amount: 400,
            Currency: 'EUR',
            Period: 'monthly',
            StartDate: '2026-09-01',
            Zone: 'Europe/Berlin',
          },
          Rates: [
            {
              Kind: 'input',
              ModelVersion: 'a-model/3',
              Effort: 'high',
              Amount: 0.000003,
              Currency: 'EUR',
            },
          ],
        },
        {
          Name: 'the-second-account',
          Kind: 'person',
          Ceiling: null,
          Rates: [],
        },
      ],
      ActsAnywhere: true,
    },
    {
      Key: 'per-2',
      Name: '',
      Duties: [],
      Obligations: [],
      Credentials: [],
      ActsAnywhere: false,
    },
  ],
};

describe('People screen', () => {
  let net: FakeFetch;
  let sources: FakeEventSource[];

  beforeEach(() => {
    const fakes = installFakes();
    net = fakes.net;
    sources = fakes.sources;
  });

  afterEach(() => {
    net.release();
    TestBed.resetTestingModule();
    restoreFakes();
  });

  it('declares a well-formed machine', () => {
    expect(malformations(peopleMachine)).toEqual([]);
  });

  it('decides every predicate over every state it declares', async () => {
    for (const state of peopleMachine.states) {
      const fixture = await drive(state);
      expect(predicateViolations(fixture.nativeElement as Element))
        .withContext(`the predicates over ${state}`)
        .toEqual([]);
      fixture.destroy();
      net.release();
    }
  });

  it('reports each row: the duties, the obligations, the credential and its ceiling', async () => {
    const fixture = await drive('ready');
    const text = wordsOf(fixture);
    expect(text).toContain('Duties: 1, 6, 9, 10');
    expect(text).toContain('hosting, driftdetector');
    expect(text).toContain("The account is an organisation's");
    expect(text).toContain('Ceiling 400 EUR per monthly');
    expect(text).toContain('in Europe/Berlin');
    fixture.destroy();
  });

  it('says a row that acts nowhere acts nowhere', async () => {
    const fixture = await drive('ready');
    const text = wordsOf(fixture);
    expect(text).toContain('This row acts nowhere');
    expect(text).toContain('Unbounded: no ceiling is authored');
    fixture.destroy();
  });

  it('refuses a ceiling with no currency and sends one with a currency', async () => {
    const fixture = await drive('ready');
    const root = fixture.nativeElement as HTMLElement;
    const ceiling = only(
      only(root.querySelector('#ceiling-amount'), '#ceiling-amount').closest('form'),
      'the ceiling form',
    );

    await fill(fixture, '#ceiling-credential', 'the-account');
    await fill(fixture, '#ceiling-amount', '400');
    await fill(fixture, '#ceiling-start', '2026-09-01');

    ceiling.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/authorCeiling')).toEqual([]);
    expect(root.textContent).toContain('the currency the rates are authored in is named');

    await fill(fixture, '#ceiling-currency', 'EUR');
    ceiling.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    const sent = net.sent('/api/call/authorCeiling');
    expect(sent.length).toBe(1);
    const args = JSON.parse(sent[0]?.body ?? '{}') as Record<string, unknown>;
    expect(args['Currency']).toBe('EUR');
    expect(args['Amount']).toBe(400);
    // The zone travels beside the start date, because every reader of that
    // date computes in it and in no other.
    expect(args['Zone']).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone);
    fixture.destroy();
  });

  it("renders the server's refusal of an erasure under a legal hold", async () => {
    const fixture = await drive('ready');
    const root = fixture.nativeElement as HTMLElement;
    net.erroring(
      '/api/call/deleteMapping',
      500,
      'legalhold: the mapping is preserved while a hold reaches a record that key is written on',
    );

    await fill(fixture, '#erasure-key', 'per-1');
    only(
      only(root.querySelector('#erasure-key'), '#erasure-key').closest('form'),
      'the erasure form',
    ).dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/deleteMapping').length).toBe(1);
    expect(root.textContent).toContain('the mapping is preserved while a hold reaches');
    fixture.destroy();
  });

  it('sends one call when the erasure form is submitted twice while the first is in flight', async () => {
    const fixture = await drive('ready');
    const root = fixture.nativeElement as HTMLElement;
    await fill(fixture, '#erasure-key', 'per-1');
    const form = only(
      only(root.querySelector('#erasure-key'), '#erasure-key').closest('form'),
      'the erasure form',
    );

    // Both dispatches run synchronously, back to back: the first sets the
    // screen's busy signal before it ever awaits anything, so the second
    // reaches the same guard and sends nothing.
    form.dispatchEvent(new Event('submit'));
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/deleteMapping').length).toBe(1);
    fixture.destroy();
  });

  it('keeps a half-written form through a change on the address', async () => {
    const fixture = await drive('ready');
    const root = fixture.nativeElement as HTMLElement;
    await fill(fixture, '#duty-key', 'per-1');

    // The address reported a change, which re-reads it whole and takes the
    // screen through its loading state. The forms section must not be
    // destroyed by that: what a human has half written is not a record.
    sources[0]?.change();
    await fixture.whenStable();

    const key = only(root.querySelector<HTMLInputElement>('#duty-key'), '#duty-key');
    expect(key.value).toBe('per-1');
    fixture.destroy();
  });

  it('refuses every action while the subscription is down', async () => {
    const fixture = await drive('ready');
    const root = fixture.nativeElement as HTMLElement;
    await fill(fixture, '#duty-key', 'per-1');
    sources[0]?.drop();
    await fixture.whenStable();

    only(
      only(root.querySelector('#duty-key'), '#duty-key').closest('form'),
      'the duty form',
    ).dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(net.sent('/api/call/declareDuty')).toEqual([]);
    expect(root.textContent).toContain('the subscription is down, so nothing was sent');
    fixture.destroy();
  });

  it('renders a version refusal as a required reload and never as a failed action', async () => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    net.refusing('/api/people', 'a later factory');
    const fixture = TestBed.createComponent(PeopleScreen);
    await fixture.whenStable();

    expect(TestBed.inject(ApiClient).reloadRequired()).toBeTrue();
    expect((fixture.nativeElement as HTMLElement).textContent).not.toContain('could not be read');
    fixture.destroy();
  });

  async function drive(state: ScreenState): Promise<ComponentFixture<PeopleScreen>> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    net.reset();
    sources.length = 0;
    if (state === 'failed') {
      net.failing('/api/people', 'the store could not be reached');
    } else if (state === 'empty') {
      net.answering('/api/people', { Rows: [] });
    } else {
      net.answering('/api/people', DECLARED);
    }
    if (state === 'loading') {
      net.holding();
    }
    const fixture = TestBed.createComponent(PeopleScreen);
    if (state === 'loading') {
      // The read is outstanding, so stability is never reached and the render
      // is taken directly rather than waited for.
      fixture.detectChanges();
      return fixture;
    }
    await fixture.whenStable();
    if (state === 'disconnected') {
      sources[0]?.drop();
      await fixture.whenStable();
    }
    return fixture;
  }

  async function fill(
    fixture: ComponentFixture<PeopleScreen>,
    selector: string,
    value: string,
  ): Promise<void> {
    const root = fixture.nativeElement as HTMLElement;
    const control = only(
      root.querySelector<HTMLInputElement | HTMLSelectElement>(selector),
      selector,
    );
    control.value = value;
    control.dispatchEvent(new Event('input'));
    control.dispatchEvent(new Event('change'));
    await fixture.whenStable();
  }
});

// What the screen rendered, as one line: a template's own line breaks are
// whitespace in the DOM, and an assertion about words should not be an
// assertion about where the template wrapped.
function wordsOf(fixture: ComponentFixture<PeopleScreen>): string {
  return (fixture.nativeElement as HTMLElement).textContent.replace(/\s+/g, ' ');
}

// The element a selector was written for, or a failure naming it: a spec that
// silently matched nothing would pass by asserting over an empty DOM.
function only<E extends Element>(found: E | null, what: string): E {
  if (found === null) {
    throw new Error(`nothing matched ${what}`);
  }
  return found;
}

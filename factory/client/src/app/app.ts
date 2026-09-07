import { Component, computed, effect, inject, signal } from '@angular/core';
import { FormField, form, submit } from '@angular/forms/signals';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { ApiClient } from './api/client';
import { StreamReader } from './api/stream';
import { Home } from './api/types';
import { principalKey, setPrincipalKey } from './api/principal';

// The shell around the four screens: the links between them, the badge count
// every screen is read beside, the key the human says they are, and the
// required reload the version refusal is rendered as.
//
// The badge is here rather than on Work alone so that it is readable from
// every screen, and what that costs is a second subscription on the home
// address while the human is at /work — one for the badge and one for the
// home view itself.
//
// What defines it:
// ../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md.
@Component({
  selector: 'factory-shell',
  imports: [RouterOutlet, RouterLink, RouterLinkActive, FormField],
  templateUrl: './app.html',
  styleUrl: './app.css',
})
export class Shell {
  private readonly api = inject(ApiClient);
  private readonly streams = inject(StreamReader);

  private readonly stream = this.streams.subscribe('home', '-');
  private readonly home = signal<Home | null>(null);

  protected readonly reloadRequired = this.api.reloadRequired;
  protected readonly disconnected = computed(() => this.stream.state() === 'disconnected');
  protected readonly badge = computed(() => this.home()?.Badge.Total ?? null);

  protected readonly who = signal({ key: principalKey() });
  protected readonly whoForm = form(this.who);
  private readonly declaredKey = signal(principalKey());
  protected readonly declared = computed(() => this.declaredKey() !== '');

  constructor() {
    effect(() => {
      // Read so that a change on the home address re-reads the badge whole.
      this.stream.changed();
      void this.readBadge();
    });
  }

  protected declareWho(event: Event): void {
    event.preventDefault();
    void submit(this.whoForm, async () => {
      setPrincipalKey(this.who().key);
      this.declaredKey.set(principalKey());
      await this.readBadge();
      return null;
    });
  }

  protected reloadNow(): void {
    globalThis.location.reload();
  }

  private async readBadge(): Promise<void> {
    const result = await this.api.get<Home>('/api/home');
    if (result.outcome === 'value') {
      this.home.set(result.value);
      return;
    }
    // A badge that could not be read shows no number rather than a stale one
    // or a zero, zero being the reading that means the factory is working.
    this.home.set(null);
  }
}

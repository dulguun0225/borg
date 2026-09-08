import { Component, computed, input } from '@angular/core';
import { ApproveUndonePair, Factory } from '../api/types-factory';
import { atInstant, spanOf } from './format';

// One entry of a map the view carries keyed by a name: a service, a gate row,
// or a named human.
export interface Named {
  name: string;
  value: number;
}

// The factory's own numbers, read at time of read over the one span the
// factory owns and nobody authors: throughput, rework rate, gate rejection
// rate, cost per feature with each intent's outcome beside it, the gates a
// resolved factor put a human at, the human's own load with the
// approve-and-undone pair beside it, the two numbers
// that say whether the score's meaning moved under the threshold, the spend
// ceilings, and the page channel.
//
// Nothing here is a control: these are numbers an owner argues with, and every
// value one of them would change is authored or decided where it is defined.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md,
// ../../../../../end-goal/how-the-factory-works/11-screens/04-what-the-factory-auto-approved-and-what-was-undone.md
// and
// ../../../../../end-goal/how-the-factory-works/11-screens/05-the-page-channel-and-what-reached-a-human.md.
@Component({
  selector: 'factory-numbers',
  imports: [],
  templateUrl: './factory-numbers.html',
  styleUrl: './factory.css',
})
export class FactoryNumbersSection {
  readonly view = input.required<Factory>();

  protected readonly numbers = computed(() => this.view().Numbers);
  protected readonly throughput = computed(() => named(this.numbers().ThroughputPerService));
  protected readonly rejectionRate = computed(() => named(this.numbers().GateRejectionRate));
  protected readonly costs = computed(() => this.numbers().CostPerFeature ?? []);
  protected readonly outcomes = computed(() => this.numbers().IntentOutcomes ?? []);
  protected readonly resolvedFactors = computed(() => this.view().ResolvedFactorGates ?? []);
  protected readonly load = computed(() => this.view().HumanLoad ?? []);
  protected readonly selfApprovals = computed(() => this.view().SelfApprovalCounts ?? []);
  protected readonly autoPass = computed(() => this.view().AutoPassRates ?? []);
  protected readonly bands = computed(() => this.view().HeldOutBands ?? []);
  protected readonly ceilings = computed(() => this.view().SpendCeilings ?? []);
  protected readonly splits = computed(() => this.view().LoadSplits ?? []);
  protected readonly channel = computed(() => this.view().PageChannel);
  protected readonly pagesPerService = computed(() => named(this.channel().PagesPerService));
  protected readonly pagesPerHuman = computed(() => named(this.channel().PagesPerHuman));

  // The pair for the factory as a whole is the row whose human key is empty,
  // and every other row is one named human's.
  protected readonly perHuman = computed(() =>
    (this.view().ApproveUndone ?? []).filter((each) => each.HumanKey !== ''),
  );
  protected readonly wholeFactory = computed<ApproveUndonePair | null>(
    () => (this.view().ApproveUndone ?? []).find((each) => each.HumanKey === '') ?? null,
  );

  protected span(seconds: number): string {
    return spanOf(seconds);
  }

  protected at(value: string): string {
    return atInstant(value);
  }
}

function named(map: Record<string, number> | null): Named[] {
  return Object.entries(map ?? {}).map(([name, value]) => ({ name, value }));
}

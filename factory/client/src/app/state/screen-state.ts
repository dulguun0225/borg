// The state machine every screen declares. The factory writes no spec version
// for itself, so a machine for one of the four screens has no record and no
// id; it is held here, in the client's own build, and each screen's spec
// checks its own for the three shapes the Spec gate rejects.
//
// What defines it:
// ../../../../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/04-the-screen-state-machine.md
// and
// ../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md.

// Empty, loading and failed are what the Spec gate requires of every screen
// the factory builds; disconnected is the fourth the subscription adds. Ready
// is the fifth and is not a required state — it is what the other four are
// distinguished from.
export type ScreenState = 'empty' | 'loading' | 'failed' | 'disconnected' | 'ready';

export interface Transition {
  readonly from: ScreenState;
  readonly on: string;
  readonly to: ScreenState;
}

// The machine is closed: a transition it does not declare is forbidden.
export interface ScreenMachine {
  readonly states: readonly ScreenState[];
  readonly initial: ScreenState;
  readonly terminal: readonly ScreenState[];
  readonly transitions: readonly Transition[];
}

// Which state a screen is in, from the four readings every screen has. The
// order is the precedence and it is not arbitrary: a read still in flight has
// nothing to show, a read that failed says so before a subscription that also
// dropped, and a dropped subscription is shown over the rows it holds because
// what it holds is stale. Empty is only reached by a read that succeeded.
export function screenState(
  loading: boolean,
  failed: boolean,
  disconnected: boolean,
  isEmpty: boolean,
): ScreenState {
  if (loading) {
    return 'loading';
  }
  if (failed) {
    return 'failed';
  }
  if (disconnected) {
    return 'disconnected';
  }
  if (isEmpty) {
    return 'empty';
  }
  return 'ready';
}

// The three shapes the Spec gate rejects a machine for, as the messages a
// spec fails on. An empty result is a well-formed machine.
export function malformations(machine: ScreenMachine): string[] {
  const found: string[] = [];
  const declared = new Set(machine.states);

  for (const state of machine.states) {
    const events = new Set<string>();
    for (const t of machine.transitions.filter((each) => each.from === state)) {
      if (events.has(t.on)) {
        found.push(`two transitions on ${t.on} from ${state}`);
      }
      events.add(t.on);
    }
    if (!machine.terminal.includes(state) && events.size === 0) {
      found.push(`${state} is not terminal and no event is declared from it`);
    }
  }

  for (const t of machine.transitions) {
    if (!declared.has(t.from) || !declared.has(t.to)) {
      found.push(`the transition ${t.from} --${t.on}--> ${t.to} names an undeclared state`);
    }
  }

  const reached = new Set<ScreenState>([machine.initial]);
  let growing = true;
  while (growing) {
    growing = false;
    for (const t of machine.transitions) {
      if (reached.has(t.from) && !reached.has(t.to)) {
        reached.add(t.to);
        growing = true;
      }
    }
  }
  for (const state of machine.states) {
    if (!reached.has(state)) {
      found.push(`${state} is reached by no path of declared transitions from ${machine.initial}`);
    }
  }

  return found;
}

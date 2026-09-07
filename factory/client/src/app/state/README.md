# state

The state machine every screen declares and the four predicates decided over each of its
states. Imports nothing of the app, the same rule `../api/` is held to.

| File | What it owns |
|---|---|
| `screen-state.ts` | The five states, the machine type, `screenState`, and `malformations` |
| `predicates.ts` | The contrast floor, the name on every control, the focus order, and the target size |

The factory writes no spec version for itself, so a machine for one of the four screens
has no record and no id: each screen declares its own as a constant in its own
`<screen>.ts`, the client's build holds it, and the screen's spec checks it with
`malformations` for the three shapes the Spec gate rejects — two transitions on one event
from one state, a state no path of declared transitions reaches from the initial one, and
a state that is not terminal with no event declared from it.

`screenState` computes which state a screen is in from the four readings every screen
has. The precedence is loading, failed, disconnected, empty, ready, and it is not
arbitrary: a read still in flight has nothing to show, a read that failed says so before
a subscription that also dropped, and a dropped subscription is reported over the rows it
holds because what it holds is stale.

`predicates.ts` is used by every screen's spec and by nothing the build ships. It is here
rather than under `../../testing/` so that the four screens decide the same four
predicates from one place.

## What defines it

[The screen state machine](../../../../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/04-the-screen-state-machine.md);
[The third outcome](../../../../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/01-the-third-outcome.md);
[The screens as software](../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md).

# Work

One item is one timeline. Intent, spec, plan, tasks, implementation, rollout and the
numbered release it ends in, in the order they happened, with each gate shown inline at
the point where it fired: the vector while a human decides, the number beside the verdict
once it is written, the acknowledgements and by whom, and at the Implementation gate the
build's diff against master — the one place the product renders code.

Filtered to what waits on a human, it is the home view and has the badge count. Beside
the badge sit a row per last check record whose component owes a further pass, named as
"the health monitor has not checked payments since 04:10" and never as a duration, and
the readiness reading per role. Only where nothing waits does the digest appear.

| Address | File pair | What it is |
|---|---|---|
| `/work` | `work.ts`, `work.html` | The home view: the badge, the last check rows, the readiness reading, the digest at zero, and every row waiting on a human |
| `/work/all` | `board.ts`, `board.html` | The board: every item and where each is stuck |
| `/work/intake` | `intake.ts`, `intake.html` | What a human supplies: an intent, an interview answer, the reading confirmed, a commit master holds, an intent ended, an intent-reach constraint and its withdrawal, a ceiling cleared |
| `/work/item/:id` | `item.ts`, `item.html` | One item's timeline, its priority, ending it, and, where a hold of the factory's own stands at the production deploy row and is approvable from here, approving through it |
| `/work/decision/:id` | `decision.ts`, `decision.html` | One gate: acknowledge, approve, reject, hold, refer, edit in place, take over |

The shape every screen directory holds: `README.md`, `work.ts`, `work.html`, `work.css`,
`work.spec.ts`, `work.routes.ts`, and `format.ts`. Work holds one further file pair per
address above; the other three screens have fewer addresses and so fewer pairs.
`format.ts` is a copy of the same file in the other three screen directories, which is
what the import boundary costs.

`workMachine` is declared in `work.ts` and is the machine every address of this screen
declares: the five states, the transitions between them, and no terminal state.

Every action on an address of this screen re-reads its record first and sends nothing
where a close or an abandonment has reached the row since it was drawn, which is where
the log's refusal of a second close is met. Every action is refused while the
subscription is down. Every verdict carries `OpenedInWorkAt`, the instant in UTC at which
the row was opened here — taken when the screen was created and not when the button was
pressed, because what the close event records is when the actor started reading.

Every calendar value a form here takes carries the viewer's IANA zone beside it, because
every reader of such a value computes in that zone and no other.

`item.ts` places every timeline entry by the instant its own record carries and by
nothing else, so the order is what the records say happened rather than an order this
file assigns to a stage. An entry with no time sorts last. The diff is shown at the
decision whose gate row is the Implementation gate, and on its own where the item's
decisions name none — never withheld.

## What defines it

[Work, Ops, Factory, People](../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md) (C2565, C2566, C2567, C2568, C2569, C2576, C2577, C2578, C2579, C2580, C2584, C2585, C2590, C2592, C2593, C2595);
[Three properties every screen needs](../../../../../end-goal/how-the-factory-works/11-screens/02-three-properties-every-screen-needs.md) (C2679, C2680, C2683, C2686, C2688, C2690, C2691, C2693);
[The screens as software](../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md) (C2698, C2700, C2701, C2702, C2703, C2704, C2705, C2706, C2707, C2711, C2712, C2713, C2714, C2715, C2716);
[Actions at each gate](../../../../../end-goal/how-the-factory-works/03-gates/03-actions-at-each-gate.md) (C0940);
[What humans do](../../../../../end-goal/what-humans-do.md) (C2862, C2863, C2870, C2883, C2884).

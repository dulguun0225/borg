# Work

One item is one timeline. Intent, spec, plan, tasks, implementation, rollout and the
numbered release it ends in, in the order they happened, with each gate shown inline at
the point where it fired: the vector while a human decides, the number beside the verdict
once it is written, the acknowledgements and by whom, and at the Implementation gate the
build's diff against master — the one place the product renders code.

Filtered to what waits on a human, it is the home view and has the badge count. Beside
the badge sit a row per last check record whose component owes a further pass, named as
"the health monitor has not checked payments since 04:10" and never as a duration, the
readiness reading per role, and what a safeguard on the report store is holding. Only
where nothing waits does the digest appear.

| Address | File pair | What it is |
|---|---|---|
| `/work` | `work.ts`, `work.html` | The home view: the badge, the last check rows, the readiness reading, the digest at zero, every row waiting on a human, and the report and the report-derived intent each admission safeguard is holding, with the admission of each |
| `/work/all` | `board.ts`, `board.html` | The board: every item and where each is stuck |
| `/work/intake` | `intake.ts`, `intake.html` | What a human supplies: an intent, an interview answer, the reading confirmed, a commit master holds, an intent ended, an intent-reach constraint and its withdrawal, a ceiling cleared |
| `/work/item/:id` | `item.ts`, `item.html` | One item's timeline, its priority, ending it, the reports grouped into its intent with the admission of each and of the group, and, where a hold of the factory's own stands at the production deploy row and is approvable from here, approving through it |
| `/work/decision/:id` | `decision.ts`, `decision.html` | One gate: acknowledge, approve, reject, hold, refer, edit in place, take over |
| — | `work.e2e.ts` | The browser run: the screen driven in Chromium against the factory's own process |

The shape every screen directory holds: `README.md`, `work.ts`, `work.html`, `work.css`,
`work.spec.ts`, `work.e2e.ts`, `work.routes.ts`, and `format.ts`. Work holds one further
file pair per address above; the other three screens have fewer addresses and so fewer
pairs. `work-admission.spec.ts` is split from `work.spec.ts` by subject at the length a
file is held to, and holds the two admissions on the report store wherever they are
rendered — the home view and one item — the way `factory-constraint.spec.ts` is split
there. `format.ts` is a copy of the same file in the other three screen directories, which
is what the import boundary costs.

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

The reports an end user sent are rendered under the intent's own entry — the statement
`item.ts` opens the item with — and on no address of their own, a browsable list of them
being what the design refuses. Each carries the harm mark the reporter set, the arrival
rendered in the viewer's zone through `format.ts`, and the notice it was shown or that
none was in force. A report no human has admitted carries the admission of that one
report, and an intent no human has admitted carries the admission of the intent, which is
one action over the group the intent already is.

The two admissions are separate and each has its own safeguard on the report store, so a
group whose every report is admitted may still be an intent nobody has admitted. Neither
wait has an item behind it — an unadmitted report is grouped into nothing and an
unadmitted intent is decomposed into nothing — so both are rows of the home view, where a
human meets them before there is a timeline to open. The item's own view carries the
intent's admission too, for a group admitted after it was decomposed. Where an owner
placed neither safeguard nothing waits, the badge counts none, and the two lists are
absent.

`item.ts` places every timeline entry by the instant its own record carries and by
nothing else, so the order is what the records say happened rather than an order this
file assigns to a stage. An entry with no time sorts last. The diff is shown at the
decision whose gate row is the Implementation gate, and on its own where the item's
decisions name none — never withheld.

## What defines it

[Work, Ops, Factory, People](../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md) (C2565, C2566, C2567, C2568, C2569, C2570, C2576, C2577, C2578, C2579, C2580, C2583, C2584, C2585, C2590, C2592, C2593, C2595);
[Three properties every screen needs](../../../../../end-goal/how-the-factory-works/11-screens/02-three-properties-every-screen-needs.md) (C2678, C2679, C2680, C2683, C2686, C2688, C2690, C2691, C2693);
[Reports](../../../../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md) (C0438, C0439, C0473);
[The screens as software](../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md) (C2698, C2700, C2701, C2702, C2703, C2704, C2705, C2706, C2707, C2711, C2712, C2713, C2714, C2715, C2716, C2992, C2993, C2994);
[Actions at each gate](../../../../../end-goal/how-the-factory-works/03-gates/03-actions-at-each-gate.md) (C0940);
[What humans do](../../../../../end-goal/what-humans-do.md) (C2862, C2863, C2870, C2883, C2884);
[The interview](../../../../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md) (C0536, C0581, C0599, C0609, C0614);
[Decomposition](../../../../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md) (C0788);
[Where a gate is, and what decides it](../../../../../end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md) (C0880, C0884);
[Tasks](../../../../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/04-tasks.md) (C1106);
[Implementation](../../../../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/README.md) (C1141);
[What an item names](../../../../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/02-what-an-item-names.md) (C0665).

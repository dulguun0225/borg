# Gates

## Where a gate sits, and what decides it

A gate sits at every stage boundary: after the cut into items, the spec, implementation plan, tasks, and implementation are authored, and before each merge and each deploy — an event gate decides whether the event happens, which is what makes hold a stop rather than an undo. The mechanism is permanent — it does not fade as the factory improves.

The factory scores each change and auto-passes what it judges low risk. The same score picks the rollout strategy: A/B, canary, blue-green, straight. Humans override by pinning a gate always-on or pinning a strategy, and can veto after the fact.

A failing rollout rolls back on its own, inside its [_watch window_](08-operations.md#the-watch-window) — no human in the loop, no waiting. The rollback is reported, not requested.

Veto after the fact assumes the change can still be undone, and what a human may do decays as later work builds on it: a rollback while the release's control still stands, a revert once it does not. Reversibility is a scored dimension and the veto window is bounded by it, through the strategy it picks and the control that strategy pays for. It decays that way and no other: with one item per release, a change is never harder to undo because it happened to ship alongside nine others. What overlapping watch windows add is not difficulty but reach — a rollback takes every release above its target, up to the K of [_Overlapping windows_](08-operations.md#overlapping-windows), which is the one bundle the factory ships and is bounded by the number itself.

## Actions at each gate

| Gate | Actions |
|---|---|
| Decomposition | Approve · Reject with feedback · Edit in place |
| Spec | Approve · Reject with feedback · Edit in place |
| Implementation plan | Approve · Reject with feedback · Edit in place |
| Tasks | Approve · Reject with feedback · Edit in place |
| Implementation | Approve · Reject with feedback |
| Deploy to candidate environment | Approve · Hold · Reject with feedback |
| Merge to master | Approve · Reject with feedback |
| Deploy to production | Approve · Hold · Pin strategy |

Those eight rows are the default path, not the whole set. A gate sits before every deploy, so a customer that defines more environments gets a row for each. It gets no more merge rows: one long-lived branch backs the promotion path, so the extra environments are deploy targets.

They are fed from master, and the candidate-fed row stays the factory's own. A customer environment persists, and a persistent one fed from candidate builds is a slot two candidates take turns on — the shared environment [_An environment per candidate_](05-environments.md#an-environment-per-candidate) exists to remove, charging one item's wait to everything behind it. A human who wants a pre-merge change in front of them gets it on that candidate's own environment, where a pin (9) puts them and the wait is that item's alone. What it costs is that there is no long-lived box to keep one standing in.

What a new row carries follows from what feeds it, not from which row it was copied off. Fed from master, every new row carries what Deploy to production carries: the merge has happened and the number is spent, so hold is the only stop and undoing is veto after the fact. Reject is available up to the merge to master and nowhere after it.

## What a gate may change

At a gate, artifacts are editable by hand. Code is not: a gate approves or rejects an implementation, it never hand-patches one. A human who wants different code authors it upstream and sends it back through the pipeline.

Merge and deploy gates edit nothing at all — what they decide is an event, not a document. Reject sends the change back up the pipeline; hold leaves the event queued with the change still good, which is why only the deploy rows offer it. What a hold waits on differs by gate: at the production deploy gate a window, a rollback standing in front of the revert, a declared dependency that is not live, or a record the reconciler caught disagreeing with what runs; at the candidate deploy gate a dependency that is not live yet — the producing release of a contract migration, since a candidate environment stands on its dependencies' current releases and never another service's candidate. The two are different answers and have to stay distinguishable: the score learns from a reject and should learn nothing from a hold.

## The attempt bound

A stage also carries an attempt bound, authored with the rest of gate policy (8). An item that exceeds it stops being retried and stands in Inbox as an escalation (12) — the factory saying it cannot do this one. [_The interview_](02-intent-into-items.md#the-interview) carries one too, counting rounds, though it is upstream of the first stage and has no gate of its own. Holds do not count against the bound; a hold is not a failed attempt, for the same reason the score does not learn from one. The bound costs something wherever it is set: low turns solvable work into human work, high burns spend before anyone sees the item.

## What particular gates carry

### Decomposition

The set is what stands here — how many items, where each lands, and what waits on what — so a rejection re-cuts the set rather than sending an item back, there being no item yet to send anywhere. Edit in place is a human re-cutting by hand. It is the one gate where approving admits several threads at once; everything below it is per item. [_The cut_](02-intent-into-items.md#the-cut) sets out the stage and what a gate on it costs.

### Spec

Two duties. [_The interview_](02-intent-into-items.md#the-interview) (3) refines intent and the spec is what it produces, so approving the spec ratifies that refinement — there is no interview gate and none is missing. The spec also states the acceptance criteria, so approving it confirms them (6). What is confirmed is the criteria; a test encoding them is downstream of that approval, not the object of it.

Criteria are predicates, not prose, for the reason a consumer's declaration is: what cannot be decided against an observed run is checked by nobody, and in the steady state that is most of them — the factory drafts its own criteria and a human reads them only where the score or a pin (9) puts one there. Each states one testable behaviour in one of a closed set of six sentence patterns, drawn from EARS.

| Pattern | The sentence it makes |
|---|---|
| **always true** | `the system shall <response>` |
| **event** | `When <trigger>, the system shall <response>` |
| **state** | `While <state>, the system shall <response>` |
| **unwanted condition** | `If <condition>, then the system shall <response>` |
| **optional feature** | `Where <feature is included>, the system shall <response>` |
| **state with an event inside it** | `While <state>, when <trigger>, the system shall <response>` |

Ids are stable and never reused, the rule the number already keeps, and a criterion that is dropped stays withdrawn rather than vacating its id. A sentence fitting no pattern is admitted with a tagged reason and counted, because a form everything escapes is not one.

The form is the whole of what this buys, and buying it makes one failure worse: pattern-perfect criteria can describe an incomplete set, and the incompleteness now stands behind a passing check. The unwanted conditions are where that lands — a set of only happy-path criteria is half a set, and nothing mechanical will ever ask for the other half.

Where the item has a user interface, its state flow is part of those criteria — the states a screen can hold, the events that move it between them, and what each state forbids, empty and loading and failed among them. It is authored as a machine rather than a sketch, which is what makes it enforceable at [_Implementation_](#implementation) — and what gives a designer holding (6) a say that survives past the gate they stood at.

The cost is the authoring, the same cost declared meaning carries in [_What a diff cannot see_](07-contracts.md#what-a-diff-cannot-see) — a sketch is cheaper and checks nothing. What the screen looks like is not here: that is the design system the project chose as a standing constraint (2), a screen design where one rode in with the request as a per-item one, and, where neither answers, [_Implementation plan_](#implementation-plan).

### Implementation plan

How the item will be built, and the decisions no standing constraint (2) answers — what a screen looks like where the design system is silent is the common one. That is what Edit in place is for: the plan is a document, so a human who wants a different approach edits one into it rather than rejecting the item to get one. Reject is for a plan that is wrong, not for one that is merely not theirs.

### Tasks

A task is an internal step of one item and never a unit that ships: one item is one release, and a task has no build, no number, and no environment of its own. The factory authors them from the approved plan — the plan is how the item will be built, the tasks are that cut into work an agent picks up — and they are spent when the implementation lands.

What the gate buys is a look at the breakdown before agents run on it, and Edit in place is where a human resequences or splits one without touching the plan above it. A task that cannot be finished escalates nothing by itself: the attempt bound is per stage, so what stands in Inbox (12) is the item.

### Implementation

Where the state machine the spec authored is enforced. An implementation that admits a forbidden transition is rejected here, mechanically rather than on taste — the whole of what authoring the flow as a machine bought.

It is also the one artifact gate with no Edit in place, for the reason [_What a gate may change_](#what-a-gate-may-change) gives. The actions are the narrowest in the table: approve what was built, or send the item back up the pipeline to have it built differently.

### Deploy to candidate environment

The dependency its hold waits on was declared at the cut, so what this gate is waiting for is known before the item was ever built — nothing has to be discovered at deploy time. Here the wait is for that dependency to become live at all, since the environment is composed from it. The same dependency is checked once more at the production deploy gate, where the question is whether it is live still.

Nothing else attaches to this row — no strategy, no rollout, no watch window — for the reason [_What the candidate environment decides_](05-environments.md#what-the-candidate-environment-decides) gives. What the deploy buys is the criteria.

### Merge to master

The release event — where a candidate becomes a numbered release — and also where the verdict on the candidate lands. Approving it admits the candidate to the merge queue, which re-verifies it against the master it will actually land on and rejects it there on the same terms if that fails; rejecting it here sends the item back up the pipeline, and nothing waits on the environment it held, which is torn down with it. The verdict is a human's when the score or a pin puts one there and the factory's own otherwise, and a human standing there is performing UAT (7).

### Deploy to production

The default path's exception to reject, which is why that row does not offer it. By the time a change stands there the merge has happened and the number is spent, so hold is the only way to stop it. Once it deploys, undoing it is veto after the fact — a rollback while its control stands, a revert after, which is a new item.

Its hold also fires on the factory's own account, like the one a rollback leaves standing. A dependency declared at the cut that is not its service's [_current release_](06-releases.md#the-number) when this gate fires holds the deploy, and the check runs at the moment of firing — the rule the two contract baselines already keep — because a producer that was live when its consumer verified can be rolled back before that consumer lands. Nothing is decided and no [_page_](08-operations.md#pages) fires; the hold lifts when the dependency is current again. A human can approve through it, as through every hold the factory sets, and what approving buys is the break the hold was standing in front of.

The hold [_The reconciler_](08-operations.md#the-reconciler) sets is the other shape and the only one of it: what the factory recorded about this service is not what is running, so nothing here can be decided on the record, and no evidence the factory can gather lifts it. That one pages, for the reason the dependency hold does not — it waits on a human and on nothing else. Approving through is still offered, and here it is the human saying the record is wrong and the deploy should go anyway.

What it does not cover is the consumer already in production when its producer rolls back. A deploy gate stops deploys, and by then there is nothing left to stop — what surfaces that one is the consumer's own error rate raising an item, the same answer the factory gives to every consumer break it cannot see from the producer's side.

### Why those last two are gates

They are the factory's own steady state. Both exist, both are scored, and both are auto-passed by default, as is every gate above them. Pinning either one puts a human back in prod's path without inventing a new mechanism.

# Gates

## Where a gate sits, and what decides it

A gate sits at every stage boundary: after the spec, implementation plan, tasks, and implementation are authored, and before each merge and each deploy — an event gate decides whether the event happens, which is what makes hold a stop rather than an undo. The mechanism is permanent — it does not fade as the factory improves.

The factory scores each change and auto-passes what it judges low risk. The same score picks the rollout strategy: A/B, canary, blue-green, straight. Humans override by pinning a gate always-on or pinning a strategy, and can veto after the fact.

A failing rollout rolls back on its own, inside its watch window — no human in the loop, no waiting. The rollback is reported, not requested.

Veto after the fact assumes the change can still be undone, and that assumption decays as later work builds on it. Reversibility is a scored dimension, and the veto window is bounded by it. It decays that way and no other: with one item per release, a change is never harder to undo because it happened to ship alongside nine others.

## Actions at each gate

| Gate | Actions |
|---|---|
| Spec | Approve · Reject with feedback · Edit in place |
| Implementation plan | Approve · Reject with feedback · Edit in place |
| Tasks | Approve · Reject with feedback · Edit in place |
| Implementation | Approve · Reject with feedback |
| Deploy to candidate environment | Approve · Hold · Reject with feedback |
| Merge to master | Approve · Reject with feedback |
| Deploy to production | Approve · Hold · Pin strategy |

Those seven rows are the default path, not the whole set. A gate sits before every deploy, so a customer that defines more environments gets a row for each. It gets no more merge rows: one long-lived branch backs the promotion path, so the extra environments are deploy targets.

What a new row carries follows from what feeds it, not from which row it was copied off. An environment fed from a candidate build carries what Deploy to candidate environment carries — the change is still a candidate, so Reject sends it back up the pipeline. An environment fed from master carries what Deploy to production carries: the merge has happened and the number is spent, so hold is the only stop and undoing is a revert. Reject is available up to the merge to master and nowhere after it.

## What a gate may change

At a gate, artifacts are editable by hand. Code is not: a gate approves or rejects an implementation, it never hand-patches one. A human who wants different code authors it upstream and sends it back through the pipeline.

Merge and deploy gates edit nothing at all — what they hold is an event, not a document. Reject sends the change back up the pipeline; hold leaves it queued at the gate, for a window or a dependency, with the change still good. The two are different answers and have to stay distinguishable: the score learns from a reject and should learn nothing from a hold.

## The attempt bound

A stage also carries an attempt bound, authored with the rest of gate policy (8). An item that exceeds it stops being retried and stands in Inbox as an escalation (12) — the factory saying it cannot do this one. Holds do not count against the bound; a hold is not a failed attempt, for the same reason the score does not learn from one. The bound costs something wherever it is set: low turns solvable work into human work, high burns spend before anyone sees the item.

## What particular gates carry

### Spec

Two duties. The interview (3) refines intent and the spec is what it produces, so approving the spec ratifies that refinement — there is no interview gate and none is missing. The spec also states the acceptance criteria, so approving it confirms them (6). What is confirmed is the criteria; a test encoding them is downstream of that approval, not the object of it.

Criteria are predicates, not prose, for the reason a consumer's declaration is: what cannot be decided against an observed run is checked by nobody, and in the steady state that is most of them — the factory drafts its own criteria and a human reads them only where the score or a pin (9) puts one there. Each states one testable behaviour in one of a closed set of sentence patterns, six of them under EARS.

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

Where the item has a user interface, its state flow is part of those criteria — the states a screen can hold, the events that move it between them, and what each state forbids, empty and loading and failed among them. It is authored as a machine rather than a sketch, which is what makes it enforceable: an implementation that admits a forbidden transition is a rejection at the implementation gate, mechanical rather than a taste call, so a designer holding (6) has a say that survives past the gate they stood at.

The cost is the authoring, the same cost declared meaning carries in [_What a diff cannot see_](06-contracts.md#what-a-diff-cannot-see) — a sketch is cheaper and checks nothing. What the screen looks like is not here: that is the design system supplied as a standing constraint (2), and, where the system does not answer, the implementation plan, whose gate offers Edit in place for exactly that.

### Merge to master

The release event — where a candidate becomes a numbered release — and also where the verdict on the candidate lands. Approving it admits the candidate to the merge queue, which re-verifies it against the master it will actually land on; rejecting it sends the item back up the pipeline, and nothing waits on the environment it held, which is torn down with it. The verdict is a human's when the score or a pin puts one there and the factory's own otherwise, and a human standing there is performing UAT (7).

### Deploy to production

The default path's exception to reject, which is why that row does not offer it. By the time a change stands there the merge has happened and the number is spent; hold is the only way to stop it, and undoing it is a revert, which is a new item. That is veto after the fact under another name.

### Why those last two are gates

They are the factory's own steady state. Both exist, both are scored, and both are auto-passed by default, as is every gate above them. Pinning either one puts a human back in prod's path without inventing a new mechanism.

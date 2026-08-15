# Gates

## Where a gate is, and what decides it

There is a gate at every stage boundary: after the cut into items, and after the spec, implementation plan, tasks, and implementation are authored, and before each merge and each deploy — an event gate decides whether the event happens, which is what makes hold a stop rather than an undo. The mechanism is permanent — it does not weaken as the factory improves.

The factory scores each change and auto-passes what it judges low risk. The same score picks the [_rollout strategy_](#the-rollout-strategy). Humans override by pinning a gate always-on or pinning a strategy, and can veto after the fact.

A gate firing writes one **decision**, into the one append-only log seam 2 of [_Deferred, but not designed out_](../deferred.md) describes, which is that log's single writer with the gate component as its caller. The record is opened when the gate fires and closed when the verdict is given — one record, two writes at two events, the shape the [_deploy record_](06-releases.md#the-deploy-record) already uses when its status advances. What forces the write at the firing is the factor vector: a human is meant to argue with the score's number before deciding, so the vector has to exist while they are deciding, and it cannot be computed when they open the row because the score version moves as outcomes arrive. Opened, it names the gate row, the item, the build where one exists, the vector and the number, the policy version and the score version, the values actually applied, and the duty or named human the row waits on. Closed, it names the verdict and its actor. It is written against the item and the build and never against the release, including at the production deploy gate, which fires after the release exists — one rule for all eight rows, so no reader has to know which side of the merge a gate sits on. What that costs is a decision with no verdict for as long as a human takes to decide, so every reader tells a pending row from a decided one, and an item stopped at the attempt bound leaves one that never receives a verdict at all.

A failing rollout rolls back on its own, inside its [_watch window_](08-operations.md#the-watch-window) — no human involved, no waiting. The rollback is reported, not requested.

Veto after the fact assumes the change can still be undone, and what a human may do narrows as later work builds on it: a rollback while the release's control is still running, a revert once it is not. Reversibility is a scored dimension and what a human may still do is limited by it, through the strategy it picks and the [_control_](08-operations.md#the-health-signal) that strategy keeps running. It narrows that way and no other: with one item per release, a change is never harder to undo because it shipped alongside nine others. What overlapping watch windows add is not difficulty but reach — a rollback undoes every release above its target, up to the K of [_Overlapping windows_](08-operations.md#overlapping-windows), which is the one set of items the factory ships together and is limited by K itself.

## The rollout strategy

A **rollout strategy** is how a release takes live traffic from the build it replaces. It attaches to a production deploy and to no other, which is narrower than master-fed: what a strategy decides is whether a control runs, a control is the instances a comparison is made against, and that comparison exists only in production because organic traffic does. A customer's master-fed pre-production environment has no more of it than a candidate one, so a strategy there would decide nothing that anything reads. That is the same division [_The health signal_](08-operations.md#the-health-signal) was already drawn on.

| Strategy | How the release takes traffic |
|---|---|
| **with a control** | on a schedule, with the build it replaces serving the rest throughout — a share widened as the comparison stays clear, a share kept fixed while the two are compared, or all of it at once switched to a second complete copy running beside the one it replaces |
| **straight** | all of it, in place, with none of the build it replaces left running |

Two rows and not four, because they differ on one axis and everything downstream reads that axis alone: whether the build being replaced is still serving while the release does. That is what a [_control_](08-operations.md#the-health-signal) requires, and what veto after the fact (10) uses while it runs — a straight deploy has neither. The schedule is an attribute of the first row rather than a strategy of its own. Canary, A/B, and blue-green are the three a builder arrives already knowing, and they are that row at three schedules; the drawback of folding them in is that a name a builder brings with them is now a value on a field instead of a row to implement.

A service's first release is straight whatever the score would prefer. There is no build being replaced, so nothing can keep serving beside it and there is no control to run — the choice exists from the second release, and what the first one is measured against instead is in [_The health signal_](08-operations.md#the-health-signal).

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

Those eight rows are the default path, not the whole set. There is a gate before every deploy, so a customer that defines more environments gets a row for each. It gets no more merge rows: one long-lived branch is the promotion path, so the extra environments are deploy targets.

They are fed from master, and the candidate-fed row stays the factory's own. A customer environment persists, and a persistent one fed from candidate builds is a slot two candidates take turns on — the shared environment [_An environment per candidate_](05-environments.md#an-environment-per-candidate) exists to remove, which delays everything behind whichever item is using it. A human who wants to see a pre-merge change sees it on that candidate's own environment, where a pin (9) puts them and the delay is that item's alone. The drawback is that there is no long-lived environment to keep a candidate running in.

What actions a new row has follows from what feeds it, not from which row it was copied from. Fed from master, every new row has what Deploy to production has: the merge has happened and the number is already assigned, so hold is the only stop and undoing is veto after the fact. Reject is available up to the merge to master and nowhere after it.

That is the actions and only the actions. The [_strategy_](#the-rollout-strategy) and the [_watch window_](08-operations.md#the-watch-window) follow production rather than master, so a new row fed from master gets neither — the number is already assigned, and what makes a control worth running is organic traffic the row does not have.

## What a gate may change

At a gate, artifacts are editable by hand. Code is not: a gate approves or rejects an implementation, it never hand-patches one. A human who wants different code authors it upstream and sends it back through the pipeline.

Merge and deploy gates edit nothing at all — what they decide is an event, not a document. Reject sends the change back up the pipeline; hold leaves the event queued with the change still good, which is why only the deploy rows offer it. What a hold waits on differs by gate: at the production deploy gate the service's K windows already open, a rollback blocking the revert, a declared dependency that is not live, or a record the reconciler found disagreeing with what runs; at the candidate deploy gate a dependency that is not live yet — the producing release of a contract migration, since a candidate environment is composed from its dependencies' current releases and never from another service's candidate — or a substrate with no room for another environment, the one condition here the cut could not have declared. The two are different answers and have to stay distinguishable: the score learns from a reject and should learn nothing from a hold.

There are three kinds of hold and only one is written as a decision. A hold a human sets is the verdict of that firing's decision record, with that human as the actor. A hold the factory sets over a record that already exists — an open window, a dependency that is not current, a rollback whose revert has not shipped, a reconciler mismatch — writes nothing and is recomputed at every firing, which is what checking at the moment of firing already requires; a record for it would be a decision where the tree says nothing is decided, and re-testing would append one every time the gate re-fired. A wait the factory could not compute at a firing, because no gate fired or because the condition is not a record — a credential that cannot be reached, a substrate with no room — is written into the log by the log, with the component that met it as the caller and the actor, so Work has a row to show. None of the three counts against the attempt bound and none teaches the score. What that costs is that one of the three is written and two are not, so how long the factory has been holding is answerable for the third alone.

## The attempt bound

Every stage also has an attempt bound, authored with the rest of gate policy (8). An item that exceeds it stops being retried and appears in [_Work_](11-surfaces.md#work-ops-factory-people) as an escalation (12) — the factory saying it cannot do this one. What the bound is compared against is the item's own attempt count, which [_What an item names_](02-intent-into-items.md#what-an-item-names) puts on the item and dispatch writes: counting the rejects in the log instead would miss every attempt that failed before a gate fired, which is exactly what a high bound spends agent time on. It is one parameter and not two: [_The interview_](02-intent-into-items.md#the-interview) counts rounds against the same bound, though it is upstream of the first stage and has no gate of its own, because a second row would be a different number on the same mechanism and would be a row every builder implements twice. Holds do not count against the bound; a hold is not a failed attempt, for the same reason the score does not learn from one. The bound is wrong in both directions: low turns solvable work into human work and stops an interview that was about to converge, high spends agent time before anyone sees the item. That spending is limited by the quota on the provider account behind the agent and by nothing else — [_The fleet_](10-fleet.md#what-the-fleet-is-not) refuses a ceiling of the factory's own, which leaves this bound the only limit the factory holds, and it counts attempts rather than money.

## What particular gates decide

### Decomposition

The one gate where approving admits several threads at once; everything below it is per item, and it fires only where the cut yielded more than one. [_The cut_](02-intent-into-items.md#the-cut) sets out the stage, what is decided at it, and the drawback of firing it conditionally.

### Spec

Two duties. [_The interview_](02-intent-into-items.md#the-interview) (3) refines intent and the spec is what it produces, so approving the spec ratifies that refinement — there is no interview gate and none is missing. A spec version also names the acceptance criteria it introduces and the ones it withdraws, so approving it confirms that change to the set (6). What is confirmed is the criteria; the encoding that decides them against an observed run is authored at [_Implementation_](#implementation) and is downstream of this approval, not the object of it.

A criterion is a record of the service and the spec does not restate it. A restated criterion would let the spec and the criterion disagree, which is the reason an item names no project, and it would make ids stable and never reused a rule about nothing. The record is written exactly once, by the artifact store in the same call that submits the spec version introducing it, and never written again: withdrawal is recorded on the withdrawing spec version, so a version the gate rejects takes its withdrawal down with it. Which criteria are in force for a build is then a query — a criterion is in force unless a spec version withdrawing it belongs to an item in that build — and that is right for master and for the withdrawing candidate at once, where a field written at either moment is wrong for the other. A human confirming (6) reads what the version changes and not the accumulated set; what a service promises now is the set in force for its current release, and Factory reports how many are in force, how many were admitted as escapes, and how many have never failed, so drift is visible rather than re-read. The cost is that a set which drifted is never re-read unless an owner pins a human here, and that an item legitimately changing a behaviour has to withdraw the old criterion and remove its encoding in one item — the one edit that removes protection.

Criteria are predicates, not prose, for the reason a consumer's declaration is: what cannot be decided against an observed run is checked by nobody, and in the steady state that is most of them — the factory drafts its own criteria and a human reads them only where the score or a pin (9) puts one there. Each states one testable behaviour in one of a closed set of six sentence patterns, drawn from EARS.

| Pattern | The sentence it makes |
|---|---|
| **always true** | `the system shall <response>` |
| **event** | `When <trigger>, the system shall <response>` |
| **state** | `While <state>, the system shall <response>` |
| **unwanted condition** | `If <condition>, then the system shall <response>` |
| **optional feature** | `Where <feature is included>, the system shall <response>` |
| **state with an event inside it** | `While <state>, when <trigger>, the system shall <response>` |

Ids are stable and never reused, the same rule the number keeps, and a criterion that is dropped stays withdrawn rather than freeing its id. A sentence fitting no pattern is admitted with a tagged reason and counted, because a form everything can escape is not a form.

The form is the whole of what this provides, and having it makes one failure worse: pattern-perfect criteria can describe an incomplete set, and the incompleteness is now hidden behind a passing check. The unwanted conditions are where that happens — a set of only happy-path criteria is half a set, and nothing mechanical will ever ask for the other half.

Where the item has a user interface, the spec version introduces a screen's **state flow** — the states a screen can be in, the events that move it between them, and what each state forbids, empty and loading and failed among them. It is part of what a human confirms here (6) and not one of the six patterns: the patterns are sentences and lose the transitions, and it is the transitions that make [_Implementation_](#implementation)'s rejection mechanical. It is a record of the service like a criterion, written by the artifact store in the same call, and the screen's identity is the id of the flow that introduced it — per service, stable, never reused, the rule the criterion id and the number both keep. A revision is a new flow naming the one it supersedes, and the chain of supersessions is the screen; there is no screen record, because a screen with no flow is nothing the factory can check. The cost is that nothing mechanical stops two items introducing two chains for what a human would call one screen, and what catches that is the owner confirming here — the one place the flow's enforcement rests on a human noticing rather than on a link.

The drawback is the authoring, the same one declared meaning has in [_What a diff cannot see_](07-contracts.md#what-a-diff-cannot-see) — a sketch is cheaper and checks nothing. What the screen looks like is not here: that is the design system the project chose as a standing constraint (2), a screen design that arrived with the request as a per-item one, and, where neither answers, [_Implementation plan_](#implementation-plan).

### Implementation plan

How the item will be built, and the decisions no standing constraint (2) answers — what a screen looks like where the design system is silent is the common one. That is what Edit in place is for: the plan is a document, so a human who wants a different approach edits one into it rather than rejecting the item to get one. Reject is for a plan that is wrong, not for one that is merely not theirs.

### Tasks

A task is an internal step of one item and never a unit that ships: one item is one release, and a task has no build, no number, and no environment of its own. The factory authors them from the approved plan — the plan is how the item will be built, the tasks are that divided into work an agent picks up — and they are complete when the implementation is.

What the gate provides is a look at the breakdown before agents work on it, and Edit in place is where a human resequences or splits one without changing the plan above it. A task that cannot be finished escalates nothing by itself: the attempt bound is per stage, so what appears in Work (12) is the item.

### Implementation

Where the state machine the spec authored is enforced. An implementation that admits a forbidden transition is rejected here, mechanically rather than on taste — the whole reason for authoring the flow as a state machine. This is also the first gate that decides over a build: the build is made from the item's candidate branch when the stage finishes, so a build that does not compile is rejected here where Reject with feedback is an action, and the score computes the change factors from that build's diff against master. The drawback is that an item rejected here has already spent a build.

It is also the one artifact gate with no Edit in place, for the reason [_What a gate may change_](#what-a-gate-may-change) gives. The actions are the narrowest in the table: approve what was built, or send the item back up the pipeline to have it built differently.

The encoding of the acceptance criteria is authored here as well. A criterion is a predicate and something has to decide it against an observed run — unit tests are today's form of that — so the encoding is written with the code it checks and is reviewed at this gate as an artifact of the item. An artifact of the item is not always a record: the encoding is code in the build, picked out by the criterion id it names and derived from the build the way a consumer's declaration is, so it cannot outlive the criterion it was written for. It runs where there is a run to observe, on [_the candidate environment_](05-environments.md#what-the-candidate-environment-decides), and what it produces attaches to the build and is read at [_Merge to master_](#merge-to-master). This gate rejects in both directions: a criterion in force for the build with no encoding naming it, and an encoding naming a criterion that same build withdraws — otherwise the item that withdraws a criterion leaves its encoding in master, deciding a promise the service no longer makes.

Its authority comes from the gate rather than from the encoding being faithful, the same arrangement [_What a consumer declares_](07-contracts.md#what-a-consumer-declares) uses for an artifact the factory derived. The drawback is that an agent which misread a criterion can encode its misreading and pass what it wrote. What limits that is the criterion a human confirmed (6) one gate up: it is unchanged, it is still the thing being checked, and an encoding that departs from it is rejected here like any other artifact that does not do what it was authored from.

### Deploy to candidate environment

The dependency its hold waits on was declared at the cut, so what this gate is waiting for is mostly known before the item was ever built. Here the wait is for that dependency to become live at all, since the environment is composed from it. The same dependency is checked once more at the production deploy gate, where the question is whether it is live still. One condition here the cut could not have declared: a substrate with no room for another environment, which holds rather than rejects — a rejection would count an attempt and teach the score something the item did not do — and lifts when an item merges or is dropped and frees one. What that costs is an item stopped for a reason no artifact of it explains and no parameter of an owner's limits: the factory's own infrastructure ceiling, visible only as a wait.

Nothing else attaches to this row — no strategy, no rollout, no watch window — for the reason [_What the candidate environment decides_](05-environments.md#what-the-candidate-environment-decides) gives. What the deploy provides is the criteria.

### Merge to master

The release event — where a candidate becomes a numbered release — and also where the verdict on the candidate is given. Approving it admits the candidate to the merge queue, which re-verifies it against the master it will actually merge into and rejects it there on the same terms if that fails; rejecting it here sends the item back up the pipeline, and nothing waits on the environment it used, which is torn down with it. The verdict is a human's when the score or a pin puts one there and the factory's own otherwise, and a human deciding there is performing UAT (7). What either reads is the candidate's own run: the acceptance criteria decided against it, every consumer's declaration checked against it, and the producer's own contract diff — each rejecting on its own terms before anyone gives a verdict.

### Deploy to production

The default path's exception to reject, which is why that row does not offer it. By the time a change reaches it the merge has happened and the number is already assigned, so hold is the only way to stop it. Once it deploys, undoing it is veto after the fact — a rollback while its control is still running, a revert after, which is a new item.

Its hold also fires on the factory's own account, like the one a rollback leaves in place. A dependency declared at the cut that is not its service's [_current release_](06-releases.md#the-deploy-record) when this gate fires holds the deploy, and the check runs at the moment of firing — the same rule the two contract baselines keep — because a producer that was live when its consumer verified can be rolled back before that consumer deploys. Nothing is decided and no [_page_](08-operations.md#pages) fires; the hold lifts when the dependency is current again. A human can approve through it, as through every hold the factory sets, and what approving accepts is the break the hold was preventing.

The hold [_The reconciler_](08-operations.md#the-reconciler) sets is the other kind and the only one of it: what the factory recorded about this service is not what is running, so nothing here can be decided on the record, and no evidence the factory can gather lifts it. That one pages, for the reason the dependency hold does not — it waits on a human and on nothing else. Approving through is still offered, and here it is the human saying the record is wrong and the deploy should proceed anyway.

What it does not cover is the consumer already in production when its producer rolls back. A deploy gate stops deploys, and by then there is nothing left to stop — what catches that one is the consumer's own error rate raising an item, the same answer the factory gives to every consumer break it cannot see from the producer's side.

# Intent into items

## Intake

What arrives is an **intent**: a request from an owner (1), a bug or a complaint from an end user (4, 5), or something the factory found itself — a comparison still running after its window closed, a consumer breaking in its own error rate. One record, three doors. Nothing is judged on the way in, because judging it is what the interview is for.

The intent is what the graph joins on. Every item walks back to it, which is why one request producing four items in four services needs no noun of its own — the point [_Work that spans services_](07-contracts.md#work-that-spans-services) makes from the far end.

Constraints (2) arrive here too and are not intents: they are never cut into items and never ship. A standing one attaches to the factory and binds every item from then on; a per-item document arrives with one request and is spent with it. What the difference decides is what the factory reads when it drafts — every standing constraint in force, plus whatever rode in with this intent.

A design system is the standing constraint a project with a user interface always has, and it is chosen as much as supplied: the project takes one the factory carries or one an owner uploads. What the upload is decides what can ever be checked against it. As code — tokens and components with a build — it is diffable, and a screen departing from it fails mechanically. As a document — an export, a page of rules — there is no build, nothing checks the result, and what enforces it is a pin (9) putting a human at the gate. Nothing about that is particular to design: no standing constraint is checked mechanically unless it arrives in a form a machine can read, which is why the compliance officer holding (2) over a regulated area holds a pin with it.

So a designer's say survives past the gate they stood at unevenly. The screen's state flow confirmed at [_Spec_](03-gates.md#spec) (6) is authored as a machine and enforced at [_Implementation_](03-gates.md#implementation); what the screen looks like is checked by nobody unless the design system came as code. The cheap form costs a human at a pinned gate for as long as the pin stands; the enforceable one costs a designer producing code.

## The interview

The factory runs the interview and the factory ends it. It asks (3) until it has enough to cut the intent and author a spec per item that passes the criteria form — the six patterns [_Spec_](03-gates.md#spec) sets out — and what ends the questioning is having enough to author, not a human declaring the intent clear. An owner cannot know what the form will demand of an answer; the factory is the one holding the form.

Questions land in [_Work_](11-surfaces.md#work-ops-factory-people) against the intent that raised them, and are answered there. The spec is the artifact, so there is no interview gate and none is missing: approving the spec ratifies the refinement that produced it, which is what [_Where a gate sits, and what decides it_](03-gates.md#where-a-gate-sits-and-what-decides-it) already assumes.

The cost is that an owner cannot shorten it. An intent whose questions go unanswered does not move, and the end condition sitting with the factory is what makes that wait the owner's to end.

Two things go wrong with an interview, and only one of them is spend. An owner who stops answering spends nothing — no build, no environment, no agent is running — so no bound reaches that wait and none should: a bound would turn their silence into an escalation (12) they have to clear anyway, which is the human load the factory exists to remove. An interview that is answered and still never reaches enough to author is spend, round after round, and it carries [_the attempt bound_](03-gates.md#the-attempt-bound) like any other stretch of work the factory can fail at — the same parameter, authored with gate policy (8), counting rounds here rather than retries. Exceeding it stands in Work as an escalation (12), the factory saying it cannot refine this one; it still takes no gate, for the reason above. What that costs is a bound low enough to catch a circling interview cutting off one that was about to converge.

## The cut

One intent becomes one item or several — one per service the work lands in, three where a contract migration is what the work is. The cut is where [_No single item may break a contract_](07-contracts.md#no-single-item-may-break-a-contract) is applied rather than discovered: "an item that cannot ship by itself was cut wrong" is a statement about this stage, and until decomposition became one, nothing stood where the cut is made.

A service the work lands in may not exist yet, and nothing about the cut changes: one item creates it — its master branch, its place in the project's environments, and the code — and whatever calls it is a second item declared to wait on the first. The item ships by itself, a service nothing calls yet being exactly the kind of thing that can. What it costs is that the factory's weakest measurement lands on the release the rest of the work waits on: a first release is [_straight_](03-gates.md#the-rollout-strategy) whatever the score would prefer, nothing can close its window [_clean_](08-operations.md#the-watch-window), where no threshold is pinned (9) it is [_unmeasured altogether_](08-operations.md#the-health-signal), and no [_rollback target_](06-releases.md#rollback) stands under it. Each of those is priced where it arises, and this is where they arrive together.

Shipping by itself is a floor and not a size — it admits an item that touches forty files and one that changes a string. What sizes an item between them is a target the score supplies, which an owner authors or pins per area (9) with the rest of gate policy (8). It learns from what a bad size costs in each direction, and both costs are already on the [_Factory_](11-surfaces.md#work-ops-factory-people) readout: cut too coarse shows as attempt-bound escalations (12), with everything spent on the item thrown away, and cut too fine shows as cost per feature and rework rate — an environment, a spec, four gates, and a release number charged against a one-line change. The floors do not move with the target. It ships by itself, and no single item may break a contract, which is three items however small the target.

The cut also records the order. Where one item cannot be verified until another has shipped — the producing release of a migration — that dependency is declared here, and both deploy gates hold on it: [_Deploy to candidate environment_](03-gates.md#deploy-to-candidate-environment) until the dependency is live, [_Deploy to production_](03-gates.md#deploy-to-production) if it has stopped being. Ordering is a property of the cut, not something discovered at deploy time.

Decomposition is a stage with a gate of its own, scored like every other, and it fires where the cut yielded more than one item. What is approved is the set: how many items, where each lands, and what waits on what. A rejection re-cuts the set rather than sending one item back, because the unit standing at this gate is the cut and not an item. Edit in place is a human re-cutting by hand.

One verdict covers the whole cut, however many services it lands in. Fanning it out — one verdict per service, each holder deciding only the items landing in theirs — rebuilds here the wait [_Who owns a contract_](07-contracts.md#who-owns-a-contract) refused: approvals that must all arrive before anything moves, in a graph where a four-service cut is ordinary, and the attempt bound turning a long enough wait into escalations (12). A holder who wants to stand at the cut for their own service buys it with a pin (9) on their area, which is what a pin buys everywhere else. The cost is that they first meet the work one stage down, with authoring already paid for — never blind, since the item walks back to its intent and Work shows the sibling threads under the same decision, but reading a spec rather than a proposal.

The Spec gate cannot do this job — it is per item, so approving one item's spec never ratifies the decomposition that produced four. Without a gate here, a wrong cut surfaces several specs later and re-cutting means abandoning items that were already approved. So the gate fires where there is a set to ratify and nowhere else: a cut that yields one item produced no decomposition, and standing a stage in front of every intent — most of them single-item — buys a decision about nothing. Scoring it was the cheaper defence and it is the wrong one: a gate the score auto-passes is still a stage every builder implements and every intent walks through. What the condition costs is the cut that should have yielded four and yielded one, which now reaches a human one stage down as a spec too large, at [_Spec_](03-gates.md#spec) rather than here.

## A partial intent

An intent whose items did not all ship is a **partial intent**, and it is an outcome rather than a fault: one item hits the attempt bound and stands as an escalation (12), or a human vetoes it, while its siblings are already live. The shipped ones stand. Each was cut to ship by itself and is worth something by itself — and where it is not, the cut was wrong, which is what the gate above exists to catch. Undoing the whole thing is one revert item per shipped sibling, joined by the same intent, and never a single undo.

The declared order is what keeps that survivable. An item that dies takes only its dependents with it: whatever it depended on shipped before it, and whatever depended on it was still held at a deploy gate. So a partial intent is a feature half delivered, not a production half broken.

## Dispatch

With the cut approved, the factory puts its own agents onto the items — a model in a role with a scope, as [_One pipeline_](01-one-pipeline.md) has it. Dispatch gets no gate: it decides who authors, and everything authored stands at a gate anyway, where the score reads the authorship prior of the model that wrote it.

What bounds how many items move at once is infrastructure — an environment per candidate — and that cost is the factory's rather than a human's. Where a human takes the pen for a stage (11, 12), nothing about dispatch changes: authorship is an attribute of the stage, and the item is still one thread.

Factory is where an owner watches this: [_the fleet_](10-fleet.md), what each agent is doing, and how well.

# Intent into items

## Intake

What arrives is an **intent**: a request from an owner (1), a bug or a complaint from an end user (4, 5), or something the factory found itself — a comparison still running after its window closed, a consumer breaking in its own error rate. One record, three doors. Nothing is judged on the way in, because judging it is what the interview is for.

Constraints (2) arrive here too and are not intents: they are never cut into items and never ship. A standing one attaches to the factory and binds every item from then on; a per-item document arrives with one request and is spent with it. What the difference decides is what the factory reads when it drafts — every standing constraint in force, plus whatever rode in with this intent.

The intent is what the graph joins on. Every item walks back to it, which is why one request producing four items in four services needs no noun of its own — the point [_Work that spans services_](07-contracts.md#work-that-spans-services) makes from the far end.

## The interview

The factory runs the interview and the factory ends it. It asks (3) until it has enough to cut the intent and author a spec per item that passes the criteria form — the six patterns [_Spec_](03-gates.md#spec) sets out — and what ends the questioning is having enough to author, not a human declaring the intent clear. An owner cannot know what the form will demand of an answer; the factory is the one holding the form.

Questions land in Inbox and are answered there. The spec is the artifact, so there is no interview gate and none is missing: approving the spec ratifies the refinement that produced it, which is what [_Where a gate sits, and what decides it_](03-gates.md#where-a-gate-sits-and-what-decides-it) already assumes.

The cost is that an owner cannot shorten it. An intent whose questions go unanswered does not move, and the end condition sitting with the factory is what makes that wait the owner's to end.

## The cut

One intent becomes one item or several — one per service the work lands in, three where a contract migration is what the work is. The cut is where [_No single item may break a contract_](07-contracts.md#no-single-item-may-break-a-contract) is applied rather than discovered: "an item that cannot ship by itself was cut wrong" is a statement about this stage, and until decomposition became one, nothing stood where the cut is made.

The cut also records the order. Where one item cannot be verified until another has shipped — the producing release of a migration — that dependency is declared here, and both deploy gates hold on it: [_Deploy to candidate environment_](03-gates.md#deploy-to-candidate-environment) until the dependency is live, [_Deploy to production_](03-gates.md#deploy-to-production) if it has stopped being. Ordering is a property of the cut, not something discovered at deploy time.

Decomposition is a stage with a gate of its own, scored like every other. What is approved is the set: how many items, where each lands, and what waits on what. A rejection re-cuts the set rather than sending one item back, because the unit standing at this gate is the cut and not an item. Edit in place is a human re-cutting by hand.

The Spec gate cannot do this job — it is per item, so approving one item's spec never ratifies the decomposition that produced four. Without a gate here, a wrong cut surfaces several specs later and re-cutting means abandoning items that were already approved. The cost is a stage in front of every intent, including the single-item cut that most of them are, and it is carried the way every gate's cost is carried: [_Risk score_](04-risk-score.md) auto-passes what it judges low risk, here as everywhere.

## A partial intent

An intent whose items did not all ship is a **partial intent**, and it is an outcome rather than a fault: one item hits the attempt bound and stands as an escalation (12), or a human vetoes it, while its siblings are already live. The shipped ones stand. Each was cut to ship by itself and is worth something by itself — and where it is not, the cut was wrong, which is what the gate above exists to catch. Undoing the whole thing is one revert item per shipped sibling, joined by the same intent, and never a single undo.

The declared order is what keeps that survivable. An item that dies takes only its dependents with it: whatever it depended on shipped before it, and whatever depended on it was still held at a deploy gate. So a partial intent is a feature half delivered, not a production half broken.

## Dispatch

With the cut approved, the factory puts its own agents onto the items — a model in a role with a scope, as [_One pipeline_](01-one-pipeline.md) has it. Dispatch gets no gate: it decides who authors, and everything authored stands at a gate anyway, where the score reads the authorship prior of the model that wrote it.

What bounds how many items move at once is infrastructure — an environment per candidate — and that cost is the factory's rather than a human's. Where a human takes the pen for a stage (11, 12), nothing about dispatch changes: authorship is an attribute of the stage, and the item is still one thread.

Factory is where an owner watches this: the fleet, what each agent is doing, and how well.

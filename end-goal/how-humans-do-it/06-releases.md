# Releases

## One item per release

**One item per release. Always, at every stage, permanently.** The single thread of an item never splits: rollout stays item-scoped like everything before it, and a veto is the rollback of exactly one item rather than extracting one change from a set of ten. One exception, limited and named: master is linear, so where watch windows overlap a rollback undoes every release above its target, up to the K of [_Overlapping windows_](08-operations.md#overlapping-windows). What ships is still one item, and what one rollback undoes is at most K.

The cost is [an environment per item in progress](05-environments.md#an-environment-per-candidate), stated there. An environment added later follows the rule rather than changing it.

## The release record

A release is a record, and it is where the graph joins. It names what is known at the merge: the item that caused it, the build and commit it is made of, and the contract versions it publishes. The gate decisions that let it through were written before it existed and name the item and the build; every deploy of it to every environment is written afterwards and names the release. Ask anything about a shipped change — from what intent, on whose approval, under which policy, running where, rolled back when — and the answer is reached by traversing edges at the release record, outbound or inbound. Traceability is not added to this; it is what the record is for.

[_The merge queue_](05-environments.md#the-merge-queue) writes it and mints the number with it. Master's only inbound path is the queue, so the fast-forward is the event and the queue is what performs it, and the serialization that stops two merges taking one number is the per-service ordering the queue keeps already. A writer of its own, called at the fast-forward, would be a component with one caller and that ordering implemented again inside it. What this costs is release identity inside the queue's component, and no release record for a commit that reached master by another path — which nothing does, the queue's exclusivity being what makes that true.

It is written once and never written again, which is what keeps that one writer. The alternative was five — a gate component, the deploy component, and the rest writing their links into it — which is five writers on the record the whole graph joins on and a seam to declare between each pair, and writes to a release after it shipped. The cost of one is that where a release runs and which gates let it through are queries over the records that name it, so every reader asking either needs those inbound edges indexed.

## What a build is called, and when

| Name | What it is | When it applies |
|---|---|---|
| **candidate** | an item plus its build — identity enough to deploy, test, and reject | from build until merge to master |
| **release** | the name a build has on master | from merge to master onward |
| **contract version** | semver, one per published interface — a compatibility promise | moves only when that interface's promise moves |

A rejected candidate never needed a number, and the number is not a third row here: it is a field of the release, minted at the same event, and [_The number_](#the-number) is where it is set out. A build has one name at a time and the vocabulary stays at two, however many environments it runs on: a customer who defines five pre-production environments still has candidate and release, one build, and five deploy records — the names do not multiply with the environments.

## The number

The number is an ordinal, per service, assigned at merge to master. It orders builds and names rollback targets, and that is the whole of its job; compatibility is the contract's business, not the release's. Numbers are never reused. A release that is rolled back keeps its number, and the fix that follows takes the next one.

A numbered release that has never run anywhere is normal, not an anomaly. The number is minted at merge, one gate before production, so it records that a change was accepted — not that it is live. A hold at the production deploy gate produces exactly this. Where a release is running is a deploy record and never the number.

Master's only inbound path is [_The merge queue_](05-environments.md#the-merge-queue), and a candidate fast-forwards only after re-verifying against the master it will actually merge into. The commit that was verified is the commit on master. What was tested is what ships — a structural property of the queue, not a discipline anyone has to keep.

## The deploy record

A service's **current release** is the one its production deploy record names — what is running, not what is newest. Merged-and-never-deployed being normal is exactly what makes those different facts, and every cross-service check reads the current one: an environment composed from its dependencies is composed from what runs, and a promise is kept by what serves it.

A deploy record is written by the agent performing the deploy, through seam 4 of [_Deferred, but not designed out_](../deferred.md), when the deploy starts, and its status advances to complete or rolled back. One rollout produces one, and [_Rollback_](#rollback) is a deploy event and produces its own. What the record does not say is what share of traffic each build takes — that is the rollout's and its [_control_](08-operations.md#the-health-signal)'s, and a deploy log recording every shift would scale with the schedule for a fact only the rollout reads while it runs.

It is keyed by service and environment and not by target, so current release is single-valued per service. [_The reconciler_](08-operations.md#the-reconciler) still reads each production target and raises a mismatch naming the target that differs, which is the stronger check: a record per target would let three targets disagree with a fourth and call each of them right. The cost is that a deploy reaching three targets of four is not a state the record expresses — it is an incomplete deploy, caught as a mismatch rather than recorded as a partial one.

Current is the most recently completed deploy and not the most recently started, and the two disagree exactly while a rollout runs. Composing a dependent's [_candidate environment_](05-environments.md#an-environment-per-candidate) from the release still taking part of the traffic would verify it against a build that may roll back within the hour, and the dependency hold at [_Deploy to production_](03-gates.md#deploy-to-production) would admit a producer nothing is yet committed to. So while a rollout runs, the current release is what the control runs — the build the release is being compared against. What that costs is a dependent whose producer is mid-rollout waiting for it, which on a widening schedule is the length of the rollout.

## Rollback

Rollback is a deploy event, not a version event: it shifts traffic onto the control of the oldest open window and writes a deploy record, minting and retiring no number. What that control runs is the last release whose [_watch window_](08-operations.md#the-watch-window) closed without harm, which is not always the ordinal predecessor — where windows overlap, the release below a bad one may still be under watch itself. Where the rollout kept no control, the same target is reached the slow way, by redeploying the build. That the restored build still works is not luck: no item may break the store it runs on, and that rule applies in both directions — what the newer release wrote while it was live is still readable by the one being restored.

Undoing a release whose window has closed is not that. Its control is gone, master keeps the change, and the correction is a revert — a new item, its own thread, its own number. That is what the second phase of veto after the fact (10) costs; the first is the rollback above, on a human's judgment instead of the comparison's.

A service's first release has no target at all: nothing below it closed without harm, no control is running under it, and there is no earlier build to redeploy. Veto after the fact (10) begins at its second phase there, so the correction is a revert with no rollback before it. The drawback is that the cheapest undo the factory has is missing on the one release its score knows least about.

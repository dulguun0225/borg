# Releases

## One item per release

**One item per release. Always, at every stage, permanently.** The single thread of an item never forks: rollout stays item-scoped like everything before it, and a veto is the rollback of exactly one item rather than a surgical extraction from a bundle of ten. One exception, bounded and named: master is linear, so where watch windows overlap a rollback takes every release above its target with it, up to the K of [_Overlapping windows_](08-operations.md#overlapping-windows). What ships is still one item, and what one rollback undoes is at most K.

The cost is [an environment per item in flight](05-environments.md#an-environment-per-candidate), priced there. An environment added later inherits the rule rather than renegotiating it.

## The release record

A release is a record, and it is where the graph joins. It holds the item that caused it, the build and commit it is made of, the gate decisions that let it through, the contract versions it publishes, and every deploy of it to every environment. Ask anything about a shipped change — from what intent, on whose approval, under which policy, running where, rolled back when — and the answer is a walk out from the release record. Traceability is not bolted to this; it is what the record is for.

## What a build is called, and when

| Name | What it is | When it applies |
|---|---|---|
| **candidate** | an item plus its build — identity enough to deploy, test, and reject | from build until merge to master |
| **release** | the label a build wears on master | from merge to master onward |
| **contract version** | semver, one per published interface — a compatibility promise | moves only when that interface's promise moves |

A rejected candidate never needed a number, and the number is not a third row here: it is a field of the release, minted at the same event, and [_The number_](#the-number) is where it is set out. A build wears one label at a time and the vocabulary stays at two, however many places it stands: a customer who defines five pre-production environments still has candidate and release, one build, and five deploy records — maturity does not multiply with places to stand.

## The number

The number is an ordinal, per service, assigned at merge to master. It orders builds and names rollback targets, and that is the whole of its job; compatibility is the contract's business, not the release's. Numbers are never reused. A release that is rolled back keeps its number, and the fix that follows takes the next one.

A numbered release that has never run anywhere is normal, not an anomaly. The number is minted at merge, one gate before production, so it records that a change was accepted — not that it is live. A hold at the production deploy gate produces exactly this. Where a release is running is a deploy record and never the number.

A service's **current release** is the one its production deploy record names — what is running, not what is newest. Merged-and-never-deployed being normal is exactly what makes those different facts, and every cross-service check reads the current one: an environment composed from its dependencies stands on what runs, and a promise is kept by what serves it.

Master's only inbound path is [_The merge queue_](05-environments.md#the-merge-queue), and a candidate fast-forwards only after re-verifying against the master it will actually land on. The commit that was verified is the commit on master. What was tested is what ships — a structural property of the queue, not a discipline anyone has to keep.

## Rollback

Rollback is a deploy event, not a version event: it shifts traffic onto the control of the oldest open window and writes a deploy record, minting and retiring nothing. What that control runs is the last release whose [_watch window_](08-operations.md#the-watch-window) closed without harm, which is not always the ordinal predecessor — where windows overlap, the release below a bad one may still be under watch itself. Where the rollout paid for no control, the same target is reached the slow way, by putting the build back. That the build coming back still works is not luck: no item may break the store it stands on, and that rule runs in both directions — what the newer release wrote while it was live is still readable by the one coming back.

Undoing a release whose window has closed is not that. Its control is gone, master keeps the change, and the correction is a revert — a new item, its own thread, its own number. That is what the second phase of veto after the fact (10) costs; the first is the rollback above, on a human's say instead of the comparison's.

A service's first release has no target at all: nothing below it closed without harm, no control stands anywhere under it, and there is no earlier build to put back the slow way. Veto after the fact (10) begins at its second phase there, so the correction is a revert with no rollback standing in front of it. What that costs is that the cheapest undo the factory has is missing on the one release its score knows least about.

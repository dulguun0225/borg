# Releases

## One item per release

**One item per release. Always, at every stage, permanently.** The single thread of an item never forks: rollout stays item-scoped like everything before it, and a veto is the rollback of exactly one item rather than a surgical extraction from a bundle of ten. The cost is one trip through the UAT slot per item, taken serially within a service. Where the score or a pin puts a human in that slot, the service moves at one human UAT per item — a ceiling that is bought rather than structural. A dev/alpha channel added later inherits the rule rather than renegotiating it.

## The release record

A release is a record, and it is where the graph joins. It holds the item that caused it, the build and commit it is made of, the gate decisions that let it through, the contract versions it publishes, and every deploy of it to every environment. Ask anything about a shipped change — from what intent, on whose approval, under which policy, running where, rolled back when — and the answer is a walk out from the release record. Traceability is not bolted to this; it is what the record is for.

## What a build is called, and when

| Name | What it is | When it applies |
|---|---|---|
| **candidate** | an item plus its build — identity enough to deploy, test, and reject | from build until merge to master |
| **beta** | the label a build wears on the UAT branch | while it holds the UAT slot |
| **release** | the label a build wears on master | from merge to master onward |
| **number** | an ordinal, per service — orders builds, names rollback targets | minted at merge to master, never reused |
| **contract version** | semver, one per published interface — a compatibility promise | moves only when that interface's promise moves |

A rejected candidate never needed a number. A build wears one label and no more, however many places it stands: a customer who defines five pre-prod environments still has two labels and one build collecting five deploy records — maturity does not multiply with places to stand.

## The number

The number is an ordinal, per service, assigned at merge to master. It orders builds and names rollback targets, and that is the whole of its job; compatibility is the contract's business, not the release's. Numbers are never reused. A release that is rolled back keeps its number, and the fix that follows takes the next one.

A numbered release that has never run anywhere is normal, not an anomaly. The number is minted at merge, one gate before production, so it records that a change was accepted — not that it is live. A hold at the production deploy gate produces exactly this. Where a release is running is a deploy record and never the number.

Because master's only inbound path is the UAT branch, and the branch holds one item, master cannot move while an item is in UAT. The merge is therefore always a fast-forward and the commit that passed UAT is the commit on master. What was tested is what ships — a structural property of the slot, not a discipline anyone has to keep.

## Rollback

Rollback is a deploy event, not a version event: it puts the previous release build back on the environment and writes a deploy record, minting and retiring nothing. That the old build still runs is not luck: no item may break the store it stands on, and that rule runs in both directions — what the newer release wrote while it was live is still readable by the one coming back.

Undoing a release that has already shipped is not that. Master keeps it, and the correction is a revert — a new item, its own thread, its own number. That is what veto after the fact (10) actually costs.

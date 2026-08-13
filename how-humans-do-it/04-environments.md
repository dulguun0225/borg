# Environments

## Records, and two branches

Environments are records, not names in code. Each carries its own gate policy, strategy defaults, credentials, and history of deploys, incidents, and rollbacks. At least UAT and prod exist everywhere; customers define more per project.

Two long-lived branches back each service's promotion path: a UAT branch and master. Merging and deploying are separate events and so are separate gates — a merge admits a change to a branch, a deploy puts a branch on an environment, and either can happen without the other. A deploy can be rerun; a merge cannot be unmerged the same way.

## The UAT slot

The UAT branch is a slot, not a queue. It is reset to master, takes exactly one item, gets deployed and tested, merges, and resets. A second item that is ready waits. A reject empties the slot at once and the item rejoins the queue: a candidate being repaired must not hold the branch shut behind it.

The slot is per service, so a twelve-service project can have twelve items in UAT at once. That is only safe because no item may break a contract, and it settles what a candidate is tested against — the current releases of its dependencies, never another service's candidate. A UAT environment is composed for the candidate standing in it.

## What UAT decides

The graph is not uniform. Up to UAT, deploys are plain and what moves is a candidate. UAT is production-like, and it is where the candidate is tested (7). Passing UAT is where a candidate becomes releasable; merge to master is where it becomes a release and gets its number. Everything from there is machine: numbering, strategy selection, rollout, monitoring, rollback.

UAT is score-gated like every other gate, so it is not the last human touchpoint and there is no last one: the same score decides at each gate whether a human stands there, and a pin (9) puts one back. Where it auto-passes, the verdict on the candidate is the factory's own, taken on a production-like environment against acceptance criteria a human already confirmed (6).

The alternative was to split UAT by origin — permanent for human-originated features, auto-passable for factory-originated fixes — and that is the second, invisible path the pipeline forbids, sorted by a worse predictor than the score's own factors. Scoring it costs this: a change can reach production with nobody having watched it run. The watch window covers half of that and says so: it catches a change that misbehaves, never one that behaves perfectly and is the wrong thing. What catches a wrong thing built well is the criteria confirmed at the Spec gate, end users (4, 5), and veto after the fact (10) — and an owner who wants more buys it back per service or per area with a pin.

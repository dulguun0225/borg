# Environments

## Records, and one long-lived branch

Environments are records, not names in code. Each carries its own gate policy, strategy defaults, credentials, and history of deploys, incidents, and rollbacks. Production exists everywhere, every candidate gets one of its own, and customers define more per project.

One long-lived branch backs each service's promotion path: master. A candidate lives on a branch of its own until it merges or is dropped. Merging and deploying are separate events and so are separate gates — a merge admits a change to master, a deploy puts a build on an environment, and either can happen without the other. A deploy can be rerun; a merge cannot be unmerged the same way.

## An environment per candidate

Every candidate gets an environment of its own, stood up from master plus that candidate and torn down when the item merges or is dropped. Nothing is shared, so nothing queues behind anything: a candidate under repair holds nobody up, and a candidate waiting on a human holds up only itself.

The environment is composed for the candidate standing in it — the current releases of its dependencies, never another service's candidate. That is what makes a twelve-service project testing twelve items at once safe, and it is only safe because no item may break a contract.

The cost is infrastructure per item in flight rather than one shared environment per service. It is the factory's cost and not a human's, and it is the whole of what buys the absence of a queue.

## The merge queue

Candidates verified in parallel cannot all fast-forward, because each was built on a master that has since moved. So merging is a queue: a candidate entering it re-verifies against master plus every candidate ahead of it, and fast-forwards only if that passes. A failure invalidates the speculation behind it, and those candidates re-verify against the master that actually resulted.

The queue restores what one shared environment used to give for free — the commit that was verified is the commit that lands. It pays in compute on speculative runs a failure ahead throws away, which is the trade worth making: the shared environment charged the same serialization in human latency, and this charges it in machine time.

Its order is settable, like the queue at any gate. Reordering changes when a candidate re-verifies, never what it has to pass.

## What the candidate environment decides

The graph is not uniform. Up to the merge, what moves is a candidate and deploys are plain — no strategy, no rollout, and no watch window, because a candidate environment has no organic traffic and a comparison drawn from one human exercising a screen is noise in the shape of evidence. What the environment decides is the criteria: the candidate runs on production-like infrastructure against the current releases of its dependencies, and every consumer's declarations are checked against it there.

Merge to master is where a candidate becomes a release and gets its number. Everything from there is machine: numbering, strategy selection, rollout, monitoring, rollback.

The verdict is score-gated like every other gate, so it is not the last human touchpoint and there is no last one: the same score decides at each gate whether a human stands there, and a pin (9) puts one back. Where one does, what they are doing is UAT (7) — the environment is no longer named for it, because in the steady state nobody is standing there. Where it auto-passes, the verdict is the factory's own, taken against acceptance criteria a human already confirmed (6).

The alternative was to split that verdict by origin — permanent for human-originated features, auto-passable for factory-originated fixes — and that is the second, invisible path the pipeline forbids, sorted by a worse predictor than the score's own factors. Scoring it costs this: a change can reach production with nobody having watched it run. The watch window covers half of that and says so: it catches a change that misbehaves, never one that behaves perfectly and is the wrong thing. What catches a wrong thing built well is the criteria confirmed at the Spec gate, end users (4, 5), and veto after the fact (10) — and an owner who wants more buys it back per service or per area with a pin.

# Environments

## Records, and one long-lived branch

Environments are records, not names in code. Each has its own gate policy, strategy defaults, credentials, and history of deploys, incidents, and rollbacks. Production exists everywhere, every candidate gets one of its own, and customers define more per project.

An environment contains a project — every service in it — while a release, a number, a master branch, and a watch window are all per service. A deploy puts one service's build on one environment, so an environment fed by a twelve-service project has twelve independent promotion paths through it. The gate row a customer's environment adds describes that environment; what fires is one decision per deploy into it.

One long-lived branch is each service's promotion path: master. A candidate has a branch of its own until it merges or is dropped. Merging and deploying are separate events and so are separate gates — a merge admits a change to master, a deploy puts a build on an environment, and either can happen without the other. A deploy can be rerun; a merge cannot be unmerged the same way.

A candidate is not a record. It names an item and a build, and a build on a candidate branch belongs to one item, so the pair and the build pick out the same thing — a record per pair would repeat what the build and the item say already. The criteria results and the candidate deploy attach to the build. The branch and the environment are the item's and persist across a rebuild, the environment being a record already. What that costs is that [_The merge queue_](#the-merge-queue)'s rejection has no record of its own: it is written where a decision is written, with the queue as the actor, and counted as an attempt against [_the bound_](03-gates.md#the-attempt-bound).

## An environment per candidate

Every candidate gets an environment of its own, created from master plus that candidate and torn down when the item merges or is dropped. Nothing is shared, so nothing queues behind anything: a candidate being repaired delays nobody, and a candidate waiting on a human delays only itself. Nothing shares because nothing else is candidate-fed — the environments a customer defines are deploy targets for master, so no persistent slot exists for candidates to take turns on.

The environment is composed for the candidate running in it — the [_current releases_](06-releases.md#the-number) of its dependencies, what is running rather than what is newest, never another service's candidate. That is what makes a twelve-service project testing twelve items at once safe, and it is only safe because no item may break a contract.

The cost is infrastructure per item in progress rather than one shared environment per service. It is the factory's cost and not a human's, and it is the whole of what removes the queue: where the score or a pin puts a human on a candidate, that human's delay is that item's alone, where a single shared environment would have imposed it on every item behind them.

## The merge queue

Candidates verified in parallel cannot all fast-forward, because each was built on a master that has since moved. So merging is a queue: a candidate entering it re-verifies against master plus every candidate ahead of it, and fast-forwards only if that passes. A failure invalidates the speculation behind it, and those candidates re-verify against the master that actually resulted.

A candidate that fails its own re-verification — against the master that actually resulted, not against a speculation ahead of it — failed on its merits, and the queue rejects it: the item goes back up the pipeline, counts an attempt against [_the bound_](03-gates.md#the-attempt-bound), and the score learns from it the way it learns from a reject and not from a hold. That is the verdict the merge gate can no longer give, having already approved, and without it a candidate failing here would have no path at all. Candidates behind a failure count nothing, for the same reason they re-verify at no charge to themselves: they failed because of someone else.

The queue restores what one shared environment used to provide — the commit that was verified is the commit that merges. It costs compute on speculative runs that a failure ahead of them throws away, which is the better exchange: the shared environment imposed the same serialization on human time, and this imposes it on machine time.

Its order is settable, like the queue at any gate. Reordering changes when a candidate re-verifies, never what it has to pass.

## What the candidate environment decides

The graph is not uniform. Up to the merge, what moves is a candidate and deploys are plain — no strategy, no rollout, and no [_watch window_](08-operations.md#the-watch-window), because a candidate environment has no organic traffic and a comparison computed from one human exercising a screen is noise that looks like evidence. What the environment decides is the criteria: the candidate runs on production-like infrastructure against the current releases of its dependencies, and every consumer's declarations are checked against it there. Deciding them is running the encoding [_Implementation_](03-gates.md#implementation) authored beside the code, and a criterion that fails is a rejection at the merge gate on the same terms a consumer's declaration failing there is.

Merge to master is where a candidate becomes a release and gets its number. Everything from there is machine: numbering, strategy selection, rollout, monitoring, rollback.

The verdict is score-gated like every other gate, so it is not the last place a human decides and there is no last one: the same score decides at each gate whether a human decides there, and a pin (9) puts one back. Where one is there, what they are doing is UAT (7) — the environment is no longer named for it, because in the steady state nobody is there. Where it auto-passes, the verdict is the factory's own, taken against acceptance criteria a human already confirmed (6).

The alternative was to separate that verdict by origin — permanent for human-originated features, auto-passable for factory-originated fixes — and that is the second, invisible path the pipeline forbids, separated by a worse predictor than the score's own factors. Scoring it costs this: a change can reach production with nobody having watched it run. The watch window covers half of that and says so: it catches a change that misbehaves, never one that behaves perfectly and is the wrong thing. What catches a wrong thing built well is the criteria confirmed at the Spec gate, end users (4, 5), and veto after the fact (10) — and an owner who wants more adds it back per service or per area with a pin.

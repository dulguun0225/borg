# Operations

## The health signal

A deploy record says which release runs where, so the factory always knows what it is looking at. It watches that release against the one it replaced — error rate, latency, throughput, on comparable traffic — and that comparison is the health signal. Nothing has to be authored for a new service to have one.

A rollout fails when the comparison crosses the line, and the rollback follows on its own. That is not the canary's alone — the comparison runs after every deploy, and the strategy decides only how the factory acts on it: a canary sheds its traffic back, an A/B split collapses to the incumbent, blue-green cuts to the old side, a straight deploy puts the previous build on. The baseline is only as good as the release it is drawn from, which is the case for pinning: an owner can pin explicit thresholds for a service the way they pin a gate or a strategy, and a service whose normal behaviour is already bad is where that earns its keep.

## The watch window

The **watch window** is how long the factory may act on the comparison alone. Inside it, crossing the line rolls the release back with no human in the loop; outside it, the same crossing raises an item. It is not the veto window: the watch window is the factory's own authority and is bounded by evidence, the veto window is a human's and is bounded by reversibility. A rollback deploy opens no window of its own: the build coming back was the baseline, and a fresh window would compare it against the release just condemned and put that one back.

The window is a volume condition, not a clock. It closes when the comparison has seen enough comparable traffic to detect a regression of the size worth catching — minutes on a busy path, hours on a quiet one. That size is the parameter, and the duration is only what it costs to reach: an owner authors it with the rest of gate policy (8) and pins it per service the way they pin explicit thresholds, and where they author nothing the score supplies it. Without a stated effect size, "long enough" is a judgment call where the rest of the factory is mechanical.

A cap bounds it, authored the same way, and a service too quiet to reach that much traffic ends at the cap every time. There the window is thin protection and reads as thin: a service called twice a night is not made safe by watching it until lunchtime. The sample the score holds out takes the cap rather than the window its own traffic would close — auto-passing a change the score wanted gated is where the factory is most openly guessing, so it buys the longest watch there is, which on a quiet service is not much.

The window reads the producer's own signal and nothing else. A consumer that breaks does so in its own error rate, on its own schedule, and what surfaces it is that service's own comparison raising an item — after the producer's window closed, with nothing rolling back on its own. Reading a sibling's signal would need every consumer's calls recorded and attributed across services, which is the recorded-usage build [_What a diff cannot see_](06-contracts.md#what-a-diff-cannot-see) declines; the window inherits that refusal rather than reopening it. The item is what stands in for it, alongside the contract rules that make the schema half mechanical, the producer's own per-field observables, and veto after the fact (10) while the change is still undoable.

The parameter is learned; the duration is derived and never set. How fast a problem would surface is already what the score reads to pick a strategy, and the same evidence sets the effect size wherever an owner has not. Only a rollback or veto traceable to the health signal counts as that evidence — a change vetoed for being the wrong feature says nothing about what the comparison should have caught, and feeding those back would shrink the effect size for owners who changed their minds and lengthen every window with it. The score learns from the signal it could have seen, the same way it learns from a reject and not from a hold.

## What an open window holds

An open window holds that service's production deploys and nothing else. The next item still builds, still takes the UAT slot, still merges, and still mints its number. Only the deploy waits, as a hold for a window.

Two live releases of one service would make two definitions ambiguous at once. Rollback puts the previous release build back and there is exactly one of those; the comparison watches a release against the one it replaced, and a second deploy mid-window makes the first the second's baseline — so a regression the first introduced is absorbed into the baseline and never surfaces again. The hold keeps both single-valued. The cost is throughput, and it lands hardest on the fully auto-passed, high-frequency service, whose UAT cycle is minutes while its window is not; where a pin puts a human in the UAT slot, the hold rarely bites at all.

A rollback holds it longer. Master keeps the change that was rolled back and the next item was built on master, so shipping it would redeliver the defect just pulled. The hold stands until the revert item ships, and does not hold the revert — a dependency hold that caught its own dependency would never lift. Without it the mechanism lets go exactly when it is needed.

A human can approve through either hold. Both are the factory's own, and the emergency lever is approve now, not skip; the production deploy gate offers Approve regardless of what placed the hold. Through a window hold, what is bought is the deploy and what is paid is the baseline. Through a rollback hold, what is paid is the defect that was just pulled — the more expensive of the two, and the one more likely to be reached for mid-incident.

## After the window

The comparison keeps running after the window closes. What it finds then is not a rollback candidate — the change has been live for a week and the window's authority is long spent. It is an unrefined item in Work, the same shape as an end-user complaint (4, 5), taking the same stages and the same gates. That is the whole of "finds issues and fixes bugs": detection writes an item, and the pipeline does the rest.

## Incidents

An incident is a record on the environment. It points at the deploy, the deploy at the release, the release at its item and its intent — so what caused an incident is a walk out of it, the same walk the release record answers from the other end.

The factory works the item it raised under the attempt bound like any other. Hitting that bound turns it into an escalation (12), the same Inbox row a stuck feature produces: a bug the factory cannot fix is not a different kind of stuck.

# Operations

## The health signal

A deploy record says which release runs where, so the factory always knows what it is looking at. It watches that release against a **control** — an instance set running the build already in production, brought up alongside the release and taking comparable traffic — and that comparison is the health signal. Nothing has to be authored for a new service to have one.

The control is built rather than found, because a new process and a long-lived one differ before either serves a request: cold caches, empty pools, a compiler that has not yet seen the workload. Measured against the instances it is replacing, a release pays for being new, and the factory reads that bill as a regression. Measured against a control of its own age, what is left is the change. **Baseline** stays the name for what the comparison is drawn against; what moved is that the factory now stands one up instead of pointing at what is already running.

A control belongs to the rollout and is not a deploy of the release it runs. It takes traffic to be measured, it mints nothing, and it goes when the window closes.

The signal is production's alone, and the reason is arithmetic rather than policy: it is drawn from organic traffic, and a candidate environment has none. A comparison fed by one human exercising a screen is noise in the shape of evidence, and standing a control beside it would double the cost of learning nothing. What a candidate environment decides is the criteria, which want a test run rather than a population.

A rollout fails when the comparison crosses the boundary, and the rollback follows on its own. That is not the canary's alone — the comparison runs after every production deploy, and the strategy decides only how the factory acts on it: a canary sheds its traffic back, an A/B split collapses to the incumbent, blue-green cuts to the old side, and a straight deploy, having nothing standing to cut to, has to put a build back and wait for it.

A control costs the old build still serving, which is precisely what a straight deploy refuses. So the real comparison comes with canary, A/B, and blue-green, and a straight deploy falls back to reading the release against its predecessor's own recent history — the confound above, unanswered, which is what makes that fallback the weak one. This is the second thing the score buys when it picks a strategy: the cheapest rollout is also the one that sees least, and where the score wants to see, it picks a shape that pays for a control.

The baseline is only as good as the build it runs, which is the case for pinning: an owner can pin explicit thresholds for a service the way they pin a gate or a strategy, and a service whose normal behaviour is already bad is where that earns its keep. An explicit threshold is absolute where the comparison is relative, and it stands beside the comparison rather than instead of it — the release clears both or neither, so a pin here can only add, like every other.

## The watch window

The **watch window** is how long the factory may act on the comparison alone. Inside it, crossing the boundary rolls the release back with no human in the loop; outside it, the same crossing raises an item. It is not [_The veto window_](#the-veto-window): the watch window is the factory's own authority and is bounded by evidence, the veto window is a human's and is bounded by reversibility. A rollback deploy opens no window of its own: the build coming back was the control, and a fresh window would compare it against the release just condemned and put that one back.

The window is a volume condition, not a clock — and not a volume computed in advance either. The comparison is evaluated as traffic arrives, against a boundary that holds at every point it is read. That much is forced by what the factory already does: a threshold set for one look at a fixed sample is not the threshold for a thousand looks at a growing one, and reading a fixed-horizon test continuously is how a factory rolls back healthy releases all day. The boundary is what makes continuous reading legitimate rather than a fault nobody attributes.

| Exit | What closes the window | What follows |
|---|---|---|
| **harm** | the comparison crosses the boundary against the release | rolled back, no human in the loop |
| **clean** | the comparison rules out a regression of the size worth catching | closed early, on evidence |
| **cap** | neither, by the cap | closed unresolved |

What an owner authors is that size and the confidence required, with the rest of gate policy (8), pinned per service the way explicit thresholds are (9); where they author nothing the score supplies both. A pinned size is a floor on protection — the score may ask to catch something smaller, never something coarser, so a pin can only add, the way a pinned gate can only add a human.

The duration is discovered and never set, and that is where the throughput is: a release that is plainly fine says so early and goes. It buys that by being fine rather than by being watched against a coarser boundary, which is the trade not to make — what a comparison needs scales as the inverse square of what it must detect, so a tenfold cut in the window costs a boundary three times coarser. Speed bought that way runs out fast. Speed bought from evidence does not.

A boundary is not arguable the way a vector of factors is, and the score's own rule is that one nobody can argue with is one nobody will trust. What an owner argues with is the size and the confidence, both authored and both pinnable; the boundary is arithmetic on those. That is the score's own shape — a published formula over inputs a human can disagree with.

A cap bounds it in elapsed time, authored the same way. The condition is a volume one, but what ends a window that will never reach its volume cannot be — so a service too quiet to take that much traffic ends at the cap every time. There the window is thin protection and reads as thin: a service called twice a night is not made safe by watching it until lunchtime. The sample the score holds out runs to the cap rather than stopping where the boundary would let it — auto-passing a change the score wanted gated is where the factory is most openly guessing, so it buys the longest watch there is, which on a quiet service is not much.

The window reads the producer's own signal and nothing else. A consumer that breaks does so in its own error rate, on its own schedule, and what surfaces it is that service's own comparison raising an item — after the producer's window closed, with nothing rolling back on its own. Reading a sibling's signal would need every consumer's calls recorded and attributed across services, which is the recorded-usage build [_What a diff cannot see_](07-contracts.md#what-a-diff-cannot-see) declines; the window inherits that refusal rather than reopening it. The item is what stands in for it, alongside the contract rules that make the schema half mechanical, the producer's own per-field observables, and veto after the fact (10) while the change is still undoable.

The parameters are learned. How fast a problem would surface is already what the score reads to pick a strategy, and the same evidence sets the size wherever an owner has not. Only a rollback or veto traceable to the health signal counts as that evidence — a change vetoed for being the wrong feature says nothing about what the comparison should have caught, and feeding those back would shrink the size for owners who changed their minds and lengthen every window with it. The score learns from the signal it could have seen, the same way it learns from a reject and not from a hold.

## Overlapping windows

An open window holds nothing. The next item builds, verifies on its own environment, merges, mints its number, and deploys while the window before it is still open — up to **K** of them at once per service. K is authored with the rest of gate policy (8), and where an owner authors nothing the score supplies it, the same division the window's own size and confidence run on. A pinned K is a ceiling on blast radius — the score may ask for fewer open windows, never more — so a pin can only add safety here too. The cost of authoring it arrives late: the number is silent until the first rollback, where it is the size of the bundle.

What the score supplies starts at 1 and earns its way up, because the evidence arrives one-sided — a service that has never rolled back has seen what K buys and nothing of what it costs. Windows closing without harm raise it; a rollback that took more than its target with it lowers it. That is the authorship prior's shape, a position held where there is no evidence and moved by what arrives, and it asks nothing of the score it does not already do. What it costs is that a service which rarely rolls back climbs slowly and holds throughput it could have had, and the first rollback after a climb is where the climb is paid for.

That is what a built control buys. A found baseline made concurrency incoherent: with the comparison drawn against the release it replaced, a second deploy mid-window made the first the second's baseline, so a regression the first introduced was absorbed into the ground it was measured from and never surfaced again. Serializing was the only way to keep that single-valued. A control is stood up per release and never moves, so two open windows are two independent comparisons and neither is the other's ground.

What a rollback aims at has to move with it. The previous release is not single-valued while K windows are open, so the recovery target is the last release whose window closed without harm, carried on the release record. Closing at the cap counts: a release that was never condemned is one the factory can go back to, and holding out for a clean close would leave a service too quiet to ever reach one with nowhere to go.

Rollback is then a shift onto the control of the oldest open window, which is running that target already — up, warm, and the instances a comparison was being drawn against. No control has to outlive its own window for that to hold: each one runs the release below it, so the fallback is always carried by the window above the one that closed. Where the rollout was a straight deploy there is nothing to shift onto, and the rollback is the slow one — a build put back and waited for.

K is paid for in rollback granularity. Master is linear, so the release above a bad one contains it: a rollback inside overlapping windows undoes every release above its target, up to K of them. That is the one place the factory ships a bundle, and it is bounded rather than absent — [_One item per release_](06-releases.md#one-item-per-release) still holds for what ships, and for what a veto undoes once the window has closed. K = 1 is the serial factory, and every increment above it buys throughput on the high-frequency service and pays in how much one rollback takes with it.

A rollback still holds, where an open window no longer does. Master keeps the change that was rolled back and the next item was built on master, so shipping it would redeliver the defect just pulled. The hold stands until the revert item ships, and does not hold the revert — a dependency hold that caught its own dependency would never lift. Without it the mechanism lets go exactly when it is needed.

A human can approve through it. The hold is the factory's own and the emergency lever is approve now, not skip; the production deploy gate offers Approve regardless. What is paid is the defect that was just pulled — the most expensive thing in the factory to approve through, and the one most likely to be reached for mid-incident.

## The veto window

Veto after the fact (10) is a human undoing a shipped change on judgment, where the watch window is the factory undoing one on the comparison. The two overlap in time and differ in what bounds them, which is why they are two windows and not one.

A veto has two phases, and what separates them is whether anything is still standing to go back to. While a control is up it is a rollback — the same traffic shift the factory would perform, on a human's say instead of the comparison's, taking up to K releases with it for the reason [_Overlapping windows_](#overlapping-windows) gives. Once the window closes the control goes with it, and the remedy is a revert: a new item, its own thread, its own number, and the whole pipeline to pay for it.

That is what bounds the veto window, and it is how reversibility bounds it. The score reads reversibility to pick a strategy, the strategy decides whether a control is paid for, and the control is what leaves a human something to reach for. So the lever exists where the strategy bought one: a rollout with a control has both phases, a straight deploy has only the revert. The window decays by no rule of its own; it ends where the thing it acts on ends.

## After the watch window

The comparison keeps running after the window closes. What it finds then is not a rollback candidate — the change has been live for a week and the window's authority is long spent. It is an unrefined intent in Work, the same shape as an end-user complaint (4, 5), taking the same stages and the same gates. That is the whole of "finds issues and fixes bugs": detection writes an intent, and the pipeline does the rest.

## Incidents

An incident is a record on the environment. It points at the deploy, the deploy at the release, the release at its item and its intent — so what caused an incident is a walk out of it, the same walk the release record answers from the other end.

The factory works the item it raised under the attempt bound like any other. Hitting that bound turns it into an escalation (12), the same Inbox row a stuck feature produces: a bug the factory cannot fix is not a different kind of stuck.

## Pages

Mail and chat carry everything waiting on a human out of the product. A **page** is the narrow channel: what earns one is a wait where the deployed software is worse until a human ends it, and nothing else fires one.

That bar is already the document's, and two refusals above fall out of it rather than being added to it. A declared dependency that is not its service's current release holds the production deploy, nothing is decided, and the hold lifts itself when the dependency is current again — there is no wait for a human to end. An owner's silence at [_The interview_](02-intent-into-items.md#the-interview) spends nothing, which is why no bound reaches that wait; a page would be the same mistake made louder.

Three things earn one. An escalation (12) on an item the factory raised from an incident — the factory saying it cannot fix a defect that is live. A human standing at a gate on a revert while the rollback that pulled the defect still holds: the service is running the build that came back, master still carries the defect, and nothing ships past that human. And a mismatch [_The reconciler_](#the-reconciler) found, which holds that service's production deploys until a human clears it. In all three, production is worse than it should be and no mechanism the factory has will improve it.

A rollback the factory performed on its own is reported, not requested, and reporting is not paging — **the factory does not page to inform.** Nothing else waiting on a human clears the bar either: a UAT assignment (7) costs its own item's environment and its own item's latency, an escalation on a feature item has nothing live to be worse, a deploy behind its service's K open windows is waiting on the factory, and a [_fleet entry with no credential to reach_](10-fleet.md#an-account-that-runs-out-is-a-hold) stops work rather than making anything live worse — the most page-tempting row in the factory and still not one. Those are Inbox rows, and mail or chat is what carries them out.

A page has no duty of its own — inventing a thirteenth would be a mechanism where routing already exists. It reaches whoever holds the duty the row belongs to, the same routing that put the row in their Inbox.

A duty may be held by more than one human, and a **rotation** is which of them a page reaches now. It is declared in People with the rest of who holds what, and enforced by nothing. Unanswered, a page widens — every holder of the duty, then the owner — so a stale rotation costs a delay and never a lost page. What that buys is paid for in the other direction: a page nobody answers becomes a page everybody gets.

An unanswered page never makes the factory act alone, because what it is paging out of is stable. The service runs the last release whose window closed without harm, not the one just condemned, so what the wait holds up is the fix and never the recovery.

A human may fire one on their own say, from Ops, which is the parallel veto after the fact (10) already has: the rollback the factory would perform, on a human's judgment instead of the comparison's. Nothing scores it and no bound reaches it. What holds it honest is that a page nobody needed makes its recipient slower to the next one.

A page is an event on the row it belongs to — who was reached, when it widened, who answered — each carrying an actor, which is seam 1 of [_Deferred, but not designed out_](../deferred.md) and nothing new. None of it is authored: the rotation is a People declaration and the widening is the factory's own, so gate policy stays at the eight rows it has.

## The reconciler

Every check above reads a record the factory wrote. A service's [_current release_](06-releases.md#the-number) is what its production deploy record names, the health signal is drawn against a control the factory stood up and recorded, and an incident points at a deploy the same log describes. So a factory whose records are wrong reports itself healthy, and nothing downstream of them contradicts it.

The **reconciler** is one process outside the pipeline that reads what is actually running on each production target and holds it against that service's current release. One fact, one comparison, read-only — no deploy privilege, and it writes nothing the factory reads back. The owner stands it up beside the factory they already host, because a reconciler the factory deployed would sit inside the trust domain it exists to check.

What it catches is the record that was wrong when it was written — a deploy that did not take, an agent that recorded one that did not, a target drifted underneath. That is the common failure and it is a bug rather than malice. What it does not catch is a record rewritten afterwards; anchoring the log's chain head is what would, and seam 2 of [_Deferred, but not designed out_](../deferred.md) holds that open for attribution rather than for this.

A mismatch holds that service's production deploys and [_pages_](#pages). The factory does nothing further, because every remedy it has reads the record in question — a rollback aims at a target carried on the release record, and aiming one with a record that just proved wrong repeats the fault with more traffic behind it. That hold does not lift itself, which is what makes the page honest rather than an alert: the service is cut off from its own fixes until a human ends it.

The cost is a second thing to run, and the cheaper candidates were declined rather than the reconciler being free. Anchoring the chain head is nearly free in the record, but leaves the owner holding a head the factory cannot reach for as long as the product runs, and a custody that never ends is a duty. Installing the reconciler is done once and asks nothing after, so it stays outside the twelve the way hosting the factory does. Target-side attestation on seam 4 spends that seam's "however it is implemented" early and lands the same custody duty anyway.

What the reconciler is not is a second opinion on whether the software is good. It compares one recorded fact to the world; the comparison, the score, and the criteria all stay where they are. One that grew a judgment of its own would be the second path [_One pipeline_](01-one-pipeline.md) refuses, arriving through the back.

# Gate policy

Everything an owner authors (8), in one place. Each parameter is defined beside the mechanism it bounds, sections apart, which is right for a reader meeting the mechanism and wrong for an owner setting the policy — the duty is one line and reads far smaller than it is. Gathered, it is eight rows.

## What it holds

| Parameter | What it bounds | The cost at each end |
|---|---|---|
| **[risk threshold](04-risk-score.md)** | where the score stops auto-passing and puts a human at the gate | gate more and the human load the factory exists to remove comes back; gate less and a change reaches production with nobody having watched it run |
| **[attempt bound](03-gates.md#the-attempt-bound)** | how many times a stage is retried before the item stands in Inbox as an escalation (12) | low turns solvable work into human work; high burns spend before anyone sees the item |
| **[the interview's round bound](02-intent-into-items.md#the-interview)** | how many rounds of questions before the factory says it cannot refine this intent | low cuts off an interview that was about to converge; high spends round after round on one that never will |
| **[item-size target](02-intent-into-items.md#the-cut)** | how large an item is meant to be, above the floor that it ships by itself | too coarse shows as attempt-bound escalations (12), with everything spent on the item thrown away; too fine shows as cost per feature and rework rate |
| **[the predicate catalog](07-contracts.md#what-a-consumer-declares)** | what kinds of assertion a consumer's declaration may draw from | narrow leaves assumptions undeclared, surfacing only once a producer has broken one; wide admits an assertion that cannot be decided against one observed exchange, and enforcement stops being mechanical |
| **[the watch window's size and confidence](08-operations.md#the-watch-window)** | the smallest regression the comparison must rule out to close clean, and how sure it must be | coarser sees less, and what a comparison needs scales as the inverse square of what it must detect; finer holds every release under watch longer |
| **[the watch window's cap](08-operations.md#the-watch-window)** | the elapsed time that ends a window which will never reach its volume | short closes more windows unresolved; long holds the next deploy behind a window a quiet service was never going to resolve |
| **[K](08-operations.md#overlapping-windows)** | how many watch windows one service may hold open at once | 1 is the serial factory; each increment buys throughput on a high-frequency service and pays in how much one rollback takes with it |

## One shape across all of them

**The score supplies what an owner does not.** Authoring is an override rather than a requirement: a factory with nothing authored in it runs, on values the score sets and moves as outcomes arrive. What that costs is that a default nobody chose is still a decision, and it can stay invisible until it is spent — K is silent until the first rollback, where it is the size of the bundle.

**A pin (9) can only add.** The direction differs per parameter and points the same way in each: a floor under the window's size, a ceiling over K, a human at a gate, an explicit health threshold standing beside the comparison rather than instead of it. The score keeps moving inside a pin and never past it, so an owner who pins one value has not frozen the rest.

**Scope follows the mechanism, not the duty.** The attempt bound is per stage, K and the watch window's parameters are per service, gate thresholds ride on the environment record beside that environment's strategy defaults and credentials, the item-size target is per area, and the predicate catalog is one list the factory owns and an owner extends. A pin (9) is what narrows any of them further, to a stage, a project, or an area.

**The duty does not shrink, and its reach grows.** The backstop duties (11, 12) fall away as the factory improves. This one does not, and what it governs widens as they go: these eight rows are what the factory may do with no human standing anywhere, so the better it gets, the more of its behaviour they describe.

## What is not in it

Three things sit next to gate policy and are not it. **The score.** An owner authors what the number is compared against, never how it is computed — the formula is published so a human can disagree with it, and the vector behind it is learned from outcomes. **The stages.** A gate is where policy lands; what the gates sit between is the factory's own. **The form a criterion takes.** The six patterns are a closed set and the factory holds the form, which is what lets it end the interview — an owner confirms criteria (6) and does not author the shape one may have.

A strategy default rides on the environment record beside gate policy, and pinning a strategy is (9). Neither is authored here.

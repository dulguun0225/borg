# The rollout strategy

A **rollout strategy** is how a release takes live traffic from the build it replaces. It attaches to a production deploy and to no other, which is narrower than [master](../05-environments/01-records-and-one-long-lived-branch.md)-fed: what a strategy decides is whether a control runs, a control is the instances a comparison is made against, and that comparison exists only in production because **organic traffic** — the requests real users are already making — does. A customer's master-fed pre-production environment has no more of it than a [candidate](../06-releases/03-what-a-build-is-called-and-when.md) one, so a strategy there would decide nothing that anything reads — the same division [_The health monitor_](../08-operations/01-the-health-monitor.md) already draws.

| Strategy | How the release takes traffic |
|---|---|
| **with a control** | on a schedule, with the build it replaces serving the rest throughout — a share widened as the comparison stays clear, a share kept fixed while the two are compared, or all of it at once switched to a second complete copy running beside the one it replaces |
| **without a control** | all of it, in place, with none of the build it replaces left running |

Two rows and not four, because they differ on one axis and everything downstream reads that axis alone: whether the build being replaced is still serving while the release does. That is what a [_control_](../08-operations/01-the-health-monitor.md) requires, and what a human undoing a change after it shipped (10) uses while it runs — the other row has neither. The schedule is an attribute of the first row rather than a strategy of its own. Canary, A/B, and blue-green are the three an engineer arrives already knowing, and they are that row at three schedules. They differ in how much traffic the release is exposed to and not in what is provisioned: on every schedule the build being replaced keeps the instances it had until the [window](../08-operations/03-overlapping-windows.md) that could return to it closes, because those instances are what a rollback shifts production onto and a share is not a capacity. The drawback of folding them in: a name an engineer brings is now a value on a field instead of a row to implement.

Both rows replace instances, and neither drops a request doing it: the operation among the named ones of seam 4 of [_Deferred, but not designed out_](../../deferred.md) that replaces an instance stops new requests reaching it and lets the ones it holds finish before it ends, and the [deployer](../08-operations/01-the-health-monitor.md) advances a target only when that operation reports it done. That is the operation's contract and not a strategy, which is why it is not a third row — it holds on the row without a control, where all of production turns over in place, exactly as on the row with one. What it costs is a replacement taking as long as the longest request it waits on, and a platform unable to hold a request open across the replacement performing a cut the factory records as a drain. What neither row offers is a switch inside the build — code shipped dark and turned on later. A change turned on by a flag is a change that took no gate, reached no environment as the thing it is, and opened no [window](../08-operations/02-the-analysis-window.md) when it started serving, which is the second path [_One pipeline_](../01-one-pipeline.md) refuses; so the only correction after a window closes is a pipeline pass, behind [a queue whose order is not settable](../05-environments/03-the-merge-queue.md) after the merge. That cost is stated plainly: an urgent fix waits its turn, and what shortens the turn is the size of the items ahead of it, not anything an owner can do at the moment it is needed.

A [configuration value](../05-environments/01-records-and-one-long-lived-branch.md) is a repository file, so takes that path.

How much of production a release may take before the comparison has cleared any of it is
bounded by the [hazard
severity](../02-intent-into-items/03-decomposition/03-hazard-severity.md) of the item's
area, which is the second thing deciding a schedule. At `irreversible` the score may pick
only the schedule that widens as the comparison stays clear. The one that switches all of
production at once is not available and neither is a fixed share, both of them moving the
rest of the traffic in one step rather than as a reading clears it. Otherwise the [window's
cap](../08-operations/02-the-analysis-window.md) is the only thing bounding how much traffic
is served before anybody reads a result, and on a quiet service the cap is hours. Where the
platform serves no share there is no schedule to pick, every deploy there going without a
control, so an `irreversible` area's [deploy to
production](07-what-particular-gates-decide/08-deploy-to-production.md) is a human's
whatever the formula returns, and what that human accepts is an exposure the platform gives
the factory no way to limit.

A schedule is a share of traffic inside one target, and a production environment has
[targets](../05-environments/01-records-and-one-long-lived-branch.md), plural, so the
rollout has a second structure over them. The deployer reaches the targets in the order the
environment record lists them, one at a time, on either row. A target is not reached until
the target before it is marked complete and the targets already reached have served the
**bake volume**: the traffic those targets serve before the next is reached. It is a volume
rather than a period because the [window](../08-operations/02-the-analysis-window.md) is a
volume condition, and a period on a quiet service measures nothing. It is a field on the
[service record](../02-intent-into-items/03-decomposition/README.md) beside the window
limit, and where an owner authors none the score supplies it from the same half of the
vector the strategy is read from. A
[safeguard](../09-gate-policy/02-one-shape-across-all-of-them.md) (9) may raise it and
never lower it. It is not one of [gate policy](../09-gate-policy/README.md)'s eleven rows,
the way the [explicit threshold](../08-operations/01-the-health-monitor.md) is not.

The window's cap bounds the whole. Once it has run, the window closes timed out and the
deployer reaches the remaining targets with no hold between them, since a quiet service
that never serves the bake volume would otherwise never complete a deploy. The window
records what each target served, so that case is weak protection reported as weak and
never a rollout that looked like a watched one. What the order buys is a bound on how much
of production a bad release reaches: the first target and no more until that target has
been read. On the row without a control nothing inside a target limits anything, so there
the order and the bake volume are the only limit the factory has. What it costs is a
rollout as long as the targets times the bake volume on the slowest of them, on a service
whose window would otherwise have closed on the first target's traffic alone. The
[rollback](../06-releases/06-rollback.md) is still across every target reached, so what
the order limits is the exposure and not the width of the recovery.

A service's first release has no control whatever the score prefers: there is no build being replaced, so nothing can keep serving beside it and there is no control to run. The choice exists from the second release; what the first is measured against instead is in [_The health monitor_](../08-operations/01-the-health-monitor.md).

The first row also makes a demand of the **platform** — whatever a service runs on, reached through seam 4 of [_Deferred, but not designed out_](../../deferred.md) — and not every platform answers it. Serving a share means deciding what fraction of arriving traffic reaches each of two builds, which the named operations of seam 4 of [_Deferred, but not designed out_](../../deferred.md) have to carry through to the target. A platform that moves instances rather than traffic — replacing them one at a time, or all at once, on its own schedule — cannot answer it: both builds run and nothing decides what each serves, so there is no comparison to make and nothing to shift back when it goes wrong. Where a service runs on one of those, the row is unavailable and every deploy goes without a control — the exemption a first release already takes, arriving here for a different reason and permanently rather than once. What that costs is that the score's choice of strategy is bounded by where a service happens to run: two services can be scored identically on one factory and roll out differently, and an owner reading a deploy without a control cannot tell a low number from a platform that offered nothing else. It is stated here rather than left to the [deployer](../08-operations/01-the-health-monitor.md) to discover, because a strategy the platform cannot perform is a rollout reporting success having run no comparison at all.

Whether a target's platform serves a share is a field on the [environment record](../05-environments/01-records-and-one-long-lived-branch.md) beside the target, declared with it, and the score picks the row with a control only where every target of the environment serves one. The production [deploy record](../06-releases/05-the-deploy-record/README.md) names the strategy the deployer performed beside the one picked. The two differ where a target declared as serving a share refused the operation of seam 4 that shifts one: the deployer performs the row without a control on that deploy and writes so, and a rollout that ran no comparison is on the record as one. The [window](../08-operations/02-the-analysis-window.md), [Ops](../11-screens/01-work-ops-factory-people.md), and the score when it learns all read the performed field, so an owner reading a low number beside a deploy without a control reads on the same record whether the platform was the reason. What it costs is a field on every target and a second strategy field on every production deploy, equal on nearly all of them.

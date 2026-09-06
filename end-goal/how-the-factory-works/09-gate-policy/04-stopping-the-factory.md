# Stopping the factory

Everything else an owner authors is per subject and adds protection. A
[safeguard](02-one-shape-across-all-of-them.md) puts a human at a gate, a [risk
threshold](01-what-is-in-it.md) is a field of one environment record, and a parameter
narrowed is narrowed for one scope. None of them is a stop: a human at a gate still has
Approve in front of them, and the [emergency action](../01-one-pipeline.md) is approve now
rather than skip. So an owner who had lost confidence in the factory itself had one remedy,
which was to author a value on every service in the install, one write at a time, while the
queue went on merging and numbered releases went on deploying.

A **halt** is the one authored record whose subject is the factory. Factory writes it, the
way it writes a safeguard and under the same rules: it is never edited, it is withdrawn by
a second record naming it, and that withdrawal is not in force until [_A halt's
withdrawal_](../03-gates/07-what-particular-gates-decide/11-a-halts-withdrawal.md) approves
it. Setting it and withdrawing it each append a [policy
version](02-one-shape-across-all-of-them.md), so the interval the factory stood halted is a
fact of the trail with an [actor](../../deferred.md) at each end, rather than something a
later reader reconstructs from what stopped arriving.

While one stands, every firing of a [deploy to
production](../03-gates/07-what-particular-gates-decide/08-deploy-to-production.md) row on
every service holds, and [the merge queue](../05-environments/03-the-merge-queue.md) stops
fast-forwarding every service's candidates, written into [the log](../../deferred.md) the
way [the backlog cap](../08-operations/03-overlapping-windows.md)'s stop already is. Nothing
is decided, no [attempt](../03-gates/05-the-attempt-limit.md) is counted, and the
[score](../04-risk-score/README.md) learns nothing, which is the treatment [a
hold](../03-gates/04-what-a-gate-may-change.md) already gets and the reason a halt is one
rather than a reject.

It is the one hold no approve passes. Every other hold the factory sets offers a human
Approve at the row, and approving through this one would be withdrawing the halt in the
place the design least wants that decided: at a deploy gate, by whoever is there, during
whatever made the owner set it. What passes it instead are the two exceptions the
[exhausted error budget](../08-operations/05-service-level-objectives.md) hold already
takes, a revert and an item the [health
monitor](../08-operations/01-the-health-monitor.md) raised on that service. A revert is known by the link its intent carries to [the release it undoes](../06-releases/06-rollback.md), whichever source raised it, and never by the source. The queue stop
does not catch a revert's own candidate, for the reason the backlog cap's does not.

It suspends nothing that recovers. The [rollback](../06-releases/06-rollback.md) inside an
open window runs, [the search](../08-operations/03-overlapping-windows.md) runs, and the
revert ships, for the reason [_Overlapping
windows_](../08-operations/03-overlapping-windows.md) gives against a pause: a stop that
removed a recovery would remove it in exactly the period a human is least available. A halt
stops the factory acting forward and never stops it undoing what it did.

Setting one is the owner's and is not a thirteenth [duty](../../what-humans-do.md): the
withdrawal row belongs to no duty and widens to the owner, which is the routing every
unheld row already takes. What it costs is the one stop in the design a human can set and
then walk away from, the state _Overlapping windows_ refuses for the rollback, admitted
here because what it stops is the factory acting rather than the factory recovering. Two
things bound that. The record shows on [Factory](../11-screens/01-work-ops-factory-people.md)
and on [the home view](../11-screens/02-three-properties-every-screen-needs.md) for as long
as it stands, so a halted factory is never a quiet one, and ending it is a decision at a
row rather than a field somebody flips.

A halt is unbounded and reaches everything. A **change freeze** is the other shape: a
period, on one service, authored ahead of what it is for. It is a field of the [service
record](../02-intent-into-items/03-decomposition/README.md) beside the [hours within which
the service pages](../08-operations/07-pages.md), and it names periods within which that
service's production deploys are held. It is authored outright with nothing supplied, for
the reason those hours are: nothing the factory observes says when a customer's peak
trading period, contractual change calendar, or notified maintenance window falls. A
safeguard (9) may add a period or lengthen one, and may never shorten one.

It holds the way an exhausted error budget holds, and that is the whole of the mechanism:
nothing is decided, no [page](../08-operations/07-pages.md) fires, and the hold lifts itself
when the period passes. It takes the same two exceptions, so a freeze never holds the fix
for what the freeze made worse. It binds deploys alone and the rollback inside a window is
untouched, so a freeze stops what ships and never what recovers.

What an owner had instead was a safeguard putting a human at that service's production
deploy row, which differs in four ways. It is permanent rather than bounded, it costs a
human in front of every release on the service rather than only those inside the period, it
is authored per service and not per period, and withdrawing it takes a gate row of its own,
so it cannot lift itself when the period ends. What the freeze costs is a value that goes
stale the way the page hours do: a period authored too wide holds that service's work for
the whole of it, which the two exceptions bound and nothing else does.

# One process

[_Components and what they call_](components.md) lists every component and what it may
call. This file is the deployment model those components run under.

**The factory is one process, and [the drift
detector](how-the-factory-works/08-operations/08-drift-detection.md) is the second.** [_An
environment per
candidate_](how-the-factory-works/05-environments/02-an-environment-per-candidate/README.md) says so as
a fact about what an install provides, and it is the whole of the deployment model. Every
component the inventory lists runs inside that process, with the two exceptions already stated: the way in
ships inside each deployed service, and the drift detector is installed beside the factory
so that it is not in it. Exactly one instance of the factory runs, and what enforces it is
the factory's own store, through a **lease** the store holds: one row naming the instance
holding it, an expiry the holder renews on a pass of its own, and a number that rises by one
at each acquisition. A starting process acquires the lease where it is unheld or expired and
takes the next number; while another holds it, the start fails rather than running beside
the holder. Two components make that necessary rather than tidy. The merge queue reads
master before it mints and the health monitor is the only thing that closes a
[window](how-the-factory-works/08-operations/02-the-analysis-window.md), so two instances
would mint one number twice and close one window twice, and no record afterwards would tell
either from one instance doing it once.

The lease alone does not reach an instance that stalled, a host suspended or a network
partition, and resumed after another acquired the lease it let expire.
The number is what reaches it. It is a **fencing token**, the term distributed systems uses
for a number a lock's holder carries so that a store can refuse a write from a holder whose
lock has lapsed. Every write any component makes to the factory's own store carries the token
of the instance making it, and the store refuses, in the same transaction as the write, one
whose token is not the lease's current number. A resumed instance finds its first write
refused and stops, before a second release number or a second close of a window and not
after. The log's append carries one condition more: the row is written only where the head
is still the one its chain field hashes over, in the same transaction, so two appenders
reading one head produce one row and one refusal. That holds from the first row for the
reason [seam 2](deferred.md) gives for the chain field, since a fork could not afterwards be
told from an edit.

What the token cannot reach is the far side of [seam 4](deferred.md): a deploy target checks
no token, and a call begun under a live lease can finish after it lapsed. What bounds that is
order. The deployer writes the [deploy
record](how-the-factory-works/06-releases/05-the-deploy-record/README.md)'s row for a target
before it calls that target and marks the target complete after, both writes carrying the
token. A stalled deployer's claim is refused, so it makes no call, and one that lapsed
mid-call completes nothing. A target running what no completed record names is the first
comparison [_Drift detection_](how-the-factory-works/08-operations/08-drift-detection.md)
makes, a mismatch cleared by a human, so every effect a stale instance can have on a target
is refused before it happens or read as a mismatch after. Neither the lease nor the schema
history below is a record of the graph, and [_Records and their writers_](records.md) lists
no row for either: each is the store's account of itself.
What it costs is a field on every write and a renewal write per interval, an interval the
factory supplies for itself the way the drift detector supplies its own. A restart after a
crash waits out the interval before it can acquire, and a deploy a lapsed instance finished
is a page and a human's clearing rather than a refusal.

**One process is not one failure.** A component here is a pass or a loop of its own, and one
can stop or fail on one service's work while the rest go on. That is why [the health
monitor](how-the-factory-works/08-operations/01-the-health-monitor.md) writes a [last
check](how-the-factory-works/08-operations/08-drift-detection.md) per service rather than one for
itself, and why [the home
view](how-the-factory-works/11-screens/02-three-properties-every-screen-needs.md) reads each of
those records against the interval that record carries rather than the newest of a class. What one process does
settle is the other end: the process stopping makes every last check stale at once, which is
one state and not four, and nothing inside the factory is left to read it.

**Every component's restart is a read of its own records.** The merge queue reads master at
every start and writes the release record its own unfinished merge left owing. [The
deployer](how-the-factory-works/08-operations/01-the-health-monitor.md) completes or returns the
[deploy records](how-the-factory-works/06-releases/05-the-deploy-record/README.md) no target has
finished. The health monitor's restart is the set of windows those same records left open: a
window opens when a deploy record is written and closes at exactly one of four exits, so a
start finds the open ones by reading records and never by keeping a list. It evaluates each
of those windows again. An [exit](how-the-factory-works/08-operations/02-the-analysis-window.md)
that a stop interrupted partway is finished by that second evaluation, because the close is
the exit's last step under [_Tight
integration_](what-the-factory-does/01-tight-integration.md)'s rule for an event that
writes more than one record. The notifier's
restart is the [delivery record](how-the-factory-works/08-operations/07-pages.md) it overwrites
per waiting row, so a row still waiting is delivered again and one that stopped waiting is
not. Factory's restart reads the newest [policy
version](how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md) per scope and
rewrites any owner-authored field not already holding what that version names.
Every other component holds nothing between calls and resumes by being called again.

**The factory's own store keeps a schema history too, the same shape [_The store is a
contract too_](how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md)
defines for a service's store.** It is one row per change, naming the version that shipped
it, the change's identity, a checksum of its text, and whether the change widened the store
or removed something from it. The store rule holds here, and its consumer is the version
before, which a bad upgrade has to be able to put back. So a breaking change to this store
is the same sequence, spread over versions rather than items. The version that ships it adds the new form beside the old, writes both,
and copies every row the old form holds into the new, at every start until the drop, a row
already copied being skipped. The removal ships no earlier
than the version after. The promise carries a number and the number is one, in both
directions: a version reads what the version before it wrote, and the version before reads
and writes what this version wrote, until the version after that removes the old form.

A version's first start reads the history, and starts against one that holds, beyond the
changes it declares, only rows from the version after it that widened the store. It refuses to start where the
history holds a removal it does not declare, a row from a version further ahead, or a change
it declares that the history cannot honour, and a skipped version is not a supported
upgrade. So a bad upgrade is undone by starting the version before, with nothing restored:
what the bad version wrote is in a form the version before reads, and the rows the version
before writes meanwhile are reached by the copy when the newer version starts again.

The upgrade is the [install event](deferred.md) the log records, and the row names the
changes the first-start step applied. Where a change cannot take that sequence, the version
declares it as a one-way step, a removal with no widening before it, and a snapshot stands
for the version before. Before the first-start step applies a removal,
the drop or a one-way step, it takes a snapshot of the factory's own store, verifies it, and
names it on the install-event row beside the change, the treatment [_A schema
change_](how-the-factory-works/06-releases/05-the-deploy-record/01-a-schema-change.md) gives
a service's store before a destructive change. A snapshot it cannot take and verify is an
upgrade not performed: no change is applied and the version refuses to start, the refusal a
history mismatch already takes. The snapshot is kept on the store's own hosting, so the
install event says where what a removal destroyed can still be read. It covers the
factory's own store and not the whole [recovery
unit](what-the-factory-does/04-what-the-factory-does-not-build.md), since an upgrade
touches nothing else in the unit. Undoing a one-way step after the version has run costs
what any restore costs: everything the version wrote is lost, and [_Drift
detection_](how-the-factory-works/08-operations/08-drift-detection.md) is loud on every
service that shipped since. That is why the sequence above is the ordinary case, and a
version declares a one-way step only where no widening can carry the change. What the rest
costs is a change to this store spanning two versions, a copy at every start until the
drop, and a startup refusal an owner can hit on a bad upgrade.

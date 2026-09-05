# Drift detection

Every other check in [_Operations_](README.md) reads a record the factory wrote. A service's [_current release_](../06-releases/05-the-deploy-record/README.md) is what its production deploy record marks complete on every target, the health monitor measures against a control the factory started and recorded, and an incident points at a deploy the same log describes. So a factory whose records are wrong reports itself healthy, and nothing downstream of them contradicts it.

The **drift detector** is one process outside the pipeline that reads what is actually
running on each production target and compares it against what that service's production
[deploy record](../06-releases/05-the-deploy-record/README.md) marks for that target — the release
it names where the target is marked complete, the previous one where it is not, and nothing
where the complete record is a [removal](../02-intent-into-items/03-decomposition/04-retirement.md)'s. The second
comparison is [the log](../../deferred.md)'s. Each pass it reads the chain's head, verifies
the chain still holds the head it recorded last pass, extended and nothing else, and records
the new one. A row's chain field holds a hash over that row's payload and over the same
field on the row before it, so what the check catches is a payload edited in place and not
only a row removed or added. A chain its recorded head is no longer inside is a mismatch whose hold
matches the record it impeaches: a deploy record reaches one service, so its mismatch holds
that service's production deploys; the log reaches every decision, so this one holds every
service's, cleared like any other by a human at the detector. A
[truncation](../09-gate-policy/03-what-is-not-in-it/02-retention.md) removes the oldest records and never
the head, so the check reads through one undisturbed. The exception is retention shorter than
the gap since the detector's last pass, where the cut removed the recorded head: that is the
same mismatch, with the truncation row as the evidence the clearing human weighs, a real cut
and a fabricated row reading the same.

Two facts, two comparisons, read-only — no deploy privilege, and it writes into a store of
its own that no factory component may write, the recorded head being the detector's own with
nothing else reading it. It is not true that the factory reads nothing back: a mismatch holds
a gate and pages, and both require reading it. What the independence actually requires is
narrower and is the rule — nothing the drift detector writes is evidence about the software.
It cannot cause anything to be built, deployed, scored, approved, or measured, and the one
thing it can do is stop.

The seam is two records and four readers. The **mismatch** is read by the gate component at
the moment the production deploy gate fires, and by the notifier. The **last check** per
production target is overwritten each pass and carries the interval that pass runs on, read
by Ops and by [the home view](../11-screens/02-three-properties-every-screen-needs.md) so a
stopped drift detector is visible rather than silent, and by the gate only where an owner's
safeguard sets a maximum age on it. The interval is on the record because the age that means
stopped has to be readable by something that authored nothing: a record older than the
interval it names has missed a pass, which is what [the home
view](../11-screens/02-three-properties-every-screen-needs.md) reads it against. An owner's
safeguard supplies its own maximum age for the gate and is not a narrowing of this one: the
two readings differ on purpose, the interval catching a missed pass on a screen and the
authored age deciding how stale is too stale to deploy through, which is a judgment about
production and not about the detector.

That shape is every last check record's: the health monitor's per service, the deployer's per
target of a persistent
[environment](../05-environments/01-records-and-one-long-lived-branch.md) and its one for the
[platform](../05-environments/02-an-environment-per-candidate/README.md), the notifier's
one, and one each for the pass over [the constraints in
force](../02-intent-into-items/01-intake/01-constraints-and-the-design-system.md) and the pass
over [the advisory feed](../02-intent-into-items/01-intake/03-advisories.md). [Dispatch](../02-intent-into-items/05-dispatch.md)'s one for the pass that argues a [fleet proposal](../10-fleet/07-a-fleet-proposal.md) is the same shape, so a stopped pass, a raise or the factory's own arguing alike, is a named row rather than clean numbers. A last check is the writing component's own
record of its own pass, about the component and never about the work, which is why a detector
may write one beside the intents it raises without being the second writer [_A fleet
proposal_](../10-fleet/07-a-fleet-proposal.md) refuses — the record of another kind that rule
reaches is a record about the work. So no component needs a threshold authored per instance to
be told apart from a silent
one, and the detector supplies its own interval the way it supplies its own recorded head,
the owner installing it once and authoring no interval after. A record is owed a pass for
exactly as long as the thing it names exists, and the component that writes it records on its
last pass that no further pass is owed — an overwrite like every other pass's, the record
kept the way everything in [the graph](../../what-the-factory-does/01-tight-integration.md)
is kept rather than deleted. So a record past its interval with a further pass owed is always
something that stopped, and never something that went away.

That is why the deployer's is per persistent target and not per deploy target. A [candidate's
environment](../05-environments/02-an-environment-per-candidate/README.md) exists for one item, every
deploy into one has that item waiting on it, and a deployer stopped there is already a stalled
timeline in [Work](../11-screens/01-work-ops-factory-people.md). A record per candidate target
would be a create, a write every pass, and a delete, per item, to report what Work reports
anyway. What the narrowing gives up is that a deployer stopped on candidate deploys alone is
read off a timeline rather than named on the home view. In the other direction it reads one
factory record per comparison: each service's production deploy record with its completion per
target, and the log's head row. Failing to reach a target is not a mismatch, since a network
blip would otherwise hold every production deploy. That is why the last check exists at all: a
check that silently stops is worse than the bug it catches. The owner installs it beside the
factory they already host, because a checker the factory deployed would be inside the trust
domain it exists to check.

A third comparison reads the last check records themselves, and it is what makes a stopped factory component reach a human. The detector already reads its own record's interval to say when a pass is owed, and the same reading over the others costs it nothing new. A last check past the interval it names with a further pass owed is a mismatch of the shape the two above have, holding what the stopped component reaches and paging: the health monitor's holds that service's production deploys, the deployer's holds that environment's. It stays inside the rule rather than widening it, because a factory component having stopped is not evidence about the software, and the detector still cannot cause anything to be built, deployed, scored, approved, or measured. What it reaches that nothing else could is the fourth [page](07-pages.md) condition, a window past its cap that nothing has evaluated. The health monitor is the only thing that closes a window, so the component that would have raised that page is the one that stopped, and the reader has to be outside the factory. The mismatch a stopped raise makes holds nothing, the two passes reaching no deploy, and the page is the whole of it. One component this cannot cover is the notifier, for the reason [_Pages_](07-pages.md) already gives, a mismatch about the notifier being delivered by the notifier; [the home view](../11-screens/02-three-properties-every-screen-needs.md) stays the whole of what reads that one. What the comparison costs is a page for a component whose pass merely ran late, which is the [home view](../11-screens/02-three-properties-every-screen-needs.md)'s stated cost arriving on the narrow channel, bounded by the interval each record carries rather than by anything an owner authors.

The rollout exemption [_The health monitor_](01-the-health-monitor.md) grants has a second bound beside [the window's cap](02-the-analysis-window.md), and it is the deployer's own last check for that target. A rollout advances only while the deployer runs, so a target the exemption covers whose deployer last check is past the interval it names, with a further pass owed, stops being exempt and reads as an ordinary mismatch. Without the bound the exemption survives exactly the stop it cannot survive: a deployer that stopped mid-rollout leaves targets it never reached, its record marks none of them complete, and the check that would notice production split across two builds is suppressed by a record the stopped component wrote. It is the cap's argument at a second scale: an exemption may not outlive the component whose work it describes. It reaches the case the cap cannot, a stop early in a window that has hours of its own left to run. What it costs is a mismatch on a rollout merely slower than the deployer's own interval, cleared by a human here like any other.

A fourth comparison reads the fleet a rollback would need. While a window is open over a
production deploy, that [deploy record](../06-releases/05-the-deploy-record/README.md) names per
target how many instances of [the rollback's target](03-overlapping-windows.md) are kept
there, and the detector reads how many are actually running against that count. Fewer is a
mismatch of the ordinary kind, holding that service's production deploys and paging while
there is still a release to protect, rather than surfacing as a recovery that failed. It
stays inside the rule the way the third comparison does: how many instances stand on a
target is a fact about what is running and not a judgment about the software. The rollout
exemption is bounded by it as well, since a build the exemption excuses for being the
release a rollback would return to excuses nothing about how much of that build is there.
Without the comparison, the one resource the [rollback](../06-releases/06-rollback.md)
cannot do without is the one thing on a production target nothing reads, and a target
running one instance where forty were kept is excused exactly as forty are. A [mitigation](../../deferred.md) that changed a target's instance count is read together with the deploy record it names, since the detector reads a mitigation still standing as intended state and not as this comparison's mismatch.

A fifth comparison reads the [schema history](../06-releases/05-the-deploy-record/README.md) the deployer keeps in each service's store against the changes the current release's build declares, through the read access it already has to the target. A history holding a change no release up to the current one declares, a row whose checksum differs from the change's text in the build, or a change the current release declares that the history lacks is a mismatch of the ordinary kind, holding that service's production deploys and paging. It stays inside the rule the third and fourth do: which changes a store carries is a fact about what stands and not a judgment about the software. Without it the one change a [rollback](../06-releases/06-rollback.md) cannot undo is the one thing on a production target that nothing outside the trust domain reads, and a deployer whose account of a store has drifted from the store drifts unseen. What it costs is a read of every production store each pass, and a mismatch on a store an operator changed by hand, cleared by a human here like any other.

The first comparison reads a third fact where the target can report one: the digest of the artifact running there, against the digest the release's [build](../05-environments/01-records-and-one-long-lived-branch.md) names for the artifact [the build runner](../05-environments/01-records-and-one-long-lived-branch.md) produced. A release name says which build a target should be running; the digest says whether the bytes there are that build's, which a name cannot, because a store can serve other content under one name. A digest that differs is a mismatch of the ordinary kind, holding that service's production deploys and paging. A target that reports no digest is compared on the release alone, and the last check for that target records that, so a target the comparison cannot reach is listed rather than read as agreeing. It stays inside the rule the others do: what bytes a target runs is a fact about what stands and not a judgment about the software. What it costs is a mismatch on a platform that rebuilt an artifact in place under the same name, cleared by a human here like any other.

A sixth comparison reads a third fact where the target can report one: the configuration digest running there, against the digest the deploy record names beside the build's. A target that reports no configuration digest is compared on the release alone, the way the first comparison already is, and the last check for that target records that. It stays inside the rule the others do: what value set a target runs is a fact about what stands and not a judgment about the software. What it costs is a mismatch on a target changed by hand outside the factory, cleared by a human here like any other.

A mismatch remains until a human clears it at the drift detector, even where a later comparison agrees, which the drift detector records on it so the human clearing has the evidence. Clearing it from Ops is refused: it would make the factory a writer of the record that says the factory is wrong, and a deploy that did not complete is precisely the kind of bug that would clear it on retry. What that costs is two actions in two places at the moment production is worst — approve through at the gate, then clear at the drift detector — and one record with no [chain](../../deferred.md) behind it, so the trail of what stopped a deploy is complete only when a second store is read beside the log.

Its store's life follows from its independence the way its installation does: a store no factory component may write is not the factory's to keep either, so keeping it, and with it the half of that trail only it holds, is part of installing the detector, the owner's as hosting the factory already is. And the one event that makes the detector loud on every service at once is the factory's own records restored from a backup, which is the mechanism working rather than failing. Every deploy record falls behind what runs, every service that shipped since the backup mismatches, production deploys hold, and each mismatch is cleared by a human holding the evidence of what the restore lost. It is the same stop [_What the queue reads before it mints_](../05-environments/05-what-the-queue-reads-before-it-mints.md) reaches from the other side when it finds master and the release records apart.

A restore is partial by design: the [recovery unit](../../what-the-factory-does/04-what-the-factory-does-not-build.md) excepts the detector's own store, so a restored graph and log can carry a chain shorter than the head the detector still holds. That is the second comparison's ordinary case, not a new one: the recorded head is no longer inside the restored chain, a mismatch holding every service's production deploys until a human clears it at the detector.

What the first comparison catches is the record that was wrong when it was written — a deploy that did not complete, the deployer recording one the target never received, a target changed underneath. That is the common failure and it is a bug rather than malice. A record rewritten afterwards is the other failure, and the head the detector records is what catches that one — the anchoring seam 2 of [_Deferred, but not designed out_](../../deferred.md) places here, with what it is and is not evidence against stated there.

A mismatch holds that service's production deploys and [_pages_](07-pages.md). The factory does nothing further, because every remedy it has reads the record in question — a rollback targets a release computed from the windows the factory itself closed, and using records that have just been shown wrong repeats the fault with more traffic behind it. That hold does not lift itself, which is what makes the page necessary rather than informational: the service cannot receive its own fixes until a human ends it. The hold reaches the rollback the [health monitor](01-the-health-monitor.md) performs on its own, a rollback being a deploy event. A [window](02-the-analysis-window.md) that [fails](02-the-analysis-window.md) while a mismatch stands closes failed with no rollback called for: no deploy begins, so no deploy record is written, the failed release keeps serving, and the health monitor raises the revert intent itself and pages — the treatment [_Rollback_](../06-releases/06-rollback.md) already gives a failed exit that finds no release to return to. The human clearing the mismatch holds both facts, and what production returns to is theirs, at [Ops](../11-screens/01-work-ops-factory-people.md) or on the platform directly. What that costs is production on a release that just failed its window until a human acts, both pages standing — chosen over shifting traffic onto a target computed from records the factory has just been told are wrong, the hold's own reason.

The drawback is a second process to run, and the cheaper alternatives were rejected rather than the drift detector being free. Anchoring the chain head alone is nearly free in the record, but a head the owner keeps by hand is custody that never ends, a duty — which is why the head lives in the detector's store instead, kept by the process rather than the person. Installing the drift detector is done once and requires nothing after, so it stays outside the twelve the way hosting the factory does. Target-side attestation on [seam 4](../../deferred.md) uses up that seam's "however it is implemented" early and creates the same custody duty anyway.

What the drift detector is not is a second opinion on whether the software is good. It compares what the factory recorded against what stands — a target's build, the chain's head — and judges neither; the health monitor, the score, and the criteria all stay where they are. One that acquired a judgment of its own would be the second path [_One pipeline_](../01-one-pipeline.md) refuses, arriving indirectly.

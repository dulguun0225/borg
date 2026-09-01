// Package healthmonitor is the health monitor: what the factory does after a deploy,
// how long it may act on that measurement alone, and what it does when something
// is wrong.
//
// # What it is and what it is not
//
// It opens a analysis window at a production deploy, reads the quantity, evaluates
// the [boundary], closes the window at exactly one of four exits, writes an
// incident at a crossing, calls for a rollback at the failed exit, and raises an
// intent for a crossing found after the window closed. It is the only component
// that reads production behaviour and knows which release and which deploy that
// behaviour belongs to.
//
// Two things it does not do, both behind interfaces its caller implements.
// [Signal] is where the quantity comes from, because what emits it is the software
// the factory wrote and where that lands is the substrate's arrangement.
// [Rollbacker] is the rollback, because reaching a deploy target is the deploy
// agent's and this package reaches none. That leaves the health monitor a reader of
// records and a writer of four of them, which is what makes it testable against a
// signal a test composes.
//
// # No control, so the fallback and only the fallback
//
// The real comparison is made against a control — an instance set running the
// build already in production, started alongside the release and taking comparable
// traffic. A control requires the old build to keep serving, which is exactly what
// a deploy without a control does not do, and on a substrate that moves a process
// rather than traffic every deploy goes without one. So no control is ever started here and
// what runs is the weak fallback: the release read against the recent history of
// the release a rollback from it would return to.
//
// What the fallback does not answer is age. A new process and a long-lived one
// differ before either serves a request — cold caches, empty pools, a compiler that
// has not yet seen the workload — so a release is slower for being new and this
// comparison reads part of that difference as the change. Nothing here corrects for
// it, and a control of the same age is the only thing that would.
//
// What the fallback does answer, and had to be corrected to answer, is which
// release is the baseline. Reading against the ordinal predecessor breaks under
// overlapping windows: the predecessor of a release under watch is itself under
// watch, so its history is not history yet and a regression the lower one
// introduced becomes part of what the upper one is measured against. The baseline
// is [TargetBelow] instead — the newest release below the one under watch whose
// window closed without failing it, which is the same release a rollback from it
// would return to. That is single-valued whatever is open above it.
//
// # A rollback is always the slow one
//
// The fast rollback shifts traffic onto the target's own instances, which a
// rollout with a control keeps running at full capacity while any open window
// could return to them. With nothing of the old build left running there is
// nothing to shift onto, so every rollback here is the slow one: the target's build
// redeployed and waited for. Production runs the failed release for as long as
// that takes.
//
// [TargetBelow] is what a rollback returns to and it is written nowhere. The
// release record is written once at the fast-forward and never again, so an outcome
// settled by a window closing long afterwards cannot be a field of it. CloseEvent at
// timing out counts as a target: a release that was never failed is one the
// factory can return to, and requiring a passed close would leave a service too
// quiet to reach one with no target at all. CloseEvent skipped does not — nothing is
// left running a skipped release's build.
//
// Master is linear, so a rollback undoes the failed release and every release
// above it, up to the window limit. The failed one is named apart from the swept ones on the
// rollback's own deploy record, and the open window of each swept release is closed
// skipped. A swept release whose window had already closed keeps the exit it closed
// at: a window closes once.
//
// # Inside the window and outside it
//
// Inside the window a crossing rolls the release back with no human involved.
// [Watch] is that: the window's own authority, limited by evidence and by nothing
// else. Outside it the same crossing raises an intent instead, which is
// [AfterWindow]: the change has been live for a week and the window's authority
// ended long before, so what it produces is an unrefined intent taking the same
// stages and the same gates as any other. That is the whole of "finds issues and
// fixes bugs" — detection writes an intent and the pipeline does the rest.
//
// An incident is written at either, and deduplication is what keeps the second
// from becoming a second intent: an open incident on this service and this release
// makes a further crossing an observation on it. A rollback does not resolve one —
// production is still worse, which is what the hold and the page both say — and
// [ResolveSettled] is what does, once the crossing has stopped against what is
// running and the item raised from the incident has shipped.
//
// # What closes a window, and what a stopped health monitor costs
//
// Nothing but this package closes a window, which is what the design requires: the
// health monitor is what evaluates every exit. So a health monitor that stops running
// leaves windows open, reaches the window limit, and holds that service's production
// deploys — a wait
// on the factory, which does not page. What makes that survivable is that every
// pass reads what it needs from the store rather than from what a process
// remembers, so the next pass finishes what the last one left.
//
// Who may write what: this package writes the analysis window through
// [window.Writer], the incident through [incident.Writer], and the revert intent
// through [intent.Intake], and it calls [notifier.Notifier] for what a human should
// hear about. It writes no deploy record and reaches no target — [Rollbacker] does
// both.
//
// What defines it: ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md
// for the control, the fallback, and what the health monitor is;
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md for the window
// and its four exits; ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// for the window limit, the rollback's target, and what a rollback sweeps;
// ../../end-goal/how-the-factory-works/08-operations/04-after-the-analysis-window.md for the
// intent a later crossing writes;
// ../../end-goal/how-the-factory-works/08-operations/06-incidents.md for the incident and its
// deduplication; and ../../end-goal/how-the-factory-works/06-releases/06-rollback.md for
// what a rollback is and what its record names.
package healthmonitor

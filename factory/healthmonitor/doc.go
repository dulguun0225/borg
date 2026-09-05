// Package healthmonitor is the health monitor: what the factory does after a
// production deploy, how long it may act on that measurement alone, and what it
// does when something is wrong.
//
// healthmonitor.go is [HealthMonitor] and [New], the [Watching] service every
// call takes, and the two things it does not do itself, both behind interfaces
// its caller implements: [Signal], where the [Quantity] comes from, because what
// emits it is the software the factory wrote and where that lands is the
// substrate's arrangement; and [Rollbacker], which performs a [Rollback],
// because reaching a deploy target is the deploy agent's and this package
// reaches none.
//
// open.go is [HealthMonitor.Open], one window per production deploy of a release
// its service has not watched before, and [HealthMonitor.Room], the read the
// window limit is compared against. watch.go is [HealthMonitor.Watch]: per open
// window it returns a [Reading], evaluates the [boundary], closes the window at
// exactly one of four exits, writes the incident a [Crossing] raises, and calls
// [Rollbacker] at the failed exit — which sweeps the releases above the failed
// one and closes their windows skipped. after.go is
// [HealthMonitor.AfterWindow], the same crossing found once the window has
// closed, which raises an intent through intake instead of rolling back, plus
// [HealthMonitor.ResolveSettled] and [Shipped]; [RevertStatement] and
// [AfterWindowStatement] are the statements of the intents raised either way.
//
// target.go is [TargetBelow]: the newest release below the one under watch whose
// window closed without failing it, which is both the baseline the release is
// read against and what a rollback returns to. It is computed rather than stored
// because the release record is written once at the fast-forward, so an outcome
// settled by a window closing long afterwards cannot be a field of it.
//
// Who may write what: this package writes the analysis window through
// [window.Writer], the incident through [incident.Writer], and the revert intent
// through [intent.Intake], and it calls [notifier.Notifier] for what a human should
// hear about. It writes no deploy record and reaches no target — [Rollbacker] does
// both. Nothing but this package closes a window.
//
// What defines it: ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md
// for the control, the fallback this substrate leaves, and what the health monitor is;
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md for the window
// and its four exits; ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// for the window limit, the rollback's target, and what a rollback sweeps;
// ../../end-goal/how-the-factory-works/08-operations/04-after-the-analysis-window.md for the
// intent a later crossing writes;
// ../../end-goal/how-the-factory-works/08-operations/06-incidents.md for the incident and its
// deduplication; and ../../end-goal/how-the-factory-works/06-releases/06-rollback.md for
// what a rollback is and what its record names.
package healthmonitor

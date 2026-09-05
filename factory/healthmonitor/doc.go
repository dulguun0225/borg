// Package healthmonitor is the health monitor: what the factory does after a
// production deploy, how long it may act on that measurement alone, and what it
// does when something is wrong.
//
// healthmonitor.go is [HealthMonitor] and [New], the [Watching] service every
// call takes, [Arm], [Reading] and [History] — what one call asks the
// [Emission] for — [Control], [Rollback] and [SearchDeploy] — what the
// [Deployer] is asked to do — [Builder], [Pager], [Mismatches], and
// [Readings], the two readings beside the comparison this package is handed
// rather than computes. open.go is [HealthMonitor.Open], one window per
// production deploy of a release its service has not watched before,
// [HealthMonitor.Room], the read the window limit is compared against, and
// what starts a control where the strategy keeps one.
//
// watch.go is [HealthMonitor.Watch]: per open window it reads the comparison
// on every target the boundary was allocated over ([Watched], [Evaluated] in
// quantities.go), reads the two other readings beside it where the
// comparison ruled nothing out (history.go), and closes the window at exactly
// one of four exits — passed and timed out tear the control down first; the
// failed exit is rollback.go's. quantities.go is [EmissionShape] with
// [EmissionShapes] and [ReadableAcross] — every emission version the factory
// has shipped and what a comparison whose arms differ in version can still
// read — [Series], [OperationSeries], [Evaluated], [Crossing] with
// [CrossingKind] and [CrossingKinds], and the arithmetic that reads one
// window's series against its boundaries. reachable.go is
// [HealthMonitor.previousRead] and the two questions it answers at the open:
// [passedReachable] and [operationsReadAlone].
//
// rollback.go is the failed exit in the order the design states it: the
// rollback's own deploy record and the releases it undid closed skipped, the
// incident and the intent it raises ([Crossed], [HealthMonitor.recordCrossing]),
// a page where nothing was rolled back, and the window closed failed last —
// never first, which is what would leave a release the factory had failed
// serving production with no rollback record, no hold, no mismatch, and
// nothing that would ever retry. after.go is [HealthMonitor.AfterWindow]: the
// same own-history reading run once the window has closed and its control was
// torn down with it, raising an intent through intake instead of rolling
// back, plus [HealthMonitor.ResolveSettled] and [Shipped].
//
// target.go is [HealthMonitor.TargetBelow] and [HealthMonitor.LastKnownGood]:
// the newest release below the one under watch whose window closed passed or
// timed out, descending past a release whose deploy stopped before its build
// took traffic. It is computed rather than stored because the release record
// is written once at the fast-forward, so an outcome settled by a window
// closing long afterwards cannot be a field of it.
//
// Who may write what: this package writes the analysis window through
// [window.Writer], the incident through [incident.Writer], its own last check
// through [lastcheck.Writer], and the intent a crossing raises through
// [intent.Intake], and it calls [Pager] for what a human should hear about.
// It writes no deploy record and reaches no target — [Deployer] does both, and
// so does the control every comparison with one is read against: this package
// only asks for it to start and to tear down. Nothing but this package closes
// a window.
//
// Not built: the deployer's [Builder] is wired but [HealthMonitor.Search] and
// the budget it spends against are not — package boundaries the search's
// three limits would need are not here either. The service-level objective's
// error budget, its burn-rate readings, and the intent a spent budget raises
// are not built: [Spend] is the shape a caller's [Emission] answers with, and
// nothing here yet computes a hold from it. The page for an open incident
// whose crossing has not stopped, with no open window, is not built — only
// the failed exit's own page is. The environment's own record of the targets
// a service with none authored runs on is not read: [targetsOrDefault] stands
// the environment in for the whole set until it is, and [unmeasurable] and
// the fallback in open.go say the same about a service's own reachability
// fields. notifier.KindFailedWithNoRollback is named here and added by
// whichever package's dispatch owns package notifier.
//
// What defines it: ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md
// for the control, its fallback, the quantities, and what the health monitor is;
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md for the window,
// its four exits, and the power; ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// for the window limit, the rollback's target, and what a rollback undoes;
// ../../end-goal/how-the-factory-works/08-operations/04-after-the-analysis-window.md for the
// intent a later crossing writes;
// ../../end-goal/how-the-factory-works/08-operations/06-incidents.md for the incident, what it
// names, and its deduplication; and ../../end-goal/how-the-factory-works/06-releases/06-rollback.md for
// what a rollback is, what its record names, and the page where it finds nothing to return to.
package healthmonitor

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
// search.go is [HealthMonitor.Search] and [SearchBudget]: one step of the
// search that recovers attribution after a batch's window fails — a build at a
// time made through [Builder], deployed with a control through
// [Deployer.DeploySearch], and measured by a window of its own — with the three
// limits the design puts on it, [ErrSearchRefused] where the service's windows
// cannot close on evidence, [ErrSearchBudgetSpent] where the budget on the
// service record is spent, and [ErrNoDeployer] where the factory is composed
// without one. objective.go is [Budget] with [Budget.Holds] and
// [Budget.Raises], [HealthMonitor.ErrorBudget] and
// [HealthMonitor.RaiseObjectiveIntent]: what is left of a service's objective,
// the burn rate over the period and over the last hour, the hold an exhausted
// or uncomputed budget sets, and the intent the objective raises keyed on the
// service and the period. pages.go is [HealthMonitor.PageOpenIncidents], the
// page an open incident whose crossing has not stopped fires where no window is
// open, and [HealthMonitor.rollbackOutstanding], which of the two kinds a page
// about a release is. mark.go is [RevertOfRollback] and [MarkStands], the two
// reads the mark a named human at Ops writes turns on.
//
// rollback.go is the failed exit in the order the design states it: the
// rollback's own deploy record and the releases it undid closed skipped, each
// with its control torn down first the way every other exit closes, the
// incident and the intent it raises ([Crossed], [HealthMonitor.recordCrossing]),
// a page where nothing was rolled back, and the window closed failed last —
// never first, which is what would leave a release the factory had failed
// serving production with no rollback record, no hold, no mismatch, and
// nothing that would ever retry. A search's window takes none of that: its
// exit is the answer, so it rolls nothing back, raises no incident, and pages
// nobody. after.go is [HealthMonitor.AfterWindow]: the
// same own-history reading run once the window has closed and its control was
// torn down with it, raising an intent through intake instead of rolling
// back, plus [HealthMonitor.ResolveSettled] and [Shipped]. kept.go is [Kept]
// and [HealthMonitor.tearDownKept]: the instances of a release a rollback
// would return to, ended at the close of the last window that could return to
// them and never at an exit of their own.
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
// Called by the command-line interface: [HealthMonitor.ErrorBudget] at the
// production deploy row, where [Budget.Holds] is
// gate.HoldErrorBudgetExhausted and the two items that pass it — a revert, and
// an item a detector raised on that service — are the row's own to admit;
// [HealthMonitor.RaiseObjectiveIntent] on the same reading;
// [HealthMonitor.PageOpenIncidents] on the pass that runs [HealthMonitor.Watch];
// and [RevertOfRollback] with [MarkStands] at the mark, where the item ids this
// returns are dropped through item.Dispatch.Drop with Ops as the caller and the
// hold lifts because the row reads the mark.
//
// [Search] has no caller: it is the step at the failed exit of a revert's own
// window, one per pass, and the composition holding the batch's deploy record is
// not built.
//
// Not built: the environment's own record of the targets
// a service with none authored runs on is not read: [targetsOrDefault] stands
// the environment in for the whole set until it is, and [unmeasurable] and
// the fallback in open.go say the same about a service's own reachability
// fields. The deploy record does not name a control per target: it carries one
// control target and one control release for the whole deploy, so what says
// which targets carry one is the count of control instances on each target's
// row, and the build every control runs is the one control release. This
// package asks the deployer to start a control on every target of the window
// all the same, and a record whose target rows name no control instances leaves
// the teardown asked for on every target the window was allocated over. Ending
// a search's own deploy at
// its exit is the deployer's and no call for it is composed: [Deployer] is
// asked for the build to be deployed and for nothing to be torn down, so the
// instances the search put in front of traffic end where the composition ends
// them.
//
// What defines it: ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md
// for the control, its fallback, the quantities, and what the health monitor is;
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md for the window,
// its four exits, and the power; ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// for the window limit, the rollback's target, and what a rollback undoes;
// ../../end-goal/how-the-factory-works/08-operations/04-after-the-analysis-window.md for the
// intent a later crossing writes;
// ../../end-goal/how-the-factory-works/08-operations/06-incidents.md for the incident, what it
// names, and its deduplication;
// ../../end-goal/how-the-factory-works/08-operations/05-service-level-objectives.md
// for the error budget, the burn rate, the hold and the intent the objective
// raises; ../../end-goal/how-the-factory-works/08-operations/07-pages.md for the
// two kinds of wait and which of them fires at any hour; and
// ../../end-goal/how-the-factory-works/06-releases/06-rollback.md for
// what a rollback is, what its record names, and the page where it finds nothing to return to.
package healthmonitor

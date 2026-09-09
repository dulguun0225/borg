// Package healthmonitor is the health monitor: what the factory does after a
// production deploy, how long it may act on that measurement alone, and what it
// does when something is wrong.
//
// healthmonitor.go is [HealthMonitor] and [New], the [Watching] service every
// call takes, [Arm], [Reading] and [History] — what one call asks the
// [Emission] for — [Control], [Rollback], [SearchDeploy] and
// [SearchDeployEnding] — what the [Deployer] is asked to do — [Builder],
// [Pager], [Mismatches], [Brownouts], and
// [Readings], the two readings beside the comparison this package is handed
// rather than computes. open.go is [HealthMonitor.Open], one window per
// production deploy of a release its service has not watched before,
// [HealthMonitor.Room], the read the window limit is compared against,
// [SizedQuantities], which quantities the comparison carries a size for —
// three, or four where [Watching.NamesAHazardousOperation] says the service's
// area names one, a fact this package does not derive because it does not
// import area — and what starts a control where the strategy keeps one.
//
// watch.go is [HealthMonitor.Watch]: per open window it reads the comparison
// on every target the boundary was allocated over ([Watched], [Evaluated] in
// quantities.go), reads the two other readings beside it where the
// comparison ruled nothing out (history.go), and closes the window at exactly
// one of four exits — passed and timed out tear the control down first; the
// failed exit is rollback.go's. quantities.go is [EmissionShape] — the whole
// versioned shape a build ships, [ShapeAt] resolving it and [QuantitiesAt] its
// quantities alone — with [EmissionShapes] and [ReadableAcross], what a
// comparison whose arms differ in version can still read — [Series],
// [OperationSeries], [Evaluated], [Crossing] with
// [CrossingKind] and [CrossingKinds], and the arithmetic that reads one
// window's series against its boundaries — including the rule every read of
// the emission passes through: a read whose newest record is older than the
// interval this service's last check carries is no volume and never a low one,
// so a window over a store that stopped keeping records cannot pass.
// brownout.go is [HealthMonitor.crossedElsewhere], the one window that reads
// more than the producer's own numbers: while a brownout's window is open every
// service is read against its own recent history, and any of them crossing
// fails it. reachable.go is
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
// without one. objective.go is [Budget] with [Budget.Holds], [Budget.Admits]
// and [Budget.Raises], [HealthMonitor.ErrorBudget] and
// [HealthMonitor.RaiseObjectiveIntent]: what is left of a service's objective,
// read per operation as the size and the explicit threshold are and held
// against the operation with least left, the burn rate over the period and over
// the last hour, the hold an exhausted or uncomputed budget sets, the two items
// that pass that hold, and the intent the objective raises keyed on the service
// and the period. pages.go is [HealthMonitor.PageOpenIncidents], the
// page an open incident whose crossing has not stopped fires where no window is
// open, [HealthMonitor.PageRollbackNotComplete], the page a rollback this
// component called for fires where its deploy record is still not complete at
// the deployer's next last check for the environment, and
// [HealthMonitor.rollbackOutstanding], which of the two kinds a page
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
// gate.HoldErrorBudgetExhausted and [Budget.Admits] decides which items pass
// it — a revert, and an item a detector raised on that service — off the three
// values the row reads from the records and hands over;
// [HealthMonitor.RaiseObjectiveIntent] on the same reading;
// [HealthMonitor.PageOpenIncidents] and [HealthMonitor.PageRollbackNotComplete]
// on the pass that runs [HealthMonitor.Watch];
// and [RevertOfRollback] with [MarkStands] at the mark, where the item ids this
// returns are dropped through item.Dispatch.Drop with Ops as the caller and the
// hold lifts because the row reads the mark.
//
// [Search] has no caller: it is the step at the failed exit of a revert's own
// window, one per pass, and the composition holding the batch's deploy record is
// not built.
//
// Not built: [Watching.NamesAHazardousOperation] is read here and supplied by
// the caller composing the call; a caller reading area's own hazard severity
// and setting it there is not built, so every call reads the zero value — no
// service names one — until it is. The environment's own record of the targets
// a service with none authored runs on is not read: [targetsOrDefault] stands
// the environment in for the whole set until it is, and [unmeasurable] and
// the fallback in open.go say the same about a service's own reachability
// fields. The deploy record does not name a control per target: it carries one
// control target and one control release for the whole deploy, so what says
// which targets carry one is the count of control instances on each target's
// row, and the build every control runs is the one control release. This
// package asks the deployer to start a control on every target of the window
// all the same, and a record whose target rows name no control instances leaves
// the teardown asked for on every target the window was allocated over.
//
// A stop between the first record an exit writes and the close is finished at
// the exit that began, which the window's own record carries: every exit that
// writes more than one record — the failed exit's rollback, and the passed and
// timed-out exits' control teardown and search-deploy ending, each ahead of the
// close — records it before that first record. A skipped window's own close
// carries none of that, being a rollback's own accounting rather than a
// reading. What the second evaluation does not recover is an incident the
// interrupted attempt had not yet raised where the rollback it did perform has
// since stopped the crossing — the window closes failed with the release
// returned and no incident, a crossing being held nowhere but in the pass that
// took it.
//
// What defines it:
// ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md
// (C1916, C1917, C1921, C1926, C1927, C1928, C1934, C1935, C1936, C1937, C1938,
// C1941, C1942, C1943, C1945, C1946, C1947, C1948, C1960, C1964, C1965, C1966,
// C1969, C1972, C1973, C1975, C1976, C1977, C1979) for the control, its
// fallback, the quantities, and what the health monitor is;
//
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md
// (C1980, C1981, C1982, C1983, C1984, C1985, C1986, C1989, C1991, C1992, C1993,
// C1994, C1995, C1996, C1997, C2000, C2001, C2002, C2003, C2004, C2005, C2006,
// C2008, C2010, C2011, C2012, C2013, C2014, C2015, C2016, C2017, C2019, C2020,
// C2021, C2022, C2023, C2025, C2026, C2027, C2028, C2029, C2030, C2031, C2032,
// C2037, C2038) for the window, its four exits, the power, and the one window
// that reads more than the producer's own numbers;
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// (C2040, C2041, C2046, C2047, C2048, C2049, C2050, C2051, C2053, C2062, C2067,
// C2068, C2069, C2070, C2072, C2073, C2074, C2078, C2079) for the window
// limit, the rollback's target, and what a rollback undoes;
//
// ../../end-goal/how-the-factory-works/08-operations/04-after-the-analysis-window.md
// (C2080, C2081) for the intent a later crossing writes;
//
// ../../end-goal/how-the-factory-works/08-operations/06-incidents.md (C2095,
// C2096, C2097, C2098, C2100, C2101, C2102) for the incident, what it names,
// and its deduplication;
//
// ../../end-goal/how-the-factory-works/08-operations/05-service-level-objectives.md
// (C2082, C2083, C2084, C2085, C2086, C2087, C2088, C2089, C2090, C2093,
// C2094) for the error budget, the burn rate, the hold, the two items the
// budget hold passes, and the intent the objective raises;
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md (C2114, C2115,
// C2120, C2121, C2122, C2130, C2131, C2135, C2136) for the two kinds of wait
// and which of them fires at any hour; and
// ../../end-goal/how-the-factory-works/06-releases/06-rollback.md (C1729,
// C1730, C1731, C1733, C1742, C1743, C1746, C1747, C1748, C1751, C1754, C1755,
// C1761) for what a rollback is, what its record names, and the page where it
// finds nothing to return to.
//
// The exit with no rollback target taking the page is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/01-a-service-that-already-exists.md
// (C0632); a failing rollout rolling back inside its window is
// ../../end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md
// (C0904).
//
// A held-out opening making passed unavailable, running to the cap, is
// ../../end-goal/how-the-factory-works/04-risk-score/02-how-it-learns.md
// (C1375); no release to return to, or a mismatch, taking only a page, is
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/README.md
// (C1724);
//
// a standing mismatch stopping the rollback it would perform, and the
// failed exit raising the intent and paging instead, are
// ../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md
// (C2176, C2177);
//
// [passedReachable] never coarsening the size in force, withholding the
// passed exit instead, and such a window running to its cap, are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md
// (C2222, C2223, C2224);
//
// history.go stating the reading that never closes is
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// (C2598);
//
// and [HealthMonitor.Watch] reading open windows off the records, evaluating
// every open window again, with the close as the exit's last step, are
// ../../end-goal/one-process.md (C2762, C2763, C2764).
package healthmonitor

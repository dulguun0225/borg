// Package localtarget is a [targetseam.Target] that runs the software as a
// local process, one per service, in one directory.
//
// # The files
//
// local.go is the process: [Local] and [New], [Local.Dir], the seam operations
// [Local.Deploy], [Local.Stop] and [Local.ReadRunning], [Local.DrainWait] with
// [DefaultDrainWait], the files [RunningFile], [SignalFile] and [ExchangeFile]
// with the [SignalEnv], [ExchangeEnv], [DeployEnv] and [WayInEnv] variables
// that name what a started process is told, and [ErrBuildNotLocal] and
// [ErrServiceNotLocal]. store.go is the service's store: [DataDir],
// [HistoryFile], [SchemaScript] and [SnapshotDir], the operations
// [Local.ApplySchemaChange] and [Local.Snapshot], and [ErrNoSchemaScript],
// [ErrSnapshotUnverified] and [ErrNameNotLocal]. traffic.go is the two
// operations this platform cannot perform, [Local.ShiftTraffic] and
// [Local.SetInstanceCount], with [ErrNoShare] and [ErrOneInstance]. The tests
// are local_test.go and store_test.go, which need a directory and no database.
//
// [New] takes a directory, and there is one target per target rather than one
// per environment or one per install: an environment names the addresses a
// deploy into it is performed against, plural and ordered, and on this platform
// an address is a directory — so an environment with three targets is three
// Locals, reached by the deployer in the environment's order, and a candidate's
// own environment gets one of its own.
//
// The deployable binary for a build is placed in the directory before Deploy is
// called, named exactly by the build string. [Local.Deploy] drains whatever
// runs for the service — asking it to end and giving it [Local.DrainWait] to
// finish what it holds — starts dir/<build>, and reports the drain, or a cut
// where the instance did not end in time. [Local.Stop] ends the process outright
// and clears what says it runs, and [Local.ReadRunning] reports the build whose
// process is still alive, the digest of the artifact it was started from, the
// one instance this platform runs, and the service's schema history. A dead
// process reads as nothing running. The directory is a boundary and not a
// prefix: a build that is not a local path is [ErrBuildNotLocal] rather than a
// path joined and run, and the same check holds for the service name, the
// change and the snapshot name, each of which is part of a filename here too.
// What crosses the seam is the build and never the release.
//
// # What this platform cannot do
//
// It moves a process rather than traffic, so it serves no share:
// [Local.ShiftTraffic] refuses with [ErrNoShare] and [Local.SetInstanceCount]
// answers a count of one and refuses every other with [ErrOneInstance]. Neither
// returns as though it had acted, because a shift reported as performed would be
// a rollout recorded as having compared two builds while one of them served no
// request. An environment record declares this per target, so the score picks the
// row without a control rather than one the deployer discovers it cannot perform;
// where a target was declared as serving a share, the deployer writes the strategy
// it performed beside the one that was picked and the refusal shows there.
//
// # The service's store
//
// The store is a directory, dir/<service>.data, and the schema history the
// deployer keeps in it is [HistoryFile], one line per change applied.
// [Local.ApplySchemaChange] runs the script the service ships for the change
// and appends to the history where the script succeeded, so a change that failed
// to apply is one the next read of the history still lacks; a change the service
// ships no script for is [ErrNoSchemaScript] and is applied by nothing.
// [Local.Snapshot] copies the store, verifies the copy by digest, and returns the
// name and the digest the deploy record then carries; a copy that does not verify
// is removed and [ErrSnapshotUnverified] is returned, a snapshot the target
// cannot take and verify being a deploy not performed. Nothing here deletes a
// snapshot: the seam has no operation for it, and the deploy record's deletion
// field is written by the deployer.
//
// [RunningFile] is where the target records the build it started and its
// process id, and [Local.ReadRunning] reads that file rather than this value's
// memory: a read operation on the seam that only its own writer can answer is
// not a read operation, so two [Local] values over one directory are two views
// of one place and a restarted factory process reads what its predecessor
// started.
//
// [SignalFile] and [ExchangeFile] are the two files the started process writes,
// named to it through the [SignalEnv] and [ExchangeEnv] environment variables:
// one line per unit of work into the first — the time the unit finished, a tab,
// and the outcome, which is a shape this package neither writes nor reads — and
// one document per unit of work
// into the second. One file per build of each, so a release's counts are told
// apart from those of the build that ran there before it. The health monitor
// and enforcement each read one through an interface knowing neither. Beside
// them the process is told the deploy record's identity through [DeployEnv],
// which is what tells the instances one deploy placed from the instances of the
// same build another placed, and the way-in token through [WayInEnv] where the
// deployment carries one.
//
// The seam requires a credential reference on every operation and this target
// refuses its absence, but it never resolves the name: nothing sits behind this
// one but the machine itself, which has no door to present a credential to. It
// requires a principal too, and this target records it nowhere and reads nothing
// in it.
//
// Two deploys of one service into one directory at once are a race: the drain,
// the start, and the write of what runs are three steps and nothing guards
// them. A process this value did not start has nobody waiting on it, so a
// crashed one may sit in the process table as a zombie and answer signal 0,
// which is the one way this target reports what was started rather than what
// runs.
//
// Who may write what: this package owns no table and writes no record. The
// deploy record is package deploy's, written by the deployer, which is what
// calls this seam. What this package writes is the file that says what runs, the
// schema history, and a snapshot; the process it starts writes the other two,
// and none of them is a record of the factory's.
//
// What defines it: seam 4 of "Security comes last" in
// ../../end-goal/deferred.md#security-comes-last — the deployer reaches a deploy
// target through a small set of named operations and no agent reaches one at
// all, the seam being where policy attaches later. The replacement that drains
// and the platform that serves no share are
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md; the
// schema change, its script and the snapshot before a destructive one are
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/01-a-schema-change.md.
// What reads [Local.ReadRunning] from outside the factory is
// ../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md, the
// quantity the started process emits is
// ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md, the
// exchange document a consumer contract is decided against is
// ../../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md,
// and the way-in token handed to a deployed service is seam 5 of
// ../../end-goal/deferred.md#security-comes-last.
package localtarget

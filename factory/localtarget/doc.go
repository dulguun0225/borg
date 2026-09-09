// Package localtarget is a [targetseam.Target] that runs the software as a
// local process, one per service, in one directory.
//
// # The files
//
// local.go is the process: [Local] and [New], [Local.Dir], the seam operations
// [Local.Deploy], [Local.Stop] and [Local.ReadRunning], the files
// [RunningFile], [SignalFile], [ExchangeFile]
// and [WayInSocket] with the [SignalEnv], [ExchangeEnv] and [DeployEnv]
// variables that name what a started process is told, and [ErrBuildNotLocal] and
// [ErrServiceNotLocal]. store.go is the service's store: [DataDir],
// [HistoryFile], [SchemaScript] and [SnapshotDir], the operations
// [Local.ApplySchemaChange], [Local.Snapshot] and [Local.DeleteSnapshot], and
// [ErrNoSchemaScript],
// [ErrSnapshotUnverified], [ErrSnapshotGone] and [ErrNameNotLocal]. traffic.go is the two
// operations this platform cannot perform, [Local.ShiftTraffic] and
// [Local.SetInstanceCount], with [ErrNoShare] and [ErrOneInstance]. The tests
// are local_test.go, store_test.go and snapshot_test.go, which need a
// directory and no database: store_test.go is the schema history, the drain
// and the cut a replacement reports, and the two operations this platform
// refuses; snapshot_test.go is the copy taken before a destructive change,
// verified, and deleted through the seam.
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
// runs for the service — asking it to end and waiting for it to finish what it
// holds, however long that takes — starts dir/<build>, and reports the drain.
// Nothing here ends an instance that is still finishing a request: neither
// rollout row drops one, so the wait is as long as the longest request and a
// caller unwilling to wait cancels the context, which is an error and no
// replacement reported. [Local.Stop] drains the same way, reports the same
// drain, and clears what says it runs, and
// [Local.ReadRunning] reports the build whose process is still alive, the
// digest of the artifact it was started from, the one instance this platform
// runs, and the service's schema history. A dead
// process reads as nothing running. The directory is a boundary and not a
// prefix: a build that is not a local path is [ErrBuildNotLocal] rather than a
// path joined and run, and the same check holds for the service name, the
// change and the snapshot name, each of which is part of a filename here too.
// A deployment names the build and never the release; the release crosses
// only on a schema change, as the history row's field naming what shipped it.
//
// # What this platform cannot do
//
// It moves a process rather than traffic, so it serves no share:
// [Local.ShiftTraffic] refuses with [ErrNoShare] and [Local.SetInstanceCount]
// answers a count of one and refuses every other with [ErrOneInstance]. Two
// things follow. One is that the row with a control is unavailable here, which
// the environment record declares per target, so every deploy here goes
// without a control rather than the deployer discovering it. The other is
// that the fast rollback — traffic
// shifted onto the instances of the release returned to — cannot be performed
// here at all: this platform runs one process per service and keeps no second
// fleet, so a rollback here is the deployer's slow way, a redeploy of a binary
// still in this directory. Neither
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
// deployer keeps in it is [HistoryFile], a file inside that directory so that a
// snapshot of the store carries it: one line per change applied — the change,
// its checksum, what it did to the store, the release that shipped it wherever
// one exists, the build it was applied under, which every row names, and
// whether the deployer applied it or found it applied.
// [Local.ApplySchemaChange] runs the script the service ships for the change
// and appends to the history where the script succeeded, so a change that failed
// to apply is one the next read of the history still lacks. The two are not one
// transaction and this store has none to put them in — a directory and a script
// over it — which is the case the design's "where the engine allows one" names.
// A change that destroys stored data is applied only after the copy it names has
// been verified against what is on disk here, which is [ErrSnapshotGone] where
// the copy is not there and [ErrSnapshotUnverified] where it holds something
// else; a change marked
// found applied is written into the history and run against nothing, which is
// what the deploy of an adopted service's first release asks for; a change the service
// ships no script for is [ErrNoSchemaScript] and is applied by nothing.
// [Local.Snapshot] copies the store, verifies the copy by digest, and returns the
// name and the digest the deploy record then carries; a copy that does not verify
// is removed and [ErrSnapshotUnverified] is returned, a snapshot the target
// cannot take and verify being a deploy not performed. [Local.DeleteSnapshot]
// removes a copy, which is what the deployer performs at the end of the
// service's snapshot retention and at an owner's call from Ops; a copy that is
// not there is not an error, and the deploy record's deletion field is written
// by the deployer beside this call.
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
// same build another placed.
//
// The way in the factory injected into the build is told three things: the
// way-in token, which arrives as one of the deployment's configuration values
// and is put in front of the process the way every other value is, and — where
// the deployment names an entrance — that entrance and [WayInSocket], a socket
// in this directory named by the service the way [RunningFile] is, whose own
// comment says what bounds its length. The variable names are package wayin's,
// the shipped source being what reads them, and this target sets the two it
// owns and spells neither of them itself. A deployment naming no entrance is
// told neither, and the way in in that build listens nowhere.
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
// ../../end-goal/deferred.md#security-comes-last (C0096, C0099, C0105, C0110) —
// the deployer reaches a deploy target through a small set of named operations
// and no agent reaches one at all, the seam being where policy attaches later.
// The replacement that drains and the platform that serves no share are
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md
// (C0914); the schema change, its script and the snapshot before a destructive
// one are
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/01-a-schema-change.md
// (C1664, C1670, C1672, C1675, C1678, C2996) — a row a deploy naming no release
// writes standing on the build, and the copy deleted at the end of the
// service's retention — and the history's own row, with the release that
// shipped each change and the mark that says the store arrived carrying it, is
// ../../end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md
// (C1861, C1862, C1867, C1868, C1869).
//
// What reads [Local.ReadRunning] from outside the factory is
// ../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md, the
// quantity the started process emits is
// ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md
// (C1953), the exchange document a consumer contract is decided against is
// ../../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md,
// and the way-in token handed to a deployed service, beside the entrance it is
// presented at, is seam 5 of ../../end-goal/deferred.md#security-comes-last.
package localtarget

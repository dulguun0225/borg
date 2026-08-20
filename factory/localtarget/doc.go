// Package localtarget is a [targetseam.Target] that runs the software as a
// local process, one per service. It is what the demonstrations deploy
// against: a straight deploy through the seam against a target that runs the
// software, per ../../roadmap.md#m1--one-change-ships.
//
// [New] takes a directory, and there is one target per environment rather than
// one per install — an environment names the addresses a deploy into it is
// performed against, and on this substrate an address is a directory, so a
// candidate's own environment gets a target of its own.
//
// The deployable binary for a build is placed there before Deploy is called,
// named exactly by the build string; Deploy kills whatever runs for the service
// and starts dir/<build>, Stop kills the process and clears what says it runs, and
// ReadRunning reports the build whose process is still alive — a dead one reads as
// nothing running. The directory is a boundary and not a prefix: a build that is not
// a local path is [ErrBuildNotLocal] rather than a path joined and run, and the same
// check holds for the service name, which is part of a filename here too.
//
// # What is running is on disk, because a second process has to read it
//
// [RunningFile] is where the target records the build it started and its process
// id, and [Local.ReadRunning] reads that file rather than this value's memory. It
// was memory until 2026-08-19, and the reconciler is what made that impossible:
// the reconciler is one process outside the factory that reads what is actually
// running on each production target, so a target whose answer lived in the
// factory's own address space could not be read by the one thing the design has
// read it. A read operation on the seam that only its own writer can answer is not
// a read operation.
//
// What that buys beside the reconciler is that two [Local] values over one
// directory are two views of one place rather than two places, and that a factory
// process which restarts reads what its predecessor started.
//
// # The two files the software writes
//
// Deploy starts the process knowing [SignalFile] through the [SignalEnv]
// environment variable and [ExchangeFile] through [ExchangeEnv], and the software
// the factory wrote appends one line per unit of work to the first and one document
// per unit of work to the second. That is the substrate wiring observability rather
// than the factory doing it: what emits them is the build, where they land is the
// target's arrangement, and the comparison and enforcement each read one through an
// interface knowing neither. One file per build of each, so a release's counts are
// told apart from those of the build that ran there before it — which is what the
// comparison's baseline is, and what makes a candidate's own documents the ones its
// consumers' declarations are decided against.
//
// Two files and not one. The signal is what the comparison counts and the exchange
// is what a predicate is decided against; one format carrying both would make every
// reader of either parse the other's, and it would rewrite a mechanism a milestone
// already built.
//
// A service the factory did not write writes nothing into either file, so it
// cannot be watched and its consumers' declarations cannot be decided against it —
// the adopted-service case ../../end-goal/deferred.md sequences away, one milestone
// on and now costing two things rather than one.
//
// What crosses the seam is the build and never the release: a release is the name
// a build has on master, and a candidate deployed to its own environment has no
// such name yet. targetseam's own declaration says the same thing once.
//
// # The credential is carried and unread
//
// The seam requires a credential reference on every operation and this target
// refuses its absence, but it never resolves the name: the seam passes the
// name, whatever sits behind the seam resolves it at the moment it connects,
// and nothing sits behind this one but the machine itself, which has no door
// to present a credential to. Requiring what it will not read keeps the
// caller shaped for every real target; the cost is one made-up reference in
// the demonstration.
//
// # What a local process is not
//
// Two deploys of one service into one directory at once are still a race: the
// stop, the start, and the write of what runs are three steps and nothing guards
// them, and the caller is the one crude path the surfaces are deferred with, which
// deploys one thing at a time. The kill is a kill, not a graceful shutdown, and
// Deploy does not wait for the old process to release anything — a port, a file —
// before the new one starts.
//
// A process this value did not start has nobody waiting on it, so when it exits it
// may sit in the process table as a zombie and answer signal 0 as though it were
// alive. So a build started by an earlier factory run and since crashed can read as
// running until something reaps it, which is the one way this target reports what was
// started rather than what runs.
//
// Who may write what: this package owns no table and writes no record. The
// deploy record is package deploy's, written by the component that calls this
// seam. What it does write is two files in its own directory — what runs, and the
// file the started process emits its quantity into — and neither is a record of the
// factory's.
//
// What defines it: seam 4 of "Security comes last" in
// ../../end-goal/deferred.md#security-comes-last — an agent reaches an
// environment through a small set of named operations, the seam being where
// policy attaches later — and the M1 demonstration in
// ../../roadmap.md#m1--one-change-ships, which needs a target that runs the
// software so the straight deploy ships something. What reads
// [Local.ReadRunning] from outside the factory is
// ../../end-goal/how-humans-do-it/08-operations.md#the-reconciler, and the
// quantity the started process emits is
// ../../end-goal/how-humans-do-it/08-operations.md#the-health-signal, and the
// exchange document a consumer's declaration is decided against is
// ../../end-goal/how-humans-do-it/07-contracts.md#what-a-consumer-declares.
package localtarget

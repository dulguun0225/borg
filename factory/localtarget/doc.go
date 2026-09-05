// Package localtarget is a [targetseam.Target] that runs the software as a
// local process, one per service, in one directory.
//
// local.go is the whole of it: [Local] and [New], the three seam operations
// [Local.Deploy], [Local.Stop] and [Local.ReadRunning], the three files
// [RunningFile], [SignalFile] and [ExchangeFile] with the [SignalEnv] and
// [ExchangeEnv] variables that name the last two to the started process, and
// [ErrBuildNotLocal]. The tests are local_test.go, which needs a directory and
// no database.
//
// [New] takes a directory, and there is one target per environment rather than
// one per install: an environment names the addresses a deploy into it is
// performed against, and on this substrate an address is a directory, so a
// candidate's own environment gets a target of its own.
//
// The deployable binary for a build is placed in the directory before Deploy is
// called, named exactly by the build string. [Local.Deploy] kills whatever runs
// for the service and starts dir/<build>, [Local.Stop] kills the process and
// clears what says it runs, and [Local.ReadRunning] reports the build whose
// process is still alive — a dead one reads as nothing running. The directory
// is a boundary and not a prefix: a build that is not a local path is
// [ErrBuildNotLocal] rather than a path joined and run, and the same check
// holds for the service name, which is part of a filename here too. What
// crosses the seam is the build and never the release.
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
// one line per unit of work into the first and one document per unit of work
// into the second. One file per build of each, so a release's counts are told
// apart from those of the build that ran there before it. The health monitor
// and enforcement each read one through an interface knowing neither.
//
// The seam requires a credential reference on every operation and this target
// refuses its absence, but it never resolves the name: nothing sits behind this
// one but the machine itself, which has no door to present a credential to.
//
// Two deploys of one service into one directory at once are a race: the stop,
// the start, and the write of what runs are three steps and nothing guards
// them. The kill is a kill and Deploy does not wait for the old process to
// release a port or a file. A process this value did not start has nobody
// waiting on it, so a crashed one may sit in the process table as a zombie and
// answer signal 0, which is the one way this target reports what was started
// rather than what runs.
//
// Who may write what: this package owns no table and writes no record. The
// deploy record is package deploy's, written by the component that calls this
// seam. What this package writes is the file that says what runs; the process
// it starts writes the other two, and none of the three is a record of the
// factory's.
//
// What defines it: seam 4 of "Security comes last" in
// ../../end-goal/deferred.md#security-comes-last — an agent reaches an environment
// through a small set of named operations, the seam being where policy attaches
// later. What reads [Local.ReadRunning] from outside the factory is
// ../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md, the
// quantity the started process emits is
// ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md, the
// exchange document a consumer contract is decided against is
// ../../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md,
// and the service the factory did not write, which writes neither file, is
// ../../end-goal/deferred.md.
package localtarget

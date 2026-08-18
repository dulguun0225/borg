// Package localtarget is a [targetseam.Target] that runs the software as a
// local process, one per service. It is what M1's demonstration deploys
// against: a straight deploy through the seam against a target that runs the
// software, per ../../roadmap.md#m1--one-change-ships.
//
// [New] takes a directory. The deployable binary for a release is placed
// there before Deploy is called, named exactly by the release string; Deploy
// kills whatever runs for the service and starts dir/<release>, Stop kills
// the process and forgets it, and ReadRunning reports the release whose
// process is still alive — a dead one reads as nothing running. The directory
// is a boundary and not a prefix: a release that is not a local path is
// [ErrReleaseNotLocal] rather than a path joined and run.
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
// Not safe for concurrent use — the caller is M1's one crude path, and
// nothing guards the process table. What is running is held in memory and
// nowhere else, so a factory process that restarts reads nothing running
// while the processes it started may still run; the reconciler that would
// read the machine and raise that disagreement is M4. The kill is a kill, not
// a graceful shutdown, and Deploy does not wait for the old process to
// release anything — a port, a file — before the new one starts.
//
// Who may write what: this package owns no table and writes no record. The
// deploy record is package deploy's, written by the component that calls this
// seam.
//
// What defines it: seam 4 of "Security comes last" in
// ../../end-goal/deferred.md#security-comes-last — an agent reaches an
// environment through a small set of named operations, the seam being where
// policy attaches later — and the M1 demonstration in
// ../../roadmap.md#m1--one-change-ships, which needs a target that runs the
// software so the straight deploy ships something.
package localtarget

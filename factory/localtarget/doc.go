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
// and starts dir/<build>, Stop kills the process and forgets it, and ReadRunning
// reports the build whose process is still alive — a dead one reads as nothing
// running. The directory is a boundary and not a prefix: a build that is not a
// local path is [ErrBuildNotLocal] rather than a path joined and run.
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
// Not safe for concurrent use — the caller is the one crude path the surfaces
// are deferred with, which deploys one thing at a time, and nothing guards the
// process table. What is running is held in memory and
// nowhere else, so a factory process that restarts reads nothing running
// while the processes it started may still run; the reconciler that would
// read the machine and raise that disagreement is M4. The kill is a kill, not
// a graceful shutdown, and Deploy does not wait for the old process to
// release anything — a port, a file — before the new one starts.
//
// Who may write what: this package owns no table and writes no record. The
// deploy record is package deploy's, written by the component that calls this
// seam. What is running is per target and held in this process's memory, so a
// candidate environment torn down by one process leaves nothing for another to
// stop.
//
// What defines it: seam 4 of "Security comes last" in
// ../../end-goal/deferred.md#security-comes-last — an agent reaches an
// environment through a small set of named operations, the seam being where
// policy attaches later — and the M1 demonstration in
// ../../roadmap.md#m1--one-change-ships, which needs a target that runs the
// software so the straight deploy ships something.
package localtarget

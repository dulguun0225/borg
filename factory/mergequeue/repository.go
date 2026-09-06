package mergequeue

import (
	"context"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/item"
)

// Repository is everything the queue needs done to the service's repository and
// the candidate's environment, which the queue does not reach itself. Whatever
// composes the deployer and the build runner implements it: the queue orders
// merges, reads master, and writes records, and reaches no deploy target.
type Repository interface {
	// Head is master's head, read from the version control system that holds it
	// rather than derived from a record. The queue reads it at every start and
	// before every mint.
	Head(ctx context.Context, serviceID string) (string, error)
	// Holds is whether master holds one commit, which is the other direction of
	// the same reading: a release record naming a commit master does not hold is
	// git restored behind the graph.
	Holds(ctx context.Context, serviceID, commit string) (bool, error)
	// Reverify builds the candidate branch onto master plus every candidate
	// ahead of it, recomposes the candidate's environment, puts that build on
	// it, and decides the criteria there. ahead is the speculation — the members
	// in front of this one, in the queue's order — and is empty for the first
	// member of a pass and for a member re-verified against the master that
	// actually resulted.
	//
	// An error is an infrastructure failure and stops the run; a candidate that
	// failed on its merits is a [Verified] with Passed false and Why saying
	// what.
	Reverify(ctx context.Context, it item.Item, ahead []item.Item) (Verified, error)
	// Confirm is the confirming run: the criteria the re-verification failed,
	// decided once more over the re-verification's own build and composition,
	// once and never until green. It is asked only where the re-verification
	// failed, and what it answers is which of the three readings the rejection
	// takes.
	Confirm(ctx context.Context, it item.Item, verified Verified) (Confirmation, error)
	// FastForward moves the service's master to the commit the re-verification
	// produced, and refuses anything that is not a fast-forward.
	FastForward(ctx context.Context, it item.Item, commit string) error
	// VerifyCommit builds a commit a human accepted and re-verifies it as a
	// candidate is re-verified, the contract checks included. It names no item,
	// there being none: the commit reached master by another path.
	VerifyCommit(ctx context.Context, serviceID, commit string) (Verified, error)
}

// Verified is what a re-verification produced: the commit the candidate branch
// reached once master was merged into it, the build made from that commit, and
// whether every pre-merge check decided against the candidate-environment run
// passed.
//
// A re-verification that changed nothing — master already an ancestor of the
// candidate — names the build already in force rather than a new one: a rebuild
// is a new build, and nothing was rebuilt.
//
// Why is what failed, in words a human reads on the rejection row, and is empty
// where it passed. A merge conflict, a criterion that failed, a breaking
// contract diff, and a consumer contract the candidate does not satisfy all
// arrive here as a candidate that failed its own re-verification, which is the
// same disposition for several reasons — so the reason is on the row.
//
// Forms is what the re-verified build publishes, derived from the checkout the
// re-verification produced. The queue does not reach a checkout, so the
// derivation is the deployer's and the write is the queue's — which is the same
// division the criteria already have, where the agent decides them on the
// environment and the queue reads what it produced.
type Verified struct {
	Commit  string
	BuildID string
	Passed  bool
	Why     string
	// FailedCriteria is the criteria in force this run did not pass, and is what
	// the confirming run is over. It is empty where the candidate failed for a
	// reason no criterion decided — a merge conflict, a breaking contract diff —
	// which is a failure with no criterion to run again.
	FailedCriteria []string
	// Composition is what the environment this run was performed against was
	// composed from, and ApprovedComposition is what the run that passed at
	// Merge to master was composed from. Both are copied here by the component
	// that performed the two runs and wrote both onto the builds: comparing them
	// is the whole of how the second reading is told from the third, and a queue
	// handed one of them and a string it had to parse would be a second place
	// spelling the deployer's encoding.
	Composition         environment.Composition
	ApprovedComposition environment.Composition
	Forms               []contract.Form
}

// Confirmation is what the confirming run produced: the criteria the
// re-verification failed, decided once more over its own build and composition.
//
// Disagreed is the criteria that answered differently the second time, which is
// the disagreement over one build that makes a criterion undecided. Repeated is
// the criteria that failed again, and a failure that repeats is real.
type Confirmation struct {
	Disagreed []string
	Repeated  []string
	// Why is what the confirming run found, in words a human reads on the
	// rejection row, and is empty where nothing failed again.
	Why string
}

// Numbers is the second reading a mint takes: the highest number among the
// releases of one service that the health monitor's store names. It is an
// interface because that store is outside the recovery unit and is not the
// factory's records — it holds what production emitted while the records were
// behind, which is what the numbers a restore lost are recovered from.
type Numbers interface {
	// HighestSeen is that number, and 0 where the store names no release of the
	// service.
	HighestSeen(ctx context.Context, serviceID string) (int64, error)
}

// NoNumbersSeen is what a factory composed with no reader of the health
// monitor's store uses: the reading is the records' alone. It is a value rather
// than a nil interface so that a factory composed without one says so.
//
// What it costs is the one thing the second reading buys: after a restore that
// lost releases, the next mint reuses their numbers, and two builds answer to
// one name until the drift detector's digest comparison holds the service.
type NoNumbersSeen struct{}

// HighestSeen is never a number.
func (NoNumbersSeen) HighestSeen(context.Context, string) (int64, error) { return 0, nil }

// DesignSystem is the one comparison a re-verification makes that no criterion
// reads: whether the two builds' design system constraint records differ on a
// component or a token the candidate's build uses. It is an interface because
// the constraint record is not built — nothing in the factory owns a design
// system constraint's components and tokens yet — so what reads two records and
// answers is supplied by the composition.
type DesignSystem interface {
	// Differs is asked only where the two builds name two different records. A
	// build naming no record compares as nothing, which the queue decides
	// before it asks.
	Differs(ctx context.Context, approvedRecordID, reverifiedRecordID, buildID string) (bool, error)
}

// EveryMoveDiffers is what a factory with no reader of the constraint records is
// composed with: two records that are not the same record differ. It is the safe
// direction of the two — an item drafted before a design system move and merging
// after it is what the comparison exists to stop — and it is a value rather than
// a nil interface so that a factory composed without a reader says so.
//
// What it costs is a candidate rejected for a move that touched no component or
// token its build uses, until the constraint record exists to read.
type EveryMoveDiffers struct{}

// Differs is always so.
func (EveryMoveDiffers) Differs(context.Context, string, string, string) (bool, error) {
	return true, nil
}

// Backlog is the reading the backlog cap's stop is decided against. It is an
// interface because what it counts is a walk the queue does not make: the
// service's newest rollback, whether its revert has shipped, the releases that
// rollback skipped, and the releases merged while it stands. The backlog cap
// itself is a field of the service record beside the window limit, which is not
// built, so the cap in force arrives here with the count rather than being read
// from a parameter.
type Backlog interface {
	// Behind is that reading for one service.
	Behind(ctx context.Context, serviceID string) (Waiting, error)
}

// Waiting is how many releases wait behind a rollback hold on one service, the
// cap in force, and the item the stop does not catch.
type Waiting struct {
	// Standing is whether a rollback hold stands on the service at all. Where it
	// does not, nothing waits behind anything and the count is not read.
	Standing bool
	// Releases is every release the revert's deploy would deliver: the ones the
	// rollback skipped and the ones merged while it stands alike.
	Releases int
	// Cap is the backlog cap in force, which is the window limit where an owner
	// authored no cap of its own.
	Cap int
	// RevertItemID is the item whose candidate the stop does not catch. The hold
	// lifts only when the revert ships, so a stop that held it would never end.
	RevertItemID string
}

// Reverts is whether one item is a revert. The queue does not read it itself:
// nothing on the item says it is one, and the release a revert undoes is
// reachable through the intent the item was decomposed from rather than through
// any field the queue holds, so the walk from a rollback to the item that undoes
// it is the caller's.
//
// It is one of the two exceptions a halt takes, and it is a reading of its own
// because a revert passes whatever raised it: the health monitor at a failed
// exit, or a named human at Ops asking for one.
type Reverts interface {
	// IsARevert is that reading for one item.
	IsARevert(ctx context.Context, it item.Item) (bool, error)
}

// NoRevertKnown is what a factory composed with no reader of the reverts uses:
// no item is known to be one. It is a value rather than a nil interface so that
// a factory composed without one says so.
//
// What it costs is the part of the halt's first exception no other reading
// covers: a revert whose intent a named human at Ops raised is stopped while a
// halt stands, where one the health monitor raised passes under the second
// exception.
type NoRevertKnown struct{}

// IsARevert is never so.
func (NoRevertKnown) IsARevert(context.Context, item.Item) (bool, error) { return false, nil }

// NoBacklog is what a factory composed with no reader of the rollbacks uses:
// nothing waits behind anything, so the stop never stands. It is a value rather
// than a nil interface so that a factory composed without one says so.
//
// What it costs is the whole of what the cap buys: a slow revert leaves the
// service's merges running and its one deploy delivering a pile nothing bounds.
type NoBacklog struct{}

// Behind is never a rollback hold.
func (NoBacklog) Behind(context.Context, string) (Waiting, error) { return Waiting{}, nil }

package window

import (
	"errors"
	"fmt"
	"time"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

// Exit is how a window closed. There are four and a window closes at exactly
// one of them.
type Exit string

const (
	// ExitFailed is the comparison crossing the boundary against the release. What
	// follows is a rollback with no human involved.
	ExitFailed Exit = "failed"
	// ExitPassed is the comparison ruling out a change of the size worth
	// catching. The window closed early, on evidence.
	ExitPassed Exit = "passed"
	// ExitTimedOut is neither, by the cap. The window closed unresolved, and on a
	// service too quiet to receive the traffic a comparison needs this is where
	// every window ends — weak protection, reported as weak.
	ExitTimedOut Exit = "timed_out"
	// ExitSkipped is a rollback aimed below this release having undone it. The
	// release is neither failed nor running, and a release skipped this way is
	// one the factory passed over rather than one it can return to.
	ExitSkipped Exit = "skipped"
)

// Exits is every exit a window may close at. The CHECK in [DDL] lists the same
// four, and TestDDLListsEveryExit fails if the two stop agreeing.
var Exits = []Exit{ExitFailed, ExitPassed, ExitTimedOut, ExitSkipped}

// PassedOrTimedOut reports whether the exit is one of those two, which is the
// pair a rollback's target and the last known-good release are computed from:
// passed ruled a change out, timed out ruled nothing out, and both leave a build
// still serving that the factory can return to.
//
// It names the two exits it admits rather than grouping the three that close
// without failing the release. Skipped closes without failing too and leaves
// nothing running, so a query that admitted all three would name a release whose
// instances the rollback ended; a mechanism that learns from exits admits passed
// alone, because timed out ruled nothing out. Every rule over exits names the
// exits it admits, one at a time.
func (e Exit) PassedOrTimedOut() bool { return e == ExitPassed || e == ExitTimedOut }

var (
	// ErrExitUnknown is returned for an exit outside [Exits].
	ErrExitUnknown = errors.New("window: the exit is none of failed, passed, timed out, skipped")
	// ErrNotFound is returned where no window has the id, the deploy, or the
	// release.
	ErrNotFound = errors.New("window: no window has that")
	// ErrAlreadyClosed is returned by [Writer.Close] for a window that has an
	// exit. A window closes once, at exactly one of four exits.
	ErrAlreadyClosed = errors.New("window: the window is closed already, and a window closes once")
	// ErrOpeningIncomplete is returned by [Writer.Open] for an opening missing
	// something every window has.
	ErrOpeningIncomplete = errors.New("window: the opening is missing something every window has")
	// ErrMeasuresNothingCarriesNoParameters is returned by [Writer.Open] for an
	// opening that both records that it measures nothing and names parameters. A
	// service missing one of the four fields the deployer populates opens a
	// window that records only that, since what the field would have supplied is
	// what a reading needs to exist at all.
	ErrMeasuresNothingCarriesNoParameters = errors.New("window: a window that measures nothing carries no parameters")
	// ErrReadRefused is returned by [Writer.Close] for a read that is not a read
	// of the quantities, and for a skipped close carrying one. Skipped is the
	// exit that is not a reading: a rollback aimed below the release ended the
	// window, so a read there would be a reading nothing performed.
	ErrReadRefused = errors.New("window: the read the window closed on is not a read of the quantities")
	// ErrPassedUnavailable is returned by [Writer.Close] for a passed close on a
	// window that never had that exit available: a held-out release, a release
	// with nothing to compare against, or one whose traffic cannot support the
	// power in force at the size in force inside the cap.
	ErrPassedUnavailable = errors.New("window: the passed exit was not available to this window")
)

// Window is one analysis window as it is stored. At is when it opened, which is
// when the deploy record it was opened over was written.
type Window struct {
	ID       string
	Actor    record.Actor
	At       string
	DeployID string
	// ReleaseID is the release the deploy was of, and is empty on the window over
	// a deploy the search called for, whose record names a build and no release.
	ReleaseID string
	// BuildID is the build the window is watching, on every window.
	BuildID   string
	ServiceID string
	// MeasuresNothing is a service missing one of the four fields the deployer
	// populates on its service record. Such a window records only that, carries
	// no parameters, and ends at once.
	MeasuresNothing bool
	// PassedAvailable is whether the passed exit was reachable at the open. It is
	// false for a release with nothing to be compared against, for a held-out
	// release, and where the traffic cannot support the power in force at the
	// size in force inside the cap — so a window ending at the cap is readable as
	// weak protection rather than as a comparison that ran out of time.
	PassedAvailable bool
	// HeldOut is whether the score selected the item this release came from into
	// its sample. It is on the record because a window that runs to the cap for
	// this reason is not the same window as one that ran to the cap for want of a
	// control, and a reader with only PassedAvailable could not tell them apart.
	HeldOut bool
	// Size and Power are one value per quantity, and Confidence and CapSeconds
	// are one value for the window. All four are what was in force at the open,
	// copied onto the record; doc.go says why they are copied.
	Size       map[gatepolicy.Quantity]float64
	Power      map[gatepolicy.Quantity]float64
	Confidence float64
	CapSeconds float64
	// BoundaryVersion names the construction that turned the size, the
	// confidence, the power and the run length into a boundary and allocated it
	// over the set, which is [boundary.Version] at the open. A mechanism that
	// learns from exits reads exits at one version and none at another.
	BoundaryVersion string
	// Targets is the production targets the rollout was planned to reach, which
	// is the set the boundary was allocated over. It does not move as targets are
	// reached, and a target the rollout never reaches keeps its allocation
	// unspent.
	Targets []string
	// OperationsReadAlone is the operations whose volume can support the power in
	// force at the size in force inside the cap. The rest are pooled into one
	// series per quantity per target and read as one, and a later reader cannot
	// tell a pooled crossing from one operation's without this.
	OperationsReadAlone []string
	// EmissionVersionRelease and EmissionVersionControl are the emission versions
	// each arm's series were read at, and QuantitiesOutside is what was outside
	// this window's set because the two differ.
	EmissionVersionRelease string
	EmissionVersionControl string
	QuantitiesOutside      []gatepolicy.Quantity
	// OwnHistorySize and OwnHistoryRunLength are the size per quantity and the
	// average run length in force for the reading against the service's own
	// recent history, which is one of the other two readings that could fail this
	// release.
	OwnHistorySize      map[gatepolicy.Quantity]float64
	OwnHistoryRunLength float64
	// ThresholdSize and ThresholdRunLength are the same for an explicit threshold
	// a safeguard set, and are empty and nothing where no safeguard set one.
	ThresholdSize      map[gatepolicy.Quantity]float64
	ThresholdRunLength float64
	// PolicyVersion and ScoreVersion are the two versions in force at the open.
	PolicyVersion string
	ScoreVersion  string
	// ClosedOn is the read the window closed on: the four counts per quantity,
	// and the same per target and operation. It is empty while the window is open
	// and at the skipped exit, which is the one close that is not a reading.
	//
	// It is here because an exit nobody can recompute is an exit nobody can argue
	// with. What else it makes possible is arithmetic over the traffic a service
	// actually receives, which is what says whether a size the score is asking
	// for is reachable at all.
	ClosedOn Read
	// FinestSizeReached is the finest size the traffic actually reached per
	// quantity, which is what the score reads to decide the size in force: the
	// size in force is the coarser of what the evidence asks for and this.
	FinestSizeReached map[gatepolicy.Quantity]float64
	// Exit is empty while the window is open.
	Exit Exit
	// ClosedAt is empty while the window is open, and moves with Exit.
	ClosedAt string
}

// Read is what a window closed on. Quantities is the four counts per quantity
// over the whole window, and Series is the same per target and per operation,
// since the comparison is evaluated per target and per operation and a reader
// with the total alone could not tell which series crossed.
type Read struct {
	Quantities map[gatepolicy.Quantity]boundary.Counts `json:"quantities,omitempty"`
	Series     []SeriesCounts                          `json:"series,omitempty"`
}

// SeriesCounts is one series' counts: one quantity, on one target, on one
// operation. The operation is the pooled name where the operation was not read
// alone.
type SeriesCounts struct {
	Target    string              `json:"target"`
	Operation string              `json:"operation"`
	Quantity  gatepolicy.Quantity `json:"quantity"`
	Counts    boundary.Counts     `json:"counts"`
}

// Empty reports whether the read holds nothing at all, which is what the skipped
// exit carries.
func (r Read) Empty() bool { return len(r.Quantities) == 0 && len(r.Series) == 0 }

// Of is one quantity's four counts over the whole window, and the zero counts
// where the window read that quantity not at all.
func (r Read) Of(q gatepolicy.Quantity) boundary.Counts { return r.Quantities[q] }

// Open reports whether the window has not closed.
func (w Window) Open() bool { return w.Exit == "" }

// Comparisons is how many readings the boundary was allocated over: the
// quantities this window carries a size for, on every operation read alone plus
// the pooled one, on every target the rollout was planned to reach. It is
// computed from the fields rather than stored, because a stored count would be a
// second copy of an arithmetic over three lists already on the record.
func (w Window) Comparisons() int {
	targets := len(w.Targets)
	if targets == 0 {
		targets = 1
	}
	// The pooled series is one more reading beside the operations read alone: a
	// regression among the rest still crosses on it, diluted by their combined
	// share.
	operations := len(w.OperationsReadAlone) + 1
	quantities := len(w.Size)
	if quantities == 0 {
		quantities = 1
	}
	return targets * operations * quantities
}

// Boundary is the boundary one quantity of this window is read against: the size
// and the power resolved at the open, the confidence held over
// [Window.Comparisons], and the direction a regression moves that quantity. It
// is false where the window carries no size for the quantity, which is a
// quantity outside this window's set.
func (w Window) Boundary(q gatepolicy.Quantity) (boundary.Boundary, bool) {
	size, carried := w.Size[q]
	if !carried {
		return boundary.Boundary{}, false
	}
	return boundary.Boundary{
		Size:        size,
		Confidence:  w.Confidence,
		Comparisons: w.Comparisons(),
		Worse:       Worse(q),
	}, true
}

// Worse is which direction of a quantity a regression moves it in. The request
// rate is the one a release can move by emitting nothing, so an arm receiving
// materially fewer requests than the other is the crossing; the other three
// cross upward.
func Worse(q gatepolicy.Quantity) boundary.Worse {
	if q == gatepolicy.QuantityRequestRate {
		return boundary.WorseLower
	}
	return boundary.WorseHigher
}

// PastCap reports whether the window has been open longer than its cap, at now.
// It is the one exit that is not a reading of the quantity: the condition a
// window is meant to reach is a traffic volume, and what ends a window that will
// never reach that volume cannot be.
//
// A closed window is never past its cap, whatever the clock says: the question
// is about a window the health monitor is still deciding.
func (w Window) PastCap(now time.Time) (bool, error) {
	if !w.Open() {
		return false, nil
	}
	opened, err := record.ParseTime(w.At)
	if err != nil {
		return false, fmt.Errorf("window: reading when %s opened: %w", w.ID, err)
	}
	return now.Sub(opened).Seconds() >= w.CapSeconds, nil
}

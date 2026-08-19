package window

import (
	"errors"
	"fmt"
	"time"

	"github.com/dulguun0225/borg/factory/record"
)

// Exit is how a window closed. There are four and a window closes at exactly
// one of them.
type Exit string

const (
	// ExitHarm is the comparison crossing the boundary against the release. What
	// follows is a rollback with no human involved.
	ExitHarm Exit = "harm"
	// ExitClean is the comparison ruling out a regression of the size worth
	// catching. The window closed early, on evidence.
	ExitClean Exit = "clean"
	// ExitCap is neither, by the cap. The window closed unresolved, and on a
	// service too quiet to receive the traffic a comparison needs this is where
	// every window ends — weak protection, reported as weak.
	ExitCap Exit = "cap"
	// ExitSwept is a rollback aimed below this release having undone it. The
	// release is neither condemned nor running, and a swept release is one the
	// factory skipped over rather than one it can return to.
	ExitSwept Exit = "swept"
)

// Exits is every exit a window may close at. The CHECK in [DDL] lists the same
// four, and TestDDLListsEveryExit fails if the two stop agreeing.
var Exits = []Exit{ExitHarm, ExitClean, ExitCap, ExitSwept}

// Counts reports whether closing at this exit leaves a release the factory can
// return to, which is what a restore floor and a rollback's target are both
// computed from. Clean and cap count: a release that was never condemned is one
// the factory can return to, and requiring a clean close would leave a service
// too quiet to ever reach one with no target at all. Harm and swept do not —
// the first was condemned, and the second has nothing left running its build.
func (e Exit) Counts() bool { return e == ExitClean || e == ExitCap }

var (
	// ErrExitUnknown is returned for an exit outside [Exits].
	ErrExitUnknown = errors.New("window: the exit is none of harm, clean, cap, swept")
	// ErrNotFound is returned where no window has the id, the deploy, or the
	// release.
	ErrNotFound = errors.New("window: no window has that")
	// ErrAlreadyClosed is returned by [Writer.Close] for a window that has an
	// exit. A window closes once, at exactly one of four exits.
	ErrAlreadyClosed = errors.New("window: the window is closed already, and a window closes once")
	// ErrOpeningIncomplete is returned by [Writer.Open] for an opening missing
	// something every window has.
	ErrOpeningIncomplete = errors.New("window: the opening is missing something every window has")
)

// Window is one watch window as it is stored. At is when it opened, which is
// when the deploy record it was opened over was written.
type Window struct {
	ID        string
	Actor     record.Actor
	At        string
	DeployID  string
	ReleaseID string
	ServiceID string
	// CleanAvailable is whether the clean exit was reachable at the open. It is
	// false for a release with nothing below it to be compared against, whose
	// window can only be condemned by an absolute threshold and can never be
	// cleared early — so a window ending at the cap is readable as weak
	// protection rather than as a comparison that ran out of time.
	CleanAvailable bool
	// Size, Confidence, and CapSeconds are the parameters in force at the open,
	// copied onto the record. doc.go says why they are copied.
	Size       float64
	Confidence float64
	CapSeconds float64
	// Formula names the construction the size and the confidence were read
	// through, which is boundary.Formula at the open.
	Formula string
	// PolicyVersion and ScoreVersion are the two versions in force at the open.
	PolicyVersion string
	ScoreVersion  string
	// Exit is empty while the window is open.
	Exit Exit
	// ClosedAt is empty while the window is open, and moves with Exit.
	ClosedAt string
}

// Open reports whether the window has not closed.
func (w Window) Open() bool { return w.Exit == "" }

// PastCap reports whether the window has been open longer than its cap, at now.
// It is the one exit that is not a reading of the quantity: the condition a
// window is meant to reach is a traffic volume, and what ends a window that will
// never reach that volume cannot be.
//
// A closed window is never past its cap, whatever the clock says: the question is
// about a window the comparison is still deciding.
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

// Opening is what [Writer.Open] is given. It is a struct and not eight
// arguments because three of them are ids and three are shares, and a caller
// that swapped two of either would compile.
type Opening struct {
	DeployID  string
	ReleaseID string
	ServiceID string
	// CleanAvailable is false where the release has no baseline to be compared
	// against, which the caller knows and this package does not.
	CleanAvailable bool
	Size           float64
	Confidence     float64
	CapSeconds     float64
	Formula        string
	PolicyVersion  string
	ScoreVersion   string
}

func (o Opening) validate() error {
	for _, required := range []struct{ what, value string }{
		{"deploy", o.DeployID}, {"release", o.ReleaseID}, {"service", o.ServiceID},
		{"boundary formula", o.Formula}, {"policy version", o.PolicyVersion},
		{"score version", o.ScoreVersion},
	} {
		if required.value == "" {
			return fmt.Errorf("%w: no %s", ErrOpeningIncomplete, required.what)
		}
	}
	if o.Size <= 0 || o.Size > 1 {
		return fmt.Errorf("%w: the size %v is not a share above nothing", ErrOpeningIncomplete, o.Size)
	}
	if o.Confidence <= 0 || o.Confidence >= 1 {
		return fmt.Errorf("%w: the confidence %v is not a share below one", ErrOpeningIncomplete, o.Confidence)
	}
	if o.CapSeconds <= 0 {
		return fmt.Errorf("%w: the cap %v is not above nothing", ErrOpeningIncomplete, o.CapSeconds)
	}
	return nil
}

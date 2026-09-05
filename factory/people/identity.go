package people

import (
	"errors"
	"fmt"
	"slices"
)

// Duty is one of the owner's twelve duties, held as its number. doc.go says
// why the names are not here.
type Duty int

// Duties is the twelve, in order. The CHECK in [DDL] holds the same range,
// and TestDDLHoldsEveryDuty fails if the two stop agreeing.
var Duties = func() []Duty {
	all := make([]Duty, 0, 12)
	for d := Duty(1); d <= 12; d++ {
		all = append(all, d)
	}
	return all
}()

// Obligation is something a human holds that is not one of the twelve,
// because it is substrate rather than work.
type Obligation string

const (
	// ObligationHosting is hosting the factory, which includes bringing it
	// up to date: a self-hosted product is upgraded by whoever runs it.
	ObligationHosting Obligation = "hosting"
	// ObligationDriftDetector is installing the drift detector beside the
	// factory. It is the one obligation this milestone reads: a mismatch
	// belongs to no duty, so the page it fires reaches whoever this record
	// says installed it.
	ObligationDriftDetector Obligation = "driftdetector"
	// ObligationFleet is composing the fleet — the entries the factory
	// dispatches against, the credentials they run on, and giving a fleet
	// proposal a disposition. Nothing reads it until the fleet is built.
	ObligationFleet Obligation = "fleet"
)

// Obligations is every obligation outside the twelve. The CHECK in [DDL]
// lists the same three, and TestDDLListsEveryObligation fails if the two
// stop agreeing.
var Obligations = []Obligation{ObligationHosting, ObligationDriftDetector, ObligationFleet}

var (
	// ErrNotAnOwner is returned for an actor that is not a human. Distributing
	// the twelve is the owner's, and a component doing it would be the
	// factory deciding who holds the factory's obligations.
	ErrNotAnOwner = errors.New("people: the declaration is written by a human")
	// ErrKeyEmpty is returned for a write naming no per-person key.
	ErrKeyEmpty = errors.New("people: a write names the per-person key it is about")
	// ErrHoldingUnknown is returned for a declaration that names neither a
	// duty in range nor an obligation, or that names both.
	ErrHoldingUnknown = errors.New("people: a declaration names one duty or one obligation, and not both")
	// ErrNotFound is returned where no declaration has that id.
	ErrNotFound = errors.New("people: no declaration has that id")
)

// OfDuty is a holding of one duty, for [Writer.Declare].
func OfDuty(duty Duty) Holding { return Holding{Duty: duty} }

// OfObligation is a holding of one obligation outside the twelve.
func OfObligation(obligation Obligation) Holding { return Holding{Obligation: obligation} }

// Holding is which of the two a declaration names. It is one value rather
// than two arguments so that a caller cannot pass both, and [OfDuty] and
// [OfObligation] are what a caller says it with.
type Holding struct {
	Duty       Duty
	Obligation Obligation
}

func (h Holding) validate() error {
	switch {
	case h.Duty != 0 && h.Obligation != "":
		return fmt.Errorf("%w: duty %d and obligation %q", ErrHoldingUnknown, h.Duty, h.Obligation)
	case h.Duty != 0:
		if !slices.Contains(Duties, h.Duty) {
			return fmt.Errorf("%w: duty %d is not one of the twelve", ErrHoldingUnknown, h.Duty)
		}
	case h.Obligation != "":
		if !slices.Contains(Obligations, h.Obligation) {
			return fmt.Errorf("%w: %q is not an obligation outside the twelve", ErrHoldingUnknown, h.Obligation)
		}
	default:
		return fmt.Errorf("%w: it names neither", ErrHoldingUnknown)
	}
	return nil
}

// String is how a holding reads in a message the notifier delivers.
func (h Holding) String() string {
	if h.Obligation != "" {
		return "the obligation of " + string(h.Obligation)
	}
	return fmt.Sprintf("duty %d", h.Duty)
}

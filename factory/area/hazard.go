package area

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Grade is the worst harm the software in an area can do while it behaves as a
// release makes it behave, graded by what the factory can do about that harm
// afterwards.
type Grade string

const (
	// GradeNegligible is an area whose software answers requests and writes its
	// own store, and does nothing outside itself that outlasts a traffic shift.
	GradeNegligible Grade = "negligible"
	// GradeRecoverable is one whose software acts outside itself where a later
	// item corrects what it did.
	GradeRecoverable Grade = "recoverable"
	// GradeIrreversible is one whose software does what nothing the factory
	// ships afterwards corrects.
	GradeIrreversible Grade = "irreversible"
)

// Grades is the three, ordered from the least harm to the most. Their order in
// this list is the comparison [SeverityInForce] makes, so a fourth grade is
// added here and nowhere else.
var Grades = []Grade{GradeNegligible, GradeRecoverable, GradeIrreversible}

// Hazard is the hazard severity an owner declared on one area: the grade, and
// for an irreversible one the operation that carries the harm with the bound the
// owner authored on it.
//
// The zero Hazard is an area that names none, which is what most areas are: the
// value in force for an item is the highest named anywhere on its chain, so
// declaring a finer area never lowers it.
type Hazard struct {
	Grade Grade
	// Operation is the call in the software that does what nothing afterwards
	// corrects: the payout, the send, the erasure, the actuator call.
	Operation string
	// Bound is the count of that operation the service may perform per period.
	Bound float64
	// BoundPeriodSeconds is the period the bound is counted over, authored with
	// the count.
	BoundPeriodSeconds float64
}

// Named reports whether the area names a grade at all.
func (h Hazard) Named() bool { return h.Grade != "" }

var (
	// ErrGradeUnknown is returned by [Writer.Declare] for a grade that is not
	// one of [Grades].
	ErrGradeUnknown = errors.New("area: the hazard severity is negligible, recoverable, or irreversible")
	// ErrIrreversibleNeedsItsBound is returned by [Writer.Declare] for an
	// irreversible grade written without the hazardous operation, the bound, and
	// the period the bound is counted over. Both are authored outright with
	// nothing supplied, so the grade is not written without them.
	ErrIrreversibleNeedsItsBound = errors.New("area: an irreversible area names its hazardous operation and its bound")
)

// checkHazard is the refusal the write makes for itself, beside the CHECK the
// store makes around it.
func checkHazard(h Hazard) error {
	if !h.Named() {
		return nil
	}
	if !slices.Contains(Grades, h.Grade) {
		return fmt.Errorf("%w: %q", ErrGradeUnknown, h.Grade)
	}
	if h.Grade != GradeIrreversible {
		return nil
	}
	if h.Operation == "" {
		return fmt.Errorf("%w: no operation is named", ErrIrreversibleNeedsItsBound)
	}
	if h.Bound <= 0 {
		return fmt.Errorf("%w: the bound is %v", ErrIrreversibleNeedsItsBound, h.Bound)
	}
	if h.BoundPeriodSeconds <= 0 {
		return fmt.Errorf("%w: the bound's period is %v", ErrIrreversibleNeedsItsBound, h.BoundPeriodSeconds)
	}
	return nil
}

// SeverityInForce is the value in force for an area: the highest grade named
// anywhere on its chain up to the project. Where no area in the chain names one
// the answer is [GradeNegligible], which is also the answer for an item that
// names no area at all — an empty id is negligible and no error.
//
// Three things read it, and each states its own rule where it acts: the
// Implementation gate, the rollout strategy, and the vector a gate firing writes.
// None of the three is built here.
func SeverityInForce(ctx context.Context, pool *pgxpool.Pool, areaID string) (Grade, error) {
	chain, _, err := Chain(ctx, pool, areaID)
	if err != nil {
		return "", err
	}
	inForce := GradeNegligible
	for _, a := range chain {
		if !a.Hazard.Named() {
			continue
		}
		if slices.Index(Grades, a.Hazard.Grade) > slices.Index(Grades, inForce) {
			inForce = a.Hazard.Grade
		}
	}
	return inForce, nil
}

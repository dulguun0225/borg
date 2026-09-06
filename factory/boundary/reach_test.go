// reach_test.go is the same arithmetic read forwards: how many intervals an
// exit needs, how fine a size the intervals actually read could rule out, what
// the power costs in either direction, and the within-interval bound in units
// of work. boundary_test.go is the reading itself; the two are one external
// test package split by subject, each file held to 500 lines.
package boundary_test

import (
	"errors"
	"math"
	"testing"

	"github.com/dulguun0225/borg/factory/boundary"
)

// TestTheIntervalsNeededScaleAsTheInverseSquareOfTheSize is the design's claim
// about how traffic scales, checked as arithmetic: a size three times finer
// costs about nine times the intervals.
func TestTheIntervalsNeededScaleAsTheInverseSquareOfTheSize(t *testing.T) {
	coarse := boundary.Boundary{Size: 0.09, Confidence: 0.95, Comparisons: 1, Worse: boundary.WorseHigher}
	fine := coarse
	fine.Size = 0.03

	coarseIntervals, err := coarse.IntervalsToPassed(0.02)
	if err != nil {
		t.Fatalf("IntervalsToPassed: %v", err)
	}
	fineIntervals, err := fine.IntervalsToPassed(0.02)
	if err != nil {
		t.Fatalf("IntervalsToPassed: %v", err)
	}
	if ratio := fineIntervals / coarseIntervals; math.Abs(ratio-9) > 0.001 {
		t.Errorf("a size three times finer cost %v times the intervals, want 9", ratio)
	}
}

// TestThePowerCostsMoreIntervalsThanEvenOdds is what the power is for: reaching
// passed reliably costs more than reaching it half the time, and the two agree
// at even odds.
func TestThePowerCostsMoreIntervalsThanEvenOdds(t *testing.T) {
	even, err := theBoundary.IntervalsForPassed(0.02, 0.5)
	if err != nil {
		t.Fatalf("IntervalsForPassed: %v", err)
	}
	toPassed, err := theBoundary.IntervalsToPassed(0.02)
	if err != nil {
		t.Fatalf("IntervalsToPassed: %v", err)
	}
	if math.Abs(even-toPassed) > 1e-9 {
		t.Errorf("IntervalsForPassed at even odds = %v, want IntervalsToPassed's %v", even, toPassed)
	}
	reliable, err := theBoundary.IntervalsForPassed(0.02, 0.9)
	if err != nil {
		t.Fatalf("IntervalsForPassed: %v", err)
	}
	if reliable <= even {
		t.Errorf("a power of nine in ten cost %v intervals, no more than even odds' %v", reliable, even)
	}
}

// TestTheFinestSizeIsTheIntervalsReadTheOtherWayRound is what a window records
// at its close: the size the traffic actually reached, which is the size that
// would have needed exactly the intervals it read.
func TestTheFinestSizeIsTheIntervalsReadTheOtherWayRound(t *testing.T) {
	fine := boundary.Boundary{Size: 0.02, Confidence: 0.95, Comparisons: 1, Worse: boundary.WorseHigher}
	needed, err := fine.IntervalsForPassed(0.05, 0.8)
	if err != nil {
		t.Fatalf("IntervalsForPassed: %v", err)
	}
	finest, err := fine.FinestSize(0.05, 0.8, int(math.Ceil(needed)))
	if err != nil {
		t.Fatalf("FinestSize: %v", err)
	}
	if math.Abs(finest-fine.Size) > 0.001 {
		t.Errorf("FinestSize over the intervals that size needs = %v, want about %v", finest, fine.Size)
	}
	coarser, err := fine.FinestSize(0.05, 0.8, 4)
	if err != nil {
		t.Fatalf("FinestSize: %v", err)
	}
	if coarser <= finest {
		t.Errorf("four intervals reached %v, no coarser than the %v the full count reached", coarser, finest)
	}
}

// TestAFinestSizeReadAtAHigherPowerIsCoarser is the power entering the size
// arithmetic: the same traffic catching a regression more reliably catches only
// a larger one.
func TestAFinestSizeReadAtAHigherPowerIsCoarser(t *testing.T) {
	reached := 0.04
	coarser, err := theBoundary.AtPower(reached, 0.5, 0.9)
	if err != nil {
		t.Fatalf("AtPower: %v", err)
	}
	if coarser <= reached {
		t.Errorf("the same traffic at a power of 0.9 reads %v, want coarser than %v at even odds", coarser, reached)
	}
	same, err := theBoundary.AtPower(reached, 0.8, 0.8)
	if err != nil {
		t.Fatalf("AtPower at the power it was recorded at: %v", err)
	}
	if math.Abs(same-reached) > 1e-12 {
		t.Errorf("AtPower at the power it was recorded at = %v, want %v", same, reached)
	}
	if _, err := theBoundary.AtPower(reached, 0.8, 1); err == nil {
		t.Error("a power of one was admitted, and no finite volume reaches it")
	}
}

func TestIntervalsToFailedRefusesADifferenceThatDriftsTheOtherWay(t *testing.T) {
	if _, err := theBoundary.IntervalsToFailed(0.02, 0.01); !errors.Is(err, boundary.ErrNoCrossing) {
		t.Errorf("IntervalsToFailed at a difference under half the size = %v, want ErrNoCrossing", err)
	}
	crossing, err := theBoundary.IntervalsToFailed(0.02, 0.2)
	if err != nil {
		t.Fatalf("IntervalsToFailed: %v", err)
	}
	if crossing <= 0 {
		t.Errorf("IntervalsToFailed = %v at a difference twice the size", crossing)
	}
}

// TestTheUnitsNeededAreTheOtherBound is the within-interval bound: it reads a
// rate rather than a series, and it scales the same way.
func TestTheUnitsNeededAreTheOtherBound(t *testing.T) {
	coarse := boundary.Boundary{Size: 0.03, Confidence: 0.95, Comparisons: 1, Worse: boundary.WorseHigher}
	fine := coarse
	fine.Size = 0.01
	coarseUnits, err := coarse.UnitsToPassed(0.5)
	if err != nil {
		t.Fatalf("UnitsToPassed: %v", err)
	}
	fineUnits, err := fine.UnitsToPassed(0.5)
	if err != nil {
		t.Fatalf("UnitsToPassed: %v", err)
	}
	if ratio := fineUnits / coarseUnits; ratio < 8 || ratio > 10 {
		t.Errorf("a size three times finer cost %v times the units, want about 9", ratio)
	}
	if _, err := coarse.UnitsToFailed(0.5, 0.5); !errors.Is(err, boundary.ErrNoCrossing) {
		t.Errorf("UnitsToFailed at the baseline rate = %v, want ErrNoCrossing", err)
	}
}

func TestCoarsestIsTheSizeInForce(t *testing.T) {
	if got := boundary.Coarsest(0.01, 0.04, 0.02); got != 0.04 {
		t.Errorf("Coarsest = %v, want the coarsest floor 0.04", got)
	}
}

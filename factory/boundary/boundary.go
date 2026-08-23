package boundary

import (
	"errors"
	"fmt"
	"math"
)

// Formula names the construction [Boundary.Evaluate] applies. It is stored on
// the watch window record beside the size and the confidence resolved at the
// open, for the reason the design gives: a reading at an exit is not
// interpretable against anything but the boundary it was actually read against,
// and the size and the confidence alone do not say what was done with them.
//
// A change to the construction changes this string, so a window closed under one
// is never read as a window closed under another.
const Formula = "log-likelihood-ratio/ville/v1"

// smoothing is the half a failure in one unit added to the baseline estimate. A
// baseline that failed nothing would otherwise put the alternative's advantage at
// infinity and condemn the first failure the release under watch has.
const smoothing = 0.5

var (
	// ErrSizeOutOfRange is returned for a size outside nothing to one. The size is
	// the smallest regression worth catching, as a share of the work — so a size of
	// nothing asks the comparison to rule out no regression at all.
	ErrSizeOutOfRange = errors.New("boundary: the size is a share above 0 and at most 1")
	// ErrConfidenceOutOfRange is returned for a confidence outside nothing to
	// one, either end excluded: at nothing every reading crosses at once, and at
	// one no reading ever crosses.
	ErrConfidenceOutOfRange = errors.New("boundary: the confidence is a share above 0 and below 1")
	// ErrUnitsNegative is returned for a count below zero, or for more failures
	// than units.
	ErrUnitsNegative = errors.New("boundary: a count of units is not negative and failures do not exceed units")
)

// The reasons neither exit is reachable, in the words [Reading.Unavailable]
// carries.
const (
	// NoBaseline is a release with nothing below it to be compared against,
	// which is a service's first release. Nothing about it is discovered by
	// watching, and the design says so: the cleared exit is not available to it.
	NoBaseline = "the release has no baseline to be compared against, so no regression can be ruled out or found"
	// NoHeadroom is a baseline rate so high that raising it by the size passes
	// one. There is no rate above it for the alternative to name, so the
	// comparison has nothing to detect a regression in.
	NoHeadroom = "the baseline already fails so often that the size raises it past every unit failing"
)

// Alternative is the rate the boundary tests against: the baseline rate plus the
// size. The size is a share of the work and not a share of the baseline, which is the
// reading that makes the boundary usable on a service that almost never fails —
// ruling out a tenth more of a baseline near nothing takes traffic no install has,
// where ruling out a tenth of the work failing takes a few hundred units.
//
// What it costs is at the other end. On a service failing a fifth of the time, a
// size of one in fifty is a regression of a tenth relative and the same absolute
// question, so the size reads coarse where it is fine and fine where it is coarse —
// and an owner reading the number has to know which of the two it is. It is the one
// the design's own arithmetic works at.
func (b Boundary) Alternative(baselineRate float64) float64 { return baselineRate + b.Size }

// Boundary is one window's boundary: the size the comparison must rule out and
// the confidence it must do it with, both resolved when the window opened and
// both stored on the window's record.
type Boundary struct {
	// Size is the smallest regression worth catching, as a share of the work: the
	// alternative the boundary tests against is the baseline rate plus this much.
	// [Boundary.Alternative] says why it is a share of the work rather than of the
	// baseline.
	Size float64
	// Confidence is how sure the comparison must be, as a share. One over
	// one-minus-confidence is where the ratio crosses in either direction.
	Confidence float64
}

// Validate reports whether the boundary may be read. It is a programmer's error
// rather than a reading, so it is an error and not an [Reading.Unavailable]: the
// values come from what an owner authored or the score supplied, both of which
// are already checked where they are written.
func (b Boundary) Validate() error {
	if b.Size <= 0 || b.Size > 1 {
		return fmt.Errorf("%w: %v", ErrSizeOutOfRange, b.Size)
	}
	if b.Confidence <= 0 || b.Confidence >= 1 {
		return fmt.Errorf("%w: %v", ErrConfidenceOutOfRange, b.Confidence)
	}
	return nil
}

// Crossing is where the log of the ratio crosses, in either direction: the log
// of one over one-minus-confidence. It is published rather than inlined because
// it is the whole of what the confidence means here.
func (b Boundary) Crossing() float64 { return math.Log(1 / (1 - b.Confidence)) }

// Observed is one read of the quantity: what the release under watch has served
// and failed, and what its baseline served and failed. The baseline's counts are
// its recent history and not a running total of everything it ever did — what
// composes them is the caller's, and on this substrate it is the file each build
// emitted into while it was serving.
type Observed struct {
	Units    int64
	Failures int64
	// BaselineUnits and BaselineFailures are the last known-good release's, which
	// is the newest release whose window closed without condemning it. Nothing is
	// the answer for a service's first release, and it makes both exits unreachable.
	BaselineUnits    int64
	BaselineFailures int64
}

// Validate refuses counts that are not counts.
func (o Observed) Validate() error {
	for _, pair := range [][2]int64{{o.Units, o.Failures}, {o.BaselineUnits, o.BaselineFailures}} {
		if pair[0] < 0 || pair[1] < 0 || pair[1] > pair[0] {
			return fmt.Errorf("%w: %d units, %d failures", ErrUnitsNegative, pair[0], pair[1])
		}
	}
	return nil
}

// Reading is what one read of the quantity says against the boundary. Every
// number the verdict was reached from is on it, because a boundary nobody can
// recompute is one nobody can argue with — the same rule the score's vector
// keeps.
type Reading struct {
	// Harm is the ratio having crossed against the release: a regression at
	// least as large as the size, at the confidence required.
	Harm bool
	// Clean is the ratio having crossed the other way: a regression of that size
	// ruled out, at the same confidence.
	Clean bool
	// Unavailable is why neither exit is reachable, and is empty where both are.
	Unavailable string
	// BaselineRate is the smoothed rate the alternative was raised from.
	BaselineRate float64
	// Alternative is the rate the boundary tested against: the baseline rate plus
	// the size.
	Alternative float64
	// Rate is what the release under watch actually failed at, which decides
	// nothing and is what a human reads.
	Rate float64
	// Log is the log of the likelihood ratio between the alternative and the
	// baseline over the units observed. Harm is this at or above the crossing and
	// clean is this at or below its negative, so one number is both exits.
	Log float64
	// Crossing is [Boundary.Crossing] as it was applied, so the reading carries
	// the line it was read against.
	Crossing float64
}

// Evaluate reads the quantity against the boundary. It reaches no state and
// remembers nothing between calls: the guarantee is over the sequence of reads
// and needs no memory of them, which is what a martingale bound buys.
func (b Boundary) Evaluate(o Observed) (Reading, error) {
	if err := b.Validate(); err != nil {
		return Reading{}, err
	}
	if err := o.Validate(); err != nil {
		return Reading{}, err
	}

	reading := Reading{Crossing: b.Crossing()}
	if o.Units > 0 {
		reading.Rate = float64(o.Failures) / float64(o.Units)
	}
	if o.BaselineUnits == 0 {
		reading.Unavailable = NoBaseline
		return reading, nil
	}

	reading.BaselineRate = (float64(o.BaselineFailures) + smoothing) / (float64(o.BaselineUnits) + 2*smoothing)
	reading.Alternative = b.Alternative(reading.BaselineRate)
	if reading.Alternative >= 1 {
		reading.Unavailable = NoHeadroom
		return reading, nil
	}

	// The log of the likelihood ratio between the alternative and the baseline
	// over the units observed: one term per failure and one per unit that did
	// not fail. Under the baseline the ratio is a non-negative martingale with
	// mean one, and so is its reciprocal under the alternative, which is why the
	// two lines are symmetric.
	failures := float64(o.Failures)
	succeeded := float64(o.Units - o.Failures)
	reading.Log = failures*math.Log(reading.Alternative/reading.BaselineRate) +
		succeeded*math.Log((1-reading.Alternative)/(1-reading.BaselineRate))

	switch {
	case reading.Log >= reading.Crossing:
		reading.Harm = true
	case reading.Log <= -reading.Crossing:
		reading.Clean = true
	}
	return reading, nil
}

// UnitsToClean is how many units a release running at exactly the baseline rate
// is expected to need before the ratio crosses toward clean. It is the crossing
// divided by the expected log per unit, which for this construction is the
// Kullback-Leibler divergence from the baseline to the alternative.
//
// It answers what a window costs before it is opened, and it is what makes the
// design's claim about this arithmetic checkable rather than asserted: the
// divergence goes as the square of the size for a small one, so the units go as
// its inverse square.
func (b Boundary) UnitsToClean(baselineRate float64) (float64, error) {
	return b.units(baselineRate, baselineRate, towardClean)
}

// UnitsToHarm is how many units a release running at rate is expected to need
// before the ratio crosses against it. A rate at or below the baseline never
// crosses that way and returns [ErrNoCrossing]: the expected log per unit is not
// positive, so the ratio drifts toward clean instead.
func (b Boundary) UnitsToHarm(baselineRate, rate float64) (float64, error) {
	return b.units(baselineRate, rate, towardHarm)
}

// ErrNoCrossing is returned by [Boundary.UnitsToHarm] and
// [Boundary.UnitsToClean] where the ratio drifts the other way, so the crossing
// asked about is not the one this rate reaches.
var ErrNoCrossing = errors.New("boundary: at this rate the ratio drifts toward the other exit")

// Which exit the units are counted toward. It is a parameter rather than
// something read off the rate, because the two questions differ at exactly the
// rate where they are easiest to confuse — a release running at its baseline is
// on its way to clean and is not on a slow walk to harm.
type direction int

const (
	towardHarm direction = iota
	towardClean
)

// units is the crossing divided by the expected log of the ratio per unit at
// rate, signed for the exit asked about. Where the drift is the other way there
// is no crossing in this direction and [ErrNoCrossing] says so — which includes
// every rate between the baseline and the alternative, the region the boundary
// was deliberately not opened to resolve.
func (b Boundary) units(baselineRate, rate float64, toward direction) (float64, error) {
	if err := b.Validate(); err != nil {
		return 0, err
	}
	if baselineRate <= 0 || baselineRate >= 1 || rate < 0 || rate > 1 {
		return 0, fmt.Errorf("boundary: a rate is a share, not baseline %v at %v", baselineRate, rate)
	}
	alternative := b.Alternative(baselineRate)
	if alternative >= 1 {
		return 0, errors.New("boundary: " + NoHeadroom)
	}
	perUnit := rate*math.Log(alternative/baselineRate) + (1-rate)*math.Log((1-alternative)/(1-baselineRate))
	if toward == towardClean {
		perUnit = -perUnit
	}
	if perUnit <= 0 {
		return 0, fmt.Errorf("%w: rate %v against baseline %v", ErrNoCrossing, rate, baselineRate)
	}
	return b.Crossing() / perUnit, nil
}

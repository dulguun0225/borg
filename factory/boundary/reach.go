package boundary

import (
	"errors"
	"fmt"
	"math"
)

// ErrNoCrossing is returned where the rate or the difference asked about drifts
// toward the other exit, so the crossing asked about is not the one it reaches.
var ErrNoCrossing = errors.New("boundary: at this reading the statistic drifts toward the other exit")

// ErrPowerUnreachable is returned by [Boundary.IntervalsForPassed] where no
// finite count of intervals reaches the power at the size in force. It is the
// arithmetic saying what the design says in words: passed is not an exit
// available to that window, which runs to the cap instead.
var ErrPowerUnreachable = errors.New("boundary: no count of intervals reaches that power at this size")

// Alternative is the value the boundary tests against: the baseline plus the
// size in the direction a regression moves the quantity. The size is a share of
// the work and not a share of the baseline, which is the reading that makes the
// boundary usable on a service that almost never fails — ruling out a tenth more
// of a baseline near nothing takes traffic no install has, where ruling out a
// tenth of the work failing takes a few hundred units.
//
// What it costs is at the other end. On a service failing a fifth of the time, a
// size of one in fifty is a change of a tenth relative and the same absolute
// question, so the size reads coarse where it is fine and fine where it is
// coarse — and an owner reading the number has to know which of the two it is.
func (b Boundary) Alternative(baselineRate float64) float64 {
	if b.Worse == WorseLower {
		return baselineRate - b.Size
	}
	return baselineRate + b.Size
}

// IntervalsToPassed is how many intervals a release whose arms differ by nothing
// is expected to need before the statistic crosses toward passed, at a spread of
// deviation between intervals. It is the crossing over the log the statistic
// loses per interval when there is no change to find.
//
// It answers what a window costs before it is opened, and it is where the
// design's claim about how traffic scales is a property of the arithmetic rather
// than a sentence beside it: the intervals go as the inverse square of the size.
func (b Boundary) IntervalsToPassed(deviation float64) (float64, error) {
	if err := b.Validate(); err != nil {
		return 0, err
	}
	if deviation <= 0 {
		return 0, fmt.Errorf("boundary: the spread between intervals is above nothing, not %v", deviation)
	}
	return 2 * b.Crossing() * deviation * deviation / (b.Size * b.Size), nil
}

// IntervalsForPassed is how many intervals a release whose arms differ by
// nothing needs before it reaches passed with probability power. That is the
// power's whole content: the rate at which a window that should close early
// actually does, rather than running to the cap having ruled the same thing out.
//
// It is [Boundary.IntervalsToPassed] at even odds and more than it above them,
// which is the check TestThePowerCostsMoreIntervalsThanEvenOdds makes.
func (b Boundary) IntervalsForPassed(deviation, power float64) (float64, error) {
	if err := b.Validate(); err != nil {
		return 0, err
	}
	if power <= 0 || power >= 1 {
		return 0, fmt.Errorf("%w: %v", ErrPowerOutOfRange, power)
	}
	if deviation <= 0 {
		return 0, fmt.Errorf("boundary: the spread between intervals is above nothing, not %v", deviation)
	}
	z := normalQuantile(power)
	root := deviation * (z + math.Sqrt(z*z+2*b.Crossing())) / b.Size
	if math.IsNaN(root) || math.IsInf(root, 0) || root <= 0 {
		return 0, fmt.Errorf("%w: size %v at power %v", ErrPowerUnreachable, b.Size, power)
	}
	return root * root, nil
}

// FinestSize is [Boundary.IntervalsForPassed] read the other way round: the
// finest size this many intervals at this spread can rule out at that power. It
// is what the traffic reaches, which a window records at its close and the score
// reads — the size in force being the coarser of what the evidence asks for and
// this.
//
// It does not read [Boundary.Size], only the crossing: what the size is asked to
// be does not change what the traffic can show.
func (b Boundary) FinestSize(deviation, power float64, intervals int) (float64, error) {
	if err := b.Validate(); err != nil {
		return 0, err
	}
	if power <= 0 || power >= 1 {
		return 0, fmt.Errorf("%w: %v", ErrPowerOutOfRange, power)
	}
	if intervals < 1 || deviation <= 0 {
		return 0, fmt.Errorf("boundary: a finest size needs an interval read and a spread above nothing, not %d at %v",
			intervals, deviation)
	}
	z := normalQuantile(power)
	return deviation * (z + math.Sqrt(z*z+2*b.Crossing())) / math.Sqrt(float64(intervals)), nil
}

// IntervalsToFailed is how many intervals a release whose arms differ by
// difference — signed so that positive is worse — is expected to need before the
// statistic crosses against it. A difference at or below half the size never
// crosses that way and returns [ErrNoCrossing]: the statistic drifts toward
// passed instead, which is the region the boundary was deliberately not opened
// to resolve.
func (b Boundary) IntervalsToFailed(deviation, difference float64) (float64, error) {
	if err := b.Validate(); err != nil {
		return 0, err
	}
	if deviation <= 0 {
		return 0, fmt.Errorf("boundary: the spread between intervals is above nothing, not %v", deviation)
	}
	perInterval := (b.Size*difference - b.Size*b.Size/2) / (deviation * deviation)
	if perInterval <= 0 {
		return 0, fmt.Errorf("%w: a difference of %v against a size of %v", ErrNoCrossing, difference, b.Size)
	}
	return b.Crossing() / perInterval, nil
}

// UnitsToPassed is the other bound the design states, and it is read in units
// rather than intervals: how many units of work a release running at exactly the
// baseline rate needs before that rate could be told from the alternative at
// all. It is the crossing over the Kullback-Leibler divergence from the baseline
// to the alternative, which goes as the square of the size, so the units go as
// its inverse square.
//
// Both bounds stand and the stricter governs. This one is what says whether a
// size the score is asking for is reachable in the traffic a service receives;
// [Boundary.IntervalsToPassed] is what says whether the intervals inside the cap
// can carry it.
func (b Boundary) UnitsToPassed(baselineRate float64) (float64, error) {
	return b.units(baselineRate, baselineRate, towardPassed)
}

// UnitsToFailed is how many units a release running at rate is expected to need
// before that rate could be told from the baseline. A rate on the other side of
// the baseline from the alternative returns [ErrNoCrossing].
func (b Boundary) UnitsToFailed(baselineRate, rate float64) (float64, error) {
	return b.units(baselineRate, rate, towardFailed)
}

// Which exit the units are counted toward. It is a parameter rather than
// something read off the rate, because the two questions differ at exactly the
// rate where they are easiest to confuse — a release running at its baseline is
// on its way to passed and is not on a slow walk to failed.
type toward int

const (
	towardFailed toward = iota
	towardPassed
)

// units is the crossing over the expected log of the likelihood ratio per unit
// at rate. It is the within-interval bound and it reads a rate rather than a
// series, so it is the one piece of this package a caller with no history can
// still ask.
func (b Boundary) units(baselineRate, rate float64, exit toward) (float64, error) {
	if err := b.Validate(); err != nil {
		return 0, err
	}
	if baselineRate <= 0 || baselineRate >= 1 || rate < 0 || rate > 1 {
		return 0, fmt.Errorf("boundary: a rate is a share, not baseline %v at %v", baselineRate, rate)
	}
	alternative := b.Alternative(baselineRate)
	if alternative >= 1 || alternative <= 0 {
		return 0, errors.New("boundary: " + NoHeadroom)
	}
	perUnit := rate*math.Log(alternative/baselineRate) + (1-rate)*math.Log((1-alternative)/(1-baselineRate))
	if exit == towardPassed {
		// The divergence from the baseline to the alternative, which is what a
		// release behaving exactly like its baseline accumulates against the
		// alternative.
		perUnit = -perUnit
	}
	if perUnit <= 0 {
		return 0, fmt.Errorf("%w: rate %v against baseline %v", ErrNoCrossing, rate, baselineRate)
	}
	return b.Crossing() / perUnit, nil
}

// Coarsest is the size in force out of the sizes asked for and the floors under
// them: what the evidence asks for, the finest the traffic can rule anything out
// at, and — on the latency quantity — the share of completions the histogram
// bucket the quantile falls in already holds, since a change smaller than a
// bucket is a change the kept series cannot show.
//
// A size finer than that is a size nothing is ruled out at, so the window times
// out every time, protects nothing, and holds the next deploy for the whole cap.
func Coarsest(sizes ...float64) float64 {
	coarsest := 0.0
	for _, size := range sizes {
		if size > coarsest {
			coarsest = size
		}
	}
	return coarsest
}

// normalQuantile is the inverse of the standard normal distribution function,
// by Peter Acklam's rational approximation, which is accurate to about one part
// in a billion over the whole range. It is here because the power is a
// probability of reaching an exit and the arithmetic that turns one into a count
// of intervals needs it, and because this package imports nothing.
func normalQuantile(p float64) float64 {
	a := [6]float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02,
		1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := [5]float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02,
		6.680131188771972e+01, -1.328068155288572e+01}
	c := [6]float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00,
		-2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	d := [4]float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00,
		3.754408661907416e+00}
	const low, high = 0.02425, 1 - 0.02425

	switch {
	case p <= 0 || p >= 1:
		return math.NaN()
	case p < low:
		q := math.Sqrt(-2 * math.Log(p))
		return (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p > high:
		q := math.Sqrt(-2 * math.Log(1-p))
		return -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	default:
		q := p - 0.5
		r := q * q
		return (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q /
			(((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	}
}

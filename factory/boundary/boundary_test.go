// The boundary's own arithmetic. Nothing here reaches a database: the package
// imports nothing and reads numbers its caller already has, so its tests are the
// only ones in the factory that are arithmetic alone.
package boundary_test

import (
	"errors"
	"math"
	"math/rand"
	"testing"

	"github.com/dulguun0225/borg/factory/boundary"
)

// theBoundary is a coarse size at the conventional confidence over one
// comparison: one unit in ten above the baseline, ruled out at ninety-five per
// cent. Coarse, because a test that needed the traffic a two per cent size needs
// would be a test of arithmetic run over hundreds of thousands of simulated
// intervals.
var theBoundary = boundary.Boundary{
	Size: 0.1, Confidence: 0.95, Comparisons: 1, Worse: boundary.WorseHigher,
}

// intervals is a series of intervals in which each arm serves units and fails at
// its own rate, with no noise between intervals beyond the counts themselves.
func intervals(n int, units int64, rate, baselineRate float64) boundary.Observed {
	var o boundary.Observed
	for i := 0; i < n; i++ {
		o.Intervals = append(o.Intervals, boundary.Counts{
			Units: units, Count: int64(math.Round(rate * float64(units))),
			BaselineUnits: units, BaselineCount: int64(math.Round(baselineRate * float64(units))),
		})
	}
	return o
}

func TestTheCrossingHoldsTheConfidenceOverTheSet(t *testing.T) {
	if got, want := theBoundary.Crossing(), math.Log(1/0.05); math.Abs(got-want) > 1e-12 {
		t.Errorf("Crossing() over one comparison = %v, want log(1/0.05) = %v", got, want)
	}
	over12 := theBoundary
	over12.Comparisons = 12
	if got, want := over12.Crossing(), math.Log(12/0.05); math.Abs(got-want) > 1e-12 {
		t.Errorf("Crossing() over twelve comparisons = %v, want log(12/0.05) = %v", got, want)
	}
	if over12.Crossing() <= theBoundary.Crossing() {
		t.Error("a boundary allocated over twelve comparisons is not stricter than one over a single reading")
	}
}

// TestARegressionCrosses is the failed exit: a release failing far above its
// control crosses, and the reading carries every number the verdict came from.
func TestARegressionCrosses(t *testing.T) {
	reading, err := theBoundary.Evaluate(intervals(4, 200, 0.5, 0.05))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !reading.Failed || reading.Passed {
		t.Errorf("failed=%v passed=%v at half the units failing against a control of one in twenty (log %v against %v)",
			reading.Failed, reading.Passed, reading.Log, reading.Crossing)
	}
	if reading.Intervals != 4 {
		t.Errorf("Intervals = %d, want the four read on both arms", reading.Intervals)
	}
	if math.Abs(reading.Difference-0.45) > 0.01 {
		t.Errorf("Difference = %v, want about 0.45", reading.Difference)
	}
	if reading.Unavailable != "" {
		t.Errorf("Unavailable = %q on a reading that crossed", reading.Unavailable)
	}
}

// TestNoRegressionRulesOneOut is the passed exit: arms that behave alike rule
// out a change of the size.
func TestNoRegressionRulesOneOut(t *testing.T) {
	reading, err := theBoundary.Evaluate(intervals(30, 500, 0.05, 0.05))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !reading.Passed || reading.Failed {
		t.Errorf("passed=%v failed=%v with both arms at one in twenty (log %v against %v)",
			reading.Passed, reading.Failed, reading.Log, reading.Crossing)
	}
}

// TestTheIntervalIsTheUnit is the change finding 23 names: a burst of traffic
// inside one interval buys nothing the next interval does not, so one interval
// carrying the whole volume rules out less than the same volume spread over
// many.
func TestTheIntervalIsTheUnit(t *testing.T) {
	burst, err := theBoundary.Evaluate(intervals(1, 15000, 0.05, 0.05))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	spread, err := theBoundary.Evaluate(intervals(30, 500, 0.05, 0.05))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if burst.Passed {
		t.Error("one interval holding the whole volume closed the window passed, which is the request as the unit")
	}
	if !spread.Passed {
		t.Error("the same volume over thirty intervals did not rule the change out")
	}
	if -burst.Log >= -spread.Log {
		t.Errorf("the burst accumulated %v against the spread's %v", burst.Log, spread.Log)
	}
}

// TestASilentReleaseArmBesideAServingControlFails is the design's plainest case,
// read on the request rate: the units are both arms' arrivals and the count is
// the release arm's own, so an arm that received nothing is a difference of one
// in the direction that is worse.
func TestASilentReleaseArmBesideAServingControlFails(t *testing.T) {
	requestRate := boundary.Boundary{
		Size: 0.1, Confidence: 0.95, Comparisons: 3, Worse: boundary.WorseLower,
	}
	var o boundary.Observed
	for i := 0; i < 3; i++ {
		o.Intervals = append(o.Intervals, boundary.Counts{
			Units: 100, Count: 0, BaselineUnits: 100, BaselineCount: 100,
		})
	}
	reading, err := requestRate.Evaluate(o)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !reading.Failed {
		t.Errorf("a release arm that received nothing beside a control serving everything did not cross (log %v against %v, unavailable %q)",
			reading.Log, reading.Crossing, reading.Unavailable)
	}
}

// TestAReleaseArmWithNoUnitsAtAllStillCrosses is the same reading where the
// emission counted the release arm's own arrivals rather than both arms': the
// arm has no units in any interval, which every other quantity reads as an
// interval not observed, and the request rate reads as the arm having received
// nothing beside a control that was served.
func TestAReleaseArmWithNoUnitsAtAllStillCrosses(t *testing.T) {
	requestRate := boundary.Boundary{
		Size: 0.1, Confidence: 0.95, Comparisons: 3, Worse: boundary.WorseLower,
	}
	var o boundary.Observed
	for i := 0; i < 3; i++ {
		o.Intervals = append(o.Intervals, boundary.Counts{
			Units: 0, Count: 0, BaselineUnits: 100, BaselineCount: 100,
		})
	}
	reading, err := requestRate.Evaluate(o)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !reading.Failed {
		t.Errorf("a release arm with no units at all did not cross the request rate (log %v against %v, unavailable %q)",
			reading.Log, reading.Crossing, reading.Unavailable)
	}
}

// TestAQuantityARegressionRaisesStillSkipsAnUnservedInterval is the other half
// of that rule: on an error rate, an interval the release arm has no units in is
// no observation, because a release that served nothing failed nothing.
func TestAQuantityARegressionRaisesStillSkipsAnUnservedInterval(t *testing.T) {
	var o boundary.Observed
	for i := 0; i < 4; i++ {
		o.Intervals = append(o.Intervals, boundary.Counts{
			Units: 0, Count: 0, BaselineUnits: 100, BaselineCount: 1,
		})
	}
	reading, err := theBoundary.Evaluate(o)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if reading.Failed || reading.Passed {
		t.Errorf("an error rate over intervals the release arm served nothing in reached an exit (log %v)", reading.Log)
	}
	if reading.Unavailable != boundary.NoVolume {
		t.Errorf("Unavailable = %q, want %q", reading.Unavailable, boundary.NoVolume)
	}
}

// TestAnArmTakingItsShareOfTrafficDoesNotCross is the other side of the same
// reading: two arms splitting the traffic evenly rule the shortfall out.
func TestAnArmTakingItsShareOfTrafficDoesNotCross(t *testing.T) {
	requestRate := boundary.Boundary{
		Size: 0.1, Confidence: 0.95, Comparisons: 1, Worse: boundary.WorseLower,
	}
	var o boundary.Observed
	for i := 0; i < 40; i++ {
		o.Intervals = append(o.Intervals, boundary.Counts{
			Units: 200, Count: 100, BaselineUnits: 200, BaselineCount: 100,
		})
	}
	reading, err := requestRate.Evaluate(o)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if reading.Failed {
		t.Errorf("two arms splitting the traffic evenly crossed on the request rate (log %v)", reading.Log)
	}
	if !reading.Passed {
		t.Errorf("two arms splitting the traffic evenly did not rule the shortfall out (log %v against %v)",
			reading.Log, reading.Crossing)
	}
}

// TestNoControlIsNeitherExit is a release with nothing beside it: the reading is
// unavailable rather than clear, and the window it belongs to ends at its cap.
func TestNoControlIsNeitherExit(t *testing.T) {
	reading, err := theBoundary.Evaluate(boundary.Observed{Intervals: []boundary.Counts{
		{Units: 400, Count: 8}, {Units: 380, Count: 6},
	}})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if reading.Failed || reading.Passed {
		t.Error("a release with no arm beside it reached an exit")
	}
	if reading.Unavailable != boundary.NoBaseline {
		t.Errorf("Unavailable = %q, want %q", reading.Unavailable, boundary.NoBaseline)
	}
}

// TestNothingReadOnEitherArmIsNoVolume is the read whose period the store does
// not cover: no volume, never a low one.
func TestNothingReadOnEitherArmIsNoVolume(t *testing.T) {
	reading, err := theBoundary.Evaluate(boundary.Observed{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if reading.Unavailable != boundary.NoVolume {
		t.Errorf("Unavailable = %q, want %q", reading.Unavailable, boundary.NoVolume)
	}
}

func TestABaselineWithNoHeadroomIsNeitherExit(t *testing.T) {
	reading, err := theBoundary.Evaluate(intervals(5, 100, 0.99, 0.95))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if reading.Unavailable != boundary.NoHeadroom {
		t.Errorf("Unavailable = %q, want %q", reading.Unavailable, boundary.NoHeadroom)
	}
}

// TestTheSpreadBetweenIntervalsIsWhatTheStatisticIsScaledBy is the correlation
// the design says the interval absorbs: a service whose arms differ by nothing
// on average but swing between intervals rules a change out more slowly than one
// that does not swing.
func TestTheSpreadBetweenIntervalsIsWhatTheStatisticIsScaledBy(t *testing.T) {
	steady := intervals(20, 400, 0.05, 0.05)
	var swinging boundary.Observed
	for i := 0; i < 20; i++ {
		count := int64(10)
		if i%2 == 0 {
			count = 30
		}
		swinging.Intervals = append(swinging.Intervals, boundary.Counts{
			Units: 400, Count: count, BaselineUnits: 400, BaselineCount: 20,
		})
	}
	steadyReading, err := theBoundary.Evaluate(steady)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	swingingReading, err := theBoundary.Evaluate(swinging)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if swingingReading.Deviation <= steadyReading.Deviation {
		t.Errorf("the swinging service's spread %v is not above the steady one's %v",
			swingingReading.Deviation, steadyReading.Deviation)
	}
	if -swingingReading.Log >= -steadyReading.Log {
		t.Errorf("the swinging service ruled the change out at %v, no slower than the steady one's %v",
			swingingReading.Log, steadyReading.Log)
	}
}

// TestTheConfidenceHoldsOverManyReadings is the rate the authored confidence
// bounds, simulated: over services where nothing changed, the share of windows
// that cross against the release stays under one in twenty.
func TestTheConfidenceHoldsOverManyReadings(t *testing.T) {
	random := rand.New(rand.NewSource(20260905))
	const runs, perWindow = 400, 40
	crossed := 0
	for run := 0; run < runs; run++ {
		var o boundary.Observed
		failedNow := false
		for i := 0; i < perWindow && !failedNow; i++ {
			o.Intervals = append(o.Intervals, boundary.Counts{
				Units: 300, Count: draw(random, 300, 0.05),
				BaselineUnits: 300, BaselineCount: draw(random, 300, 0.05),
			})
			reading, err := theBoundary.Evaluate(o)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if reading.Failed {
				failedNow = true
			}
		}
		if failedNow {
			crossed++
		}
	}
	if rate := float64(crossed) / runs; rate > 0.05 {
		t.Errorf("%d of %d windows over services where nothing changed crossed against the release (%.3f), above the authored one in twenty",
			crossed, runs, rate)
	}
}

func draw(random *rand.Rand, units int64, rate float64) int64 {
	var count int64
	for i := int64(0); i < units; i++ {
		if random.Float64() < rate {
			count++
		}
	}
	return count
}

// TestARunLengthIsAConfidenceReadFromTheOtherEnd is the reading that never
// closes: no last look to spend a rate against, so what is stated is the mean
// number of intervals between crossings when nothing has changed.
func TestARunLengthIsAConfidenceReadFromTheOtherEnd(t *testing.T) {
	b, err := boundary.AtRunLength(0.1, 1000, 3, boundary.WorseHigher)
	if err != nil {
		t.Fatalf("AtRunLength: %v", err)
	}
	if math.Abs(b.RunLength()-1000) > 1e-9 {
		t.Errorf("RunLength() = %v, want the thousand it was built from", b.RunLength())
	}
	if got, want := b.Crossing(), math.Log(3*1000); math.Abs(got-want) > 1e-9 {
		t.Errorf("Crossing() = %v, want log(3 x 1000) = %v", got, want)
	}
	if _, err := boundary.AtRunLength(0.1, 1, 1, boundary.WorseHigher); !errors.Is(err, boundary.ErrRunLengthTooShort) {
		t.Errorf("AtRunLength at one observation = %v, want ErrRunLengthTooShort", err)
	}
}

func TestValidateRefusesWhatIsNotABoundary(t *testing.T) {
	for _, refused := range []struct {
		what string
		b    boundary.Boundary
		want error
	}{
		{"a size of nothing", boundary.Boundary{Size: 0, Confidence: 0.95, Comparisons: 1, Worse: boundary.WorseHigher}, boundary.ErrSizeOutOfRange},
		{"a confidence of one", boundary.Boundary{Size: 0.1, Confidence: 1, Comparisons: 1, Worse: boundary.WorseHigher}, boundary.ErrConfidenceOutOfRange},
		{"no comparison", boundary.Boundary{Size: 0.1, Confidence: 0.95, Comparisons: 0, Worse: boundary.WorseHigher}, boundary.ErrComparisonsNotPositive},
		{"no direction", boundary.Boundary{Size: 0.1, Confidence: 0.95, Comparisons: 1}, boundary.ErrWorseUnknown},
	} {
		if err := refused.b.Validate(); !errors.Is(err, refused.want) {
			t.Errorf("Validate with %s = %v, want %v", refused.what, err, refused.want)
		}
	}
}

func TestCountsThatAreNotCountsAreRefused(t *testing.T) {
	_, err := theBoundary.Evaluate(boundary.Observed{Intervals: []boundary.Counts{{Units: 10, Count: 11}}})
	if !errors.Is(err, boundary.ErrCountsNegative) {
		t.Errorf("Evaluate with more failures than units = %v, want ErrCountsNegative", err)
	}
}

func TestTotalsAddAcrossIntervals(t *testing.T) {
	totals := intervals(3, 100, 0.1, 0.2).Totals()
	want := boundary.Counts{Units: 300, Count: 30, BaselineUnits: 300, BaselineCount: 60}
	if totals != want {
		t.Errorf("Totals = %+v, want %+v", totals, want)
	}
}

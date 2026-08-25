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

// theBoundary is a coarse size at the conventional confidence: one unit in ten
// failing above the baseline, ruled out at ninety-five per cent. Coarse, because a
// test that needed the traffic a two per cent size needs would be a test of
// arithmetic run over hundreds of thousands of simulated units.
var theBoundary = boundary.Boundary{Size: 0.1, Confidence: 0.95}

func TestTheCrossingIsWhatTheConfidenceMeans(t *testing.T) {
	want := math.Log(1 / 0.05)
	if got := theBoundary.Crossing(); math.Abs(got-want) > 1e-12 {
		t.Errorf("Crossing() = %v, want log(1/0.05) = %v", got, want)
	}
}

// TestARegressionCrosses is the failed exit: a release failing far above its
// baseline crosses, and the reading carries every number the verdict came from.
func TestARegressionCrosses(t *testing.T) {
	reading, err := theBoundary.Evaluate(boundary.Observed{
		Units: 400, Failures: 200,
		BaselineUnits: 1000, BaselineFailures: 50,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !reading.Harm || reading.Clean {
		t.Errorf("harm=%v clean=%v at half the units failing against a baseline of one in twenty (log %v against %v)",
			reading.Harm, reading.Clean, reading.Log, reading.Crossing)
	}
	if reading.Unavailable != "" {
		t.Errorf("the reading is unavailable: %s", reading.Unavailable)
	}
	if math.Abs(reading.Rate-0.5) > 1e-12 {
		t.Errorf("rate = %v, 200 of 400 units failed", reading.Rate)
	}
	// The baseline is smoothed by half a failure in one unit, so it is not the
	// raw share and the difference is what stops a baseline with no failure
	// failing the first one the release has.
	wantBaseline := (50 + 0.5) / (1000 + 1)
	if math.Abs(reading.BaselineRate-wantBaseline) > 1e-12 {
		t.Errorf("baseline rate = %v, want the smoothed %v", reading.BaselineRate, wantBaseline)
	}
	if math.Abs(reading.Alternative-(wantBaseline+theBoundary.Size)) > 1e-12 {
		t.Errorf("alternative = %v, want the baseline plus the size", reading.Alternative)
	}
}

// TestNoRegressionCloses is the passed exit: a release failing at its baseline's
// own rate rules out a regression of the size, and it takes about as many units
// as the arithmetic says it should.
func TestNoRegressionCloses(t *testing.T) {
	const baselineUnits, baselineFailures = 4000, 200 // one in twenty
	baselineRate := (float64(baselineFailures) + 0.5) / (baselineUnits + 1)
	expected, err := theBoundary.UnitsToClean(baselineRate)
	if err != nil {
		t.Fatalf("UnitsToClean: %v", err)
	}

	// Twice the expected units, failing at the baseline's rate: comfortably past
	// the crossing without the test resting on the exact count.
	units := int64(2 * expected)
	reading, err := theBoundary.Evaluate(boundary.Observed{
		Units: units, Failures: int64(baselineRate * float64(units)),
		BaselineUnits: baselineUnits, BaselineFailures: baselineFailures,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !reading.Clean || reading.Harm {
		t.Errorf("clean=%v harm=%v after %d units at the baseline's own rate (log %v against %v)",
			reading.Clean, reading.Harm, units, reading.Log, reading.Crossing)
	}

	// And at a tenth of the expected units neither exit is reached, which is the
	// window still open: the duration is measured and never set.
	early := int64(expected / 10)
	reading, err = theBoundary.Evaluate(boundary.Observed{
		Units: early, Failures: int64(baselineRate * float64(early)),
		BaselineUnits: baselineUnits, BaselineFailures: baselineFailures,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if reading.Clean || reading.Harm {
		t.Errorf("clean=%v harm=%v after %d units, a tenth of what the arithmetic asks for",
			reading.Clean, reading.Harm, early)
	}
}

// TestTheUnitsNeededScaleAsTheInverseSquareOfTheSize is the design's own claim
// about this arithmetic, checked as a property of it: decomposing the size tenfold
// multiplies the units a comparison needs by about a hundred, which is why
// closing a window faster by coarsening the boundary runs out quickly.
func TestTheUnitsNeededScaleAsTheInverseSquareOfTheSize(t *testing.T) {
	// The property is the limit as the size goes to nothing, so the two sizes are
	// both small against the baseline. At a size comparable to the baseline the
	// divergence is no longer quadratic and the ratio is nearer forty than a hundred,
	// which is the arithmetic being honest rather than the property failing.
	const baselineRate = 0.5
	coarse, err := boundary.Boundary{Size: 0.05, Confidence: 0.95}.UnitsToClean(baselineRate)
	if err != nil {
		t.Fatalf("UnitsToClean at the coarse size: %v", err)
	}
	fine, err := boundary.Boundary{Size: 0.005, Confidence: 0.95}.UnitsToClean(baselineRate)
	if err != nil {
		t.Fatalf("UnitsToClean at the fine size: %v", err)
	}

	ratio := fine / coarse
	if ratio < 90 || ratio > 110 {
		t.Errorf("decomposing the size tenfold multiplied the units by %v, want about a hundred (%v then %v)",
			ratio, coarse, fine)
	}
}

// TestAFirstReleaseCanNeitherBePassedNorFailed is the exit table's own
// exception: a release with no baseline has no comparison, so nothing about it
// is discovered by watching and its window ends at the cap.
func TestAFirstReleaseCanNeitherBePassedNorFailed(t *testing.T) {
	reading, err := theBoundary.Evaluate(boundary.Observed{Units: 100000, Failures: 100000})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if reading.Harm || reading.Clean {
		t.Errorf("harm=%v clean=%v with no baseline, and every unit failing", reading.Harm, reading.Clean)
	}
	if reading.Unavailable != boundary.NoBaseline {
		t.Errorf("Unavailable = %q, want %q", reading.Unavailable, boundary.NoBaseline)
	}
	// The rate is still reported: what a human reads is available where a verdict
	// is not.
	if reading.Rate != 1 {
		t.Errorf("rate = %v, every unit failed", reading.Rate)
	}
}

// TestABaselineWithNoHeadroomIsUnavailable is the other unreachable reading: a
// service failing so often that raising its rate by the size passes every unit
// failing has nothing above it to detect a regression in, which is exactly the
// service the design says an absolute threshold is worth stating for.
func TestABaselineWithNoHeadroomIsUnavailable(t *testing.T) {
	reading, err := theBoundary.Evaluate(boundary.Observed{
		Units: 100, Failures: 99,
		BaselineUnits: 100, BaselineFailures: 95,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if reading.Unavailable != boundary.NoHeadroom {
		t.Errorf("Unavailable = %q, want %q (baseline %v, alternative %v)",
			reading.Unavailable, boundary.NoHeadroom, reading.BaselineRate, reading.Alternative)
	}
	if reading.Harm || reading.Clean {
		t.Errorf("harm=%v clean=%v with no headroom above the baseline", reading.Harm, reading.Clean)
	}
}

// TestTheBoundaryHoldsUnderRepeatedReading is what the whole construction is
// for, and the one property a fixed-horizon threshold does not have. A release
// running at exactly its baseline's rate is read after every single unit, which
// is the most looks anybody could take; the share of runs that ever cross toward
// harm has to stay under one-minus-confidence, and a threshold read that way
// would cross in nearly all of them.
func TestTheBoundaryHoldsUnderRepeatedReading(t *testing.T) {
	const (
		runs          = 400
		unitsPerRun   = 2000
		baselineUnits = 20000
		baselineRate  = 0.1
	)
	b := boundary.Boundary{Size: 0.1, Confidence: 0.95}
	// The generator is seeded, so a failure here is a defect and never a run of
	// bad luck a rerun hides.
	random := rand.New(rand.NewSource(4))

	crossed := 0
	for range runs {
		var units, failures int64
		for range unitsPerRun {
			units++
			if random.Float64() < baselineRate {
				failures++
			}
			reading, err := b.Evaluate(boundary.Observed{
				Units: units, Failures: failures,
				BaselineUnits: baselineUnits, BaselineFailures: baselineUnits * baselineRate,
			})
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if reading.Harm {
				crossed++
				break
			}
			if reading.Clean {
				break
			}
		}
	}

	// Ville's inequality bounds this at one-minus-confidence over the whole
	// sequence of reads, however long it is. The allowance above it is the
	// sampling error of 400 runs and not slack in the bound.
	share := float64(crossed) / runs
	if share > 0.05+0.04 {
		t.Errorf("%d of %d runs at the baseline's own rate crossed toward harm (%.3f), and the bound is 0.05",
			crossed, runs, share)
	}
}

func TestAnUnreadableBoundaryOrCountIsRefused(t *testing.T) {
	for _, refused := range []struct {
		what string
		b    boundary.Boundary
		want error
	}{
		{"a size of nothing", boundary.Boundary{Size: 0, Confidence: 0.95}, boundary.ErrSizeOutOfRange},
		{"a size above one", boundary.Boundary{Size: 1.5, Confidence: 0.95}, boundary.ErrSizeOutOfRange},
		{"a confidence of one", boundary.Boundary{Size: 0.5, Confidence: 1}, boundary.ErrConfidenceOutOfRange},
		{"a confidence of nothing", boundary.Boundary{Size: 0.5, Confidence: 0}, boundary.ErrConfidenceOutOfRange},
	} {
		if _, err := refused.b.Evaluate(boundary.Observed{}); !errors.Is(err, refused.want) {
			t.Errorf("Evaluate with %s = %v, want %v", refused.what, err, refused.want)
		}
	}

	for _, refused := range []boundary.Observed{
		{Units: -1},
		{Units: 10, Failures: 11},
		{BaselineUnits: 10, BaselineFailures: -1},
	} {
		if _, err := theBoundary.Evaluate(refused); !errors.Is(err, boundary.ErrUnitsNegative) {
			t.Errorf("Evaluate(%+v) = %v, want %v", refused, err, boundary.ErrUnitsNegative)
		}
	}
}

// TestARegressionTooSmallToNameDriftsTheOtherWay is where the boundary's own
// indifference lies, and it is not at the alternative. The drift changes sign at
// the rate where the two hypotheses are equally well supported per unit, which
// sits between the baseline and the alternative and nearer the baseline — so a
// regression below it closes the window clean, one above it eventually fails
// however long that takes, and the size is what says which regressions the
// window was opened to reach quickly.
func TestARegressionTooSmallToNameDriftsTheOtherWay(t *testing.T) {
	const baselineRate = 0.1
	barely := baselineRate + 0.005 // a real regression, and far below the alternative
	if _, err := theBoundary.UnitsToHarm(baselineRate, barely); !errors.Is(err, boundary.ErrNoCrossing) {
		t.Errorf("UnitsToHarm at a regression of a two-hundredth = %v, want %v", err, boundary.ErrNoCrossing)
	}
	// Just under the alternative it does drift toward harm, and slowly: the units
	// it asks for are more than a release at the alternative's own rate needs.
	near, err := theBoundary.UnitsToHarm(baselineRate, baselineRate+0.08)
	if err != nil {
		t.Fatalf("UnitsToHarm just under the alternative: %v", err)
	}
	at, err := theBoundary.UnitsToHarm(baselineRate, theBoundary.Alternative(baselineRate))
	if err != nil {
		t.Fatalf("UnitsToHarm at the alternative: %v", err)
	}
	if near <= at {
		t.Errorf("a regression of eight hundredths crosses in %v units and one of a tenth in %v, and the smaller one should be slower",
			near, at)
	}
	// And a release running below its baseline never crosses toward harm at all.
	if _, err := theBoundary.UnitsToHarm(baselineRate, baselineRate/2); !errors.Is(err, boundary.ErrNoCrossing) {
		t.Errorf("UnitsToHarm below the baseline = %v, want %v", err, boundary.ErrNoCrossing)
	}
}

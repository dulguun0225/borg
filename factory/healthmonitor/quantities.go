package healthmonitor

import (
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/window"
)

// PooledOperation is the name the series of every operation not read alone is
// kept under. A service's operations are not called alike, and one called a few
// times an hour can never rule a change out at the size in force inside the cap;
// read as one more member of the set it would take passed away from every window
// on the service. So the rest are pooled into one series per quantity per target
// and read as one, and a regression among them still crosses, diluted by their
// combined share and no more.
const PooledOperation = "pooled"

// EmissionShape is one version of what the software the factory writes emits:
// the names on a record, the outcome set, the interval resolution, the histogram
// boundaries and the quantile, and — which is what the health monitor reads off
// it — the quantities a series at that version carries.
//
// A build emits the version the factory that built it ships, so a service moves
// to a new version on its next build and never by an item raised for the
// purpose. The health monitor reads every version the factory has shipped, and a
// comparison whose arms differ in version is read over the quantities both
// carry.
type EmissionShape struct {
	Version    string
	Quantities []gatepolicy.Quantity
}

// EmissionShapes is every emission version the factory has shipped, oldest
// first. A version adds a name or a quantity beside what the version before
// carried and removes nothing until the version after, so a series kept at an
// earlier version is read as it was; a change that cannot be an addition is a
// removal and a version of its own.
var EmissionShapes = []EmissionShape{{
	Version: "emission/1",
	Quantities: []gatepolicy.Quantity{
		gatepolicy.QuantityRequestRate, gatepolicy.QuantityErrorRate,
		gatepolicy.QuantityLatency, gatepolicy.QuantityHazardousOperation,
	},
}, {
	// The second adds the time of each unit of work, which is what the store
	// assigns a record to an interval by. It is an addition and not a removal:
	// the quantities are the same four, and a series kept at the version before
	// is read as it was — one interval per unit of work, which is all a record
	// with no time in it distinguishes.
	Version: "emission/2",
	Quantities: []gatepolicy.Quantity{
		gatepolicy.QuantityRequestRate, gatepolicy.QuantityErrorRate,
		gatepolicy.QuantityLatency, gatepolicy.QuantityHazardousOperation,
	},
}}

// QuantitiesAt is what a series at one emission version carries, and false for a
// version this factory never shipped — which is a series it cannot read.
func QuantitiesAt(version string) ([]gatepolicy.Quantity, bool) {
	for _, shape := range EmissionShapes {
		if shape.Version == version {
			return shape.Quantities, true
		}
	}
	return nil, false
}

// ReadableAcross is the quantities both arms carry, and the quantities the newer
// arm alone carries, which the window records as outside its set for that
// reason. Where the two versions are equal the second list is empty.
func ReadableAcross(release, baseline string) (both, outside []gatepolicy.Quantity, err error) {
	ofRelease, known := QuantitiesAt(release)
	if !known {
		return nil, nil, fmt.Errorf("healthmonitor: the release's arm is at emission version %q, which this factory never shipped", release)
	}
	if baseline == "" || baseline == release {
		return ofRelease, nil, nil
	}
	ofBaseline, known := QuantitiesAt(baseline)
	if !known {
		return nil, nil, fmt.Errorf("healthmonitor: the other arm is at emission version %q, which this factory never shipped", baseline)
	}
	for _, q := range ofRelease {
		if slices.Contains(ofBaseline, q) {
			both = append(both, q)
		} else {
			outside = append(outside, q)
		}
	}
	return both, outside, nil
}

// Series is what the emission returns for one arm pair on one target: the
// intervals per quantity per operation, the emission version each arm was read
// at, and the newest time the store holds a record for the service.
type Series struct {
	EmissionVersionRelease  string
	EmissionVersionBaseline string
	// Newest is the newest time the store holds a record for this service, which
	// the health monitor writes onto its last check. A read whose newest record
	// is older than the interval that last check carries is read as no volume and
	// never as a low one.
	Newest string
	// LatencyBucketShare is the share of completions the histogram bucket the
	// latency quantile falls in holds. It is the floor under the latency size: a
	// change smaller than a bucket is a change the kept series cannot show.
	LatencyBucketShare float64
	Operations         []OperationSeries
}

// OperationSeries is one operation's intervals, per quantity. The operation is
// [PooledOperation] for the series every operation not read alone was pooled
// into.
type OperationSeries struct {
	Operation  string
	Quantities map[gatepolicy.Quantity]boundary.Observed
}

// Evaluated is one window's whole reading over every target, operation and
// quantity: what crossed if anything did, whether every quantity ruled its own
// change out, and the read to store at the close.
type Evaluated struct {
	// Crossed is the first crossing found, and is nil where nothing crossed. A
	// crossing on any one comparison starts the same rollback, so the first is
	// enough to act on and is what the incident names.
	Crossed *Crossing
	// PassedEvery is every quantity on every series having ruled its own change
	// out, which is the conjunction the passed exit is.
	PassedEvery bool
	// Read is the four counts per quantity and the same per target and operation,
	// which is what the window stores its exit against.
	Read window.Read
	// FinestSizeReached is the finest size the traffic reached per quantity, at
	// the power in force.
	FinestSizeReached map[gatepolicy.Quantity]float64
	// Volume is whether any series was read on both arms at all. A window over no
	// volume says nothing about the release.
	Volume bool
	// Newest is the newest time the store held a record for this service over the
	// series read, which the health monitor writes onto its last check: a read
	// whose newest record is older than the interval that check carries is read
	// as no volume and never as a low one.
	Newest string
}

// CrossingKind is which of the three readings crossed. An incident names it,
// because more than one reading runs on one service and a crossing is not
// interpretable without knowing which took it.
type CrossingKind string

const (
	// KindComparison is the comparison against the control, or against the
	// release below on the target where the strategy kept none.
	KindComparison CrossingKind = "comparison"
	// KindOwnHistory is the reading against the service's own recent history,
	// which runs whether or not a window is open.
	KindOwnHistory CrossingKind = "own_history"
	// KindExplicitThreshold is a threshold an owner's safeguard set, read at a run
	// length of its own.
	KindExplicitThreshold CrossingKind = "explicit_threshold"
)

// CrossingKinds is every reading that can cross. The CHECK in package incident
// lists the same three.
var CrossingKinds = []CrossingKind{KindComparison, KindOwnHistory, KindExplicitThreshold}

// Crossing is one reading that crossed: which reading, on which quantity, on
// which series, and the boundary it was read against. It is what an incident
// names, because a crossing is not interpretable against anything but the
// boundary it was actually read against.
type Crossing struct {
	Kind      CrossingKind
	Quantity  gatepolicy.Quantity
	Target    string
	Operation string
	Boundary  boundary.Boundary
	Reading   boundary.Reading
}

// evaluate reads one window's series against its own boundaries: every quantity
// the window carries a size for, on every operation the store holds a series
// for, on every target read. The first crossing is the answer, and passing is
// the conjunction of every series having ruled its own change out.
func evaluate(boundaryFor func(gatepolicy.Quantity) (boundary.Boundary, bool),
	power map[gatepolicy.Quantity]float64,
	target string, series Series, kind CrossingKind, into *Evaluated) error {
	readable, _, err := ReadableAcross(series.EmissionVersionRelease, series.EmissionVersionBaseline)
	if err != nil {
		return err
	}
	if series.Newest > into.Newest {
		into.Newest = series.Newest
	}
	if into.Read.Quantities == nil {
		into.Read.Quantities = map[gatepolicy.Quantity]boundary.Counts{}
		into.FinestSizeReached = map[gatepolicy.Quantity]float64{}
		into.PassedEvery = true
	}

	for _, operation := range series.Operations {
		for _, quantity := range readable {
			observed, read := operation.Quantities[quantity]
			if !read {
				continue
			}
			b, carried := boundaryFor(quantity)
			if !carried {
				continue
			}
			b.Size = boundary.Coarsest(b.Size, floorFor(quantity, series))
			reading, err := b.Evaluate(observed)
			if err != nil {
				return fmt.Errorf("healthmonitor: reading %s of %s on %s: %w", quantity, operation.Operation, target, err)
			}

			counts := observed.Totals()
			into.Read.Quantities[quantity] = into.Read.Quantities[quantity].Add(counts)
			into.Read.Series = append(into.Read.Series, window.SeriesCounts{
				Target: target, Operation: operation.Operation, Quantity: quantity, Counts: counts,
			})
			if reading.Intervals > 0 {
				into.Volume = true
				finest, err := b.FinestSize(reading.Deviation, powerFor(power, quantity), reading.Intervals)
				if err == nil && (into.FinestSizeReached[quantity] == 0 || finest < into.FinestSizeReached[quantity]) {
					into.FinestSizeReached[quantity] = finest
				}
			}
			if !reading.Passed {
				into.PassedEvery = false
			}
			if reading.Failed && into.Crossed == nil {
				into.Crossed = &Crossing{
					Kind: kind, Quantity: quantity, Target: target,
					Operation: operation.Operation, Boundary: b, Reading: reading,
				}
			}
		}
	}
	return nil
}

// floorFor is the floor under a quantity's size that the kept series puts there.
// Only the latency quantile has one: it is read as the share of completions at
// or past the bucket the quantile falls in, so a change smaller than that bucket
// holds is a change the histogram cannot show.
func floorFor(quantity gatepolicy.Quantity, series Series) float64 {
	if quantity == gatepolicy.QuantityLatency {
		return series.LatencyBucketShare
	}
	return 0
}

// powerFor is the power in force for one quantity, and a half where the window
// carries none — which reads the finest size at even odds rather than refusing
// to report one.
func powerFor(power map[gatepolicy.Quantity]float64, quantity gatepolicy.Quantity) float64 {
	if p, authored := power[quantity]; authored && p > 0 && p < 1 {
		return p
	}
	return 0.5
}

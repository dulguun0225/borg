// The analysis window's own rules: which quantity a miss moves the size of, and
// the run the power falls on. They are here rather than in learn_test.go because
// both are read per quantity, and the rest of the pass is not.
package score

import (
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/window"
)

// TestAMissMovesTheQuantityTheIncidentNames: which quantity's size a miss moves
// is the incident's own reading, and an incident that names none — a human's
// undo among them — moves every quantity.
func TestAMissMovesTheQuantityTheIncidentNames(t *testing.T) {
	size, _ := Starting(gatepolicy.WindowSize)

	named := withIncidentOn(evidenceFor("svc_a", closes(1, window.ExitTimedOut), nil),
		"rel_svc_a_0", gatepolicy.QuantityLatency)
	latency := QuantitySubject("svc_a", gatepolicy.QuantityLatency)
	if got := valueOf(t, named, gatepolicy.WindowSize, latency); got != size.Value/2 {
		t.Errorf("a miss on a latency incident supplies a latency size of %v, want %v", got, size.Value/2)
	}
	for _, quantity := range gatepolicy.Quantities {
		if quantity == gatepolicy.QuantityLatency {
			continue
		}
		subject := QuantitySubject("svc_a", quantity)
		if row := rowFor(t, named, gatepolicy.WindowSize, subject); row != nil {
			t.Errorf("a miss on a latency incident moved %s to %v, and the incident named the quantity it crossed on",
				quantity, row.Value)
		}
	}

	none := withIncident(evidenceFor("svc_a", closes(1, window.ExitTimedOut), nil), "rel_svc_a_0")
	for _, quantity := range gatepolicy.Quantities {
		subject := QuantitySubject("svc_a", quantity)
		if got := valueOf(t, none, gatepolicy.WindowSize, subject); got != size.Value/2 {
			t.Errorf("an incident naming no quantity left %s at %v, want the halved %v", quantity, got, size.Value/2)
		}
	}
}

// TestThePowerFallsOnTheQuantityWhoseTrafficReachedItsSize: the size in force is
// one value per quantity, so the run the power falls on is read per quantity
// against that quantity's own size and moves that quantity's power alone.
func TestThePowerFallsOnTheQuantityWhoseTrafficReachedItsSize(t *testing.T) {
	power, _ := Starting(gatepolicy.WindowPower)
	size, _ := Starting(gatepolicy.WindowSize)

	// The newest window reaches the same size on every quantity, which is what
	// the size in force is; the oldest reached a coarser latency, so the latency
	// run is two and the error-rate run is three.
	fine := everyQuantityAt(size.Value / 2)
	coarse := everyQuantityAt(size.Value / 2)
	coarse[gatepolicy.QuantityLatency] = size.Value * 4
	e := withFinestSizePerWindow(evidenceFor("svc_a", closes(3, window.ExitTimedOut), nil),
		[]map[gatepolicy.Quantity]float64{coarse, fine, fine})

	errorRate := QuantitySubject("svc_a", gatepolicy.QuantityErrorRate)
	if got := valueOf(t, e, gatepolicy.WindowPower, errorRate); !near(got, power.Value-windowPowerStep) {
		t.Errorf("the error rate's power reads %v, want the fallen %v", got, power.Value-windowPowerStep)
	}
	latency := QuantitySubject("svc_a", gatepolicy.QuantityLatency)
	if got := valueOf(t, e, gatepolicy.WindowPower, latency); got != power.Value {
		t.Errorf("the latency power reads %v, want the starting %v: two of its three windows reached its size", got, power.Value)
	}
}

// everyQuantityAt is one finest-size reading, the same on every quantity.
func everyQuantityAt(reached float64) map[gatepolicy.Quantity]float64 {
	per := map[gatepolicy.Quantity]float64{}
	for _, quantity := range gatepolicy.Quantities {
		per[quantity] = reached
	}
	return per
}

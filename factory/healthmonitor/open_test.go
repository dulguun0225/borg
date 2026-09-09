// open_test.go is what the open resolves before anything is measured: which
// quantities a window's comparison and its reading against a service's own
// recent history carry a size for, out of the parameters in force and whether
// the service names a hazardous operation.
package healthmonitor_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/policy"
)

// authoredWindowParameters is a size and a power authored for every quantity,
// the hazardous operation's count among them, which is what an owner naming
// four quantities' worth of parameters looks like regardless of whether this
// service's area names one.
func authoredWindowParameters() policy.Window {
	w := policy.Window{Size: map[gatepolicy.Quantity]policy.Effective{}, Power: map[gatepolicy.Quantity]policy.Effective{}}
	for _, q := range gatepolicy.Quantities {
		w.Size[q] = policy.Effective{Number: 0.1}
		w.Power[q] = policy.Effective{Number: 0.8}
	}
	return w
}

// TestTheFourthQuantityIsSizedOnlyWhereTheServiceNamesAHazardousOperation is
// C2008: the size is one value per quantity, three for a service and four
// where its area names a hazardous operation — a fact healthmonitor does not
// derive, since it does not import area, and takes as the plain input its
// caller hands over on [healthmonitor.Watching]. Sizing it never turns on
// whether the store happens to keep the count: a store that keeps none for
// such a service is an unavailable quantity at the read, not a reason to
// narrow the window to three quantities at the open.
func TestTheFourthQuantityIsSizedOnlyWhereTheServiceNamesAHazardousOperation(t *testing.T) {
	parameters := authoredWindowParameters()

	size, power := healthmonitor.SizedQuantities(parameters, false)
	if _, carried := size[gatepolicy.QuantityHazardousOperation]; carried {
		t.Errorf("the comparison carries a size for the hazardous operation on a service whose area names none: %v", size)
	}
	if len(size) != 3 || len(power) != 3 {
		t.Errorf("the comparison carries %d size(s) and %d power(s), want the three ordinary quantities", len(size), len(power))
	}

	size, power = healthmonitor.SizedQuantities(parameters, true)
	if s, carried := size[gatepolicy.QuantityHazardousOperation]; !carried || s != 0.1 {
		t.Errorf("the comparison carries %v for the hazardous operation on a service whose area names one, want the authored size", size)
	}
	if len(size) != 4 || len(power) != 4 {
		t.Errorf("the comparison carries %d size(s) and %d power(s), want all four quantities", len(size), len(power))
	}
}

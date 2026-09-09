// shape_test.go is the versioned emission shape: what one version fixes about
// what the software the factory writes emits and what the store keeps, and the
// refusal of a read naming a version this factory never shipped.
package healthmonitor_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
)

// TestEmissionShapeCarriesTheWholeVersionedShape is C1973: the names on a
// record, the outcome set, the interval resolution, the histogram boundaries
// and the quantile, the failure record's key set, and the unfinished deadline
// are one shape carried per emission version — not the quantity list alone.
func TestEmissionShapeCarriesTheWholeVersionedShape(t *testing.T) {
	for _, one := range []struct {
		version           string
		wantNames         []string
		wantIntervalKnown bool
	}{
		{"emission/1", []string{"outcome"}, false},
		{"emission/2", []string{"time", "outcome"}, true},
	} {
		shape, ok := healthmonitor.ShapeAt(one.version)
		if !ok {
			t.Fatalf("ShapeAt(%q) found no shape, want the one this factory shipped", one.version)
		}
		if len(shape.Names) != len(one.wantNames) {
			t.Errorf("%s names %v, want %v", one.version, shape.Names, one.wantNames)
		}
		if len(shape.OutcomeSet) == 0 {
			t.Errorf("%s names no outcome, want at least the one the store adds", one.version)
		}
		if len(shape.FailureRecordKeySet) == 0 {
			t.Errorf("%s names no failure record key, want the whole key a copy on an incident needs", one.version)
		}
		if (shape.IntervalResolution > 0) != one.wantIntervalKnown {
			t.Errorf("%s has interval resolution %v, want a resolution present: %t", one.version, shape.IntervalResolution, one.wantIntervalKnown)
		}
	}

	if _, ok := healthmonitor.ShapeAt("emission/9"); ok {
		t.Error("ShapeAt found a shape for a version this factory never shipped")
	}
}

// TestAReadNamingAnUnshippedEmissionVersionIsRefused is what
// [healthmonitor.ReadableAcross] already refuses, kept read against the whole
// shape table and not the quantity list on its own: a comparison whose arm
// names a version the table lacks is a series this factory cannot read.
func TestAReadNamingAnUnshippedEmissionVersionIsRefused(t *testing.T) {
	if _, _, err := healthmonitor.ReadableAcross("emission/9", ""); err == nil {
		t.Error("ReadableAcross read a release arm at a version this factory never shipped")
	}
	if _, _, err := healthmonitor.ReadableAcross("emission/1", "emission/9"); err == nil {
		t.Error("ReadableAcross read a baseline arm at a version this factory never shipped")
	}
	both, outside, err := healthmonitor.ReadableAcross("emission/1", "emission/1")
	if err != nil || len(outside) != 0 || len(both) != len(gatepolicy.Quantities) {
		t.Errorf("ReadableAcross(emission/1, emission/1) = %v, %v, %v; want every quantity and nothing outside", both, outside, err)
	}
}

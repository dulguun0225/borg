package score

import (
	"context"
	"testing"
)

// TestAnExposureListNobodyDerivedIsUnavailableAndNotNothing: a diff adding none
// of this reads as nothing and lowers the number; a list nobody derived reads as
// unavailable and resolves the factor. The two are the opposite response, and
// the zero value is the second.
func TestAnExposureListNobodyDerivedIsUnavailableAndNotNothing(t *testing.T) {
	s := &Score{}
	ctx := context.Background()

	nobody, err := s.exposure(ctx, Change{})
	if err != nil {
		t.Fatalf("exposure: %v", err)
	}
	if nobody.unavailable == "" {
		t.Errorf("an exposure evidence nobody derived read as %v, and it is unavailable", nobody.level)
	}

	empty, err := s.exposure(ctx, Change{Exposure: ExposureEvidence{Derived: true}})
	if err != nil {
		t.Fatalf("exposure: %v", err)
	}
	if empty.unavailable != "" || empty.level != 0 {
		t.Errorf("a derived list with nothing in it read as %q at %v, want nothing at 0", empty.unavailable, empty.level)
	}

	some, err := s.exposure(ctx, Change{Exposure: ExposureEvidence{
		Derived:           true,
		DependencyChanges: []string{"pkg/one@2.0.0 (MIT)"},
	}})
	if err != nil {
		t.Fatalf("exposure: %v", err)
	}
	if some.unavailable != "" || some.level <= 0 {
		t.Errorf("a derived list with one entry read as %q at %v", some.unavailable, some.level)
	}
}

// TestTheFleetReadingsResolveWhereNothingReadTheFleetsRecords: the two fleet
// readings are the caller's, no component writes one yet, and a share of nothing
// is not a reading of a version nobody works from.
func TestTheFleetReadingsResolveWhereNothingReadTheFleetsRecords(t *testing.T) {
	s := &Score{}
	ctx := context.Background()

	for _, read := range []func(context.Context, Change) (reading, error){s.fleetShare, s.fleetDeparture} {
		unread, err := read(ctx, Change{})
		if err != nil {
			t.Fatalf("reading a fleet factor: %v", err)
		}
		if unread.unavailable == "" {
			t.Errorf("a fleet reading nobody took read as %v, and it is unavailable", unread.level)
		}
		taken, err := read(ctx, Change{Fleet: FleetChange{Derived: true, ShareWorkingFromIt: 0.5, Departure: 0.5}})
		if err != nil {
			t.Fatalf("reading a fleet factor: %v", err)
		}
		if taken.unavailable != "" || taken.level != 0.5 {
			t.Errorf("a fleet reading that was taken read as %q at %v, want 0.5", taken.unavailable, taken.level)
		}
	}
}

package gatepolicy

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// TestElevenRows is the count gate policy states about itself, held against the
// parameters: one row carries the window's size, confidence and power, and every
// other row carries one parameter.
func TestElevenRows(t *testing.T) {
	rows := Rows()
	if len(rows) != 11 {
		t.Fatalf("Definitions name %d rows, and gate policy is eleven: %v", len(rows), rows)
	}
	if len(Definitions) != 13 {
		t.Fatalf("Definitions hold %d parameters, want thirteen over the eleven rows", len(Definitions))
	}
	for _, row := range rows {
		n := 0
		for _, d := range Definitions {
			if d.Row == row {
				n++
			}
		}
		switch {
		case row == RowWindowSizeConfidencePower && n != 3:
			t.Errorf("row %q carries %d parameters, want the size, the confidence and the power", row, n)
		case row != RowWindowSizeConfidencePower && n != 1:
			t.Errorf("row %q carries %d parameters, want one", row, n)
		}
	}
}

// TestSevenCeilingsAndFiveFloors: twelve of the thirteen parameters are clamped
// by a safeguard and the thirteenth adds a human, and which way each clamp points
// is the design's own list rather than something read off the row.
func TestSevenCeilingsAndFiveFloors(t *testing.T) {
	ceilings := []Parameter{
		WindowSize, WindowLimit, HeldOutSampleRate, AttemptLimit, ItemSizeTarget, ExposureBound, AdvisorySeverity,
	}
	floors := []Parameter{
		WindowConfidence, WindowPower, WindowCap, ReviewSampleRate, AllowedPredicateKinds,
	}
	if len(ceilings) != 7 || len(floors) != 5 {
		t.Fatalf("the test names %d ceilings and %d floors, want seven and five", len(ceilings), len(floors))
	}
	for _, d := range Definitions {
		want := DirectionAddsAHuman
		switch {
		case slices.Contains(ceilings, d.Parameter):
			want = DirectionCeiling
		case slices.Contains(floors, d.Parameter):
			want = DirectionFloor
		case d.Parameter != RiskThreshold:
			t.Errorf("%q is in neither list, so the count of seven and five no longer covers the parameters", d.Parameter)
			continue
		}
		if d.Direction != want {
			t.Errorf("a safeguard on %q is a %q, want %q", d.Parameter, d.Direction, want)
		}
	}
}

// TestEveryParameterIsDefinedOnce: Define answers for every parameter of the
// three lists, no name is listed twice, and a name outside them is refused rather
// than resolving to a zero definition.
func TestEveryParameterIsDefinedOnce(t *testing.T) {
	seen := map[Parameter]bool{}
	for _, d := range slices.Concat(Definitions, NotAmongTheEleven, SafeguardOnly) {
		if seen[d.Parameter] {
			t.Errorf("%q is defined twice", d.Parameter)
		}
		seen[d.Parameter] = true
		if d.Kind == "" || d.Direction == "" || d.Scope == "" || d.Limits == "" {
			t.Errorf("%q is missing part of its definition: %+v", d.Parameter, d)
		}
		if (d.Row != "") != slices.Contains(Definitions, d) {
			t.Errorf("%q names row %q, and a row is what the eleven have and nothing else does", d.Parameter, d.Row)
		}
		got, err := Define(d.Parameter)
		if err != nil || got.Parameter != d.Parameter {
			t.Errorf("Define(%q) = %+v, %v", d.Parameter, got, err)
		}
	}
	if _, err := Define("no_such_parameter"); !errors.Is(err, ErrUnknown) {
		t.Errorf("Define of an unknown name = %v, want ErrUnknown", err)
	}
}

// TestOnlyTheThresholdAddsAHuman: the risk threshold's safeguard adds a human and
// carries no bound, and every other parameter of the eleven rows is clamped.
func TestOnlyTheThresholdAddsAHuman(t *testing.T) {
	for _, d := range Definitions {
		adds := d.Direction == DirectionAddsAHuman
		if adds != (d.Parameter == RiskThreshold) {
			t.Errorf("%q has direction %q", d.Parameter, d.Direction)
		}
	}
}

// TestOnlyAFloorOverAListReachesClampList: three parameters hold a list, and
// only a floor over one is clamped — by union, which is what "a safeguard may
// add a period or lengthen one" and "a kind of assertion added is coverage
// added" each are. The paging hours are the third and no safeguard reaches them,
// so nothing unions them.
func TestOnlyAFloorOverAListReachesClampList(t *testing.T) {
	lists := []Parameter{AllowedPredicateKinds, ChangeFreeze, PagingHours}
	for _, d := range slices.Concat(Definitions, NotAmongTheEleven) {
		if (d.Kind == KindList) != slices.Contains(lists, d.Parameter) {
			t.Errorf("%q is of kind %q", d.Parameter, d.Kind)
		}
		if d.Kind == KindList && d.Direction != DirectionFloor && d.Direction != DirectionNone {
			t.Errorf("a safeguard on the list %q is a %q, and a list is clamped by union or not at all",
				d.Parameter, d.Direction)
		}
	}
}

// TestTheItemSizeTargetIsCountedInRequirements: the unit is the count of the
// intent's requirements an item answers, which decomposition sets, and not lines.
func TestTheItemSizeTargetIsCountedInRequirements(t *testing.T) {
	d, err := Define(ItemSizeTarget)
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	if want := "requirements"; !strings.Contains(d.Unit, want) {
		t.Errorf("the item-size target's unit is %q, want the count of %s an item answers", d.Unit, want)
	}
	if strings.Contains(d.Unit, "lines") {
		t.Errorf("the item-size target's unit is %q, and the design authors it in requirements", d.Unit)
	}
}

// TestTheReadingsSizesArePerQuantity: one value per quantity, because a
// detectable change in an error rate and one in a latency quantile are not one
// number. Five parameters are keyed that way and no other is — the window's size
// and power, the explicit threshold with the size beside it, which is read on
// one quantity and says nothing about another, and the size of the reading
// against a service's own recent history.
func TestTheReadingsSizesArePerQuantity(t *testing.T) {
	perQuantityParameters := []Parameter{
		WindowSize, WindowPower, ExplicitThreshold, ExplicitThresholdSize, RecentHistorySize,
	}
	for _, d := range slices.Concat(Definitions, NotAmongTheEleven, SafeguardOnly) {
		perQuantity := d.Key == KeyQuantity
		want := slices.Contains(perQuantityParameters, d.Parameter)
		if perQuantity != want {
			t.Errorf("%q is keyed %q", d.Parameter, d.Key)
		}
	}
	if len(Quantities) != 4 {
		t.Fatalf("the health monitor's quantities are %v, want the three every service emits and the fourth an irreversible area names", Quantities)
	}
	for _, q := range Quantities {
		got, err := DecidableQuantity(string(q))
		if err != nil || got != q {
			t.Errorf("DecidableQuantity(%q) = %q, %v", q, got, err)
		}
	}
	if _, err := DecidableQuantity("saturation"); !errors.Is(err, ErrQuantityUnknown) {
		t.Errorf("a quantity the health monitor does not read = %v, want ErrQuantityUnknown", err)
	}
}

// TestTheAttemptLimitIsOneParameterAndNotThree: the interview's rounds and
// decomposition's re-decompositions are two more subjects of the attempt limit's
// own key, and not two more parameters.
func TestTheAttemptLimitIsOneParameterAndNotThree(t *testing.T) {
	d, err := Define(AttemptLimit)
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	if d.Key != KeyStage {
		t.Errorf("the attempt limit is keyed %q, want the stage", d.Key)
	}
	for _, other := range slices.Concat(Definitions, NotAmongTheEleven, SafeguardOnly) {
		if other.Parameter != AttemptLimit && strings.Contains(string(other.Parameter), "attempt") {
			t.Errorf("%q is a second attempt limit, and it is one parameter and not three", other.Parameter)
		}
	}
}

// TestAuthoredAndNotAmongTheEleven: the retention values, the two report-channel
// rates, the remediation period, the harm mark's cap, the strategy default and
// the ceiling on candidate environments, and the twelve fields the design names
// on the service record with the values authored beside them, are authored and
// are not gate policy's rows — so they carry no row and are still resolvable, and
// each names the direction the design gives it rather than one read off the row.
func TestAuthoredAndNotAmongTheEleven(t *testing.T) {
	directions := map[Parameter]Direction{
		DecisionLogRetention:  DirectionFloor,
		ReportRetention:       DirectionCeiling,
		BackupRetention:       DirectionNone,
		RetentionFloor:        DirectionNone,
		RemediationPeriod:     DirectionCeiling,
		ReportChannelRate:     DirectionCeiling,
		HarmMarkPageCap:       DirectionCeiling,
		ExplicitThreshold:     DirectionAdds,
		ExplicitThresholdSize: DirectionCeiling,

		StrategyDefault:                    DirectionAdds,
		MaxConcurrentCandidateEnvironments: DirectionNone,

		// The design's twelve on the service record: the bake volume and the
		// mutation floor a safeguard may raise and never lower, the rest of the
		// supplied six it may lower and never raise, and six authored outright
		// with nothing supplied — of which only the freeze admits a safeguard.
		BakeVolume:              DirectionFloor,
		MutationFloor:           DirectionFloor,
		BacklogCap:              DirectionCeiling,
		SearchBudget:            DirectionCeiling,
		RecentHistorySize:       DirectionCeiling,
		RecentHistoryRunLength:  DirectionCeiling,
		Objective:               DirectionNone,
		KeptFraction:            DirectionNone,
		MaxConcurrentKeptFleets: DirectionNone,
		PagingHours:             DirectionNone,
		ProofTestRate:           DirectionNone,
		ChangeFreeze:            DirectionFloor,

		// Authored beside them on the same record.
		InstanceHourRate:    DirectionNone,
		EnvironmentHourRate: DirectionNone,
		OperationCap:        DirectionCeiling,
		MutantCap:           DirectionNone,
		FailureRecordKeyCap: DirectionCeiling,
		UnreliableBound:     DirectionFloor,
		IncidentItemBound:   DirectionNone,
		SnapshotRetention:   DirectionCeiling,
	}
	if len(NotAmongTheEleven) != len(directions) {
		t.Fatalf("NotAmongTheEleven holds %d parameters, the test names %d", len(NotAmongTheEleven), len(directions))
	}
	for _, d := range NotAmongTheEleven {
		want, named := directions[d.Parameter]
		if !named {
			t.Errorf("%q is not one the test names", d.Parameter)
			continue
		}
		if d.Direction != want {
			t.Errorf("a safeguard on %q is a %q, want %q", d.Parameter, d.Direction, want)
		}
		if d.Row != "" {
			t.Errorf("%q names row %q, and it is authored and not among the eleven", d.Parameter, d.Row)
		}
	}
}

// TestTheTwelveOnTheServiceRecord: the design names twelve fields on the service
// record beside the window limit and the analysis window's parameters, and every
// one of them is a parameter here whose scope is that record — which is what
// lets a safeguard bind it.
func TestTheTwelveOnTheServiceRecord(t *testing.T) {
	twelve := []Parameter{
		ExplicitThreshold, Objective, KeptFraction, MaxConcurrentKeptFleets,
		RecentHistorySize, RecentHistoryRunLength, PagingHours, ProofTestRate,
		ChangeFreeze, BacklogCap, SearchBudget, BakeVolume, MutationFloor,
	}
	// Thirteen names for twelve fields: the size and the average run length of
	// the reading against a service's own recent history are one of the twelve
	// and two numbers, as the design states them.
	if len(twelve) != 13 {
		t.Fatalf("the test names %d parameters for the design's twelve fields", len(twelve))
	}
	for _, parameter := range twelve {
		d, err := Define(parameter)
		if err != nil {
			t.Errorf("Define(%q): %v", parameter, err)
			continue
		}
		if d.Scope != ScopeService {
			t.Errorf("%q is scoped to %q, and the design makes it a field of the service record", parameter, d.Scope)
		}
		if d.Row != "" {
			t.Errorf("%q names row %q, and it is authored and not among the eleven", parameter, d.Row)
		}
	}
}

// TestTheTwoParametersOnlyASafeguardSetsSayWhatReadsThem:
// [Definition.ReaderAtThisMilestone] is what an owner reads to see whether a
// bound they place changes anything, so a parameter nothing reads yet names
// nothing rather than the mechanism that would read it. A safeguard's predicate
// reaches enforcement; a maximum age on the drift detector's last check reaches
// nothing, no hold at the production deploy row carrying one.
func TestTheTwoParametersOnlyASafeguardSetsSayWhatReadsThem(t *testing.T) {
	readers := map[Parameter]string{
		SafeguardPredicate:           "enforcement, beside the consumer contracts derived from a consumer's build",
		DriftDetectorLastCheckMaxAge: "",
	}
	if len(SafeguardOnly) != len(readers) {
		t.Fatalf("SafeguardOnly holds %d parameters, the test names %d", len(SafeguardOnly), len(readers))
	}
	for _, d := range SafeguardOnly {
		want, named := readers[d.Parameter]
		if !named {
			t.Errorf("%q is not one the test names", d.Parameter)
			continue
		}
		if d.ReaderAtThisMilestone != want {
			t.Errorf("%q says %q reads it, want %q", d.Parameter, d.ReaderAtThisMilestone, want)
		}
	}
}

// TestASafeguardNeverWidens is the rule stated as arithmetic: a ceiling over a
// value already lower leaves it, a floor under a value already higher leaves it,
// and neither moves a value the wrong way.
func TestASafeguardNeverWidens(t *testing.T) {
	cases := []struct {
		direction Direction
		bound     float64
		value     float64
		want      float64
	}{
		{DirectionCeiling, 5, 2, 2},        // the authored two stands against a safeguard's five
		{DirectionCeiling, 2, 5, 2},        // the safeguard caps the wider value
		{DirectionFloor, 0.9, 0.95, 0.95},  // the authored confidence is already higher
		{DirectionFloor, 0.9, 0.5, 0.9},    // the safeguard raises the weaker value
		{DirectionAddsAHuman, 0, 0.3, 0.3}, // a safeguard on the threshold moves no number
		{DirectionAdds, 0.9, 0.3, 0.3},     // a safeguard that adds a check moves no number either
		{DirectionNone, 7, 0.3, 0.3},       // nothing clamps a parameter no safeguard reaches
	}
	for _, c := range cases {
		if got := Clamp(c.direction, c.bound, c.value); got != c.want {
			t.Errorf("Clamp(%s, %v, %v) = %v, want %v", c.direction, c.bound, c.value, got, c.want)
		}
	}
}

// TestClampListIsTheUnion: a safeguard on a list may only extend the value in
// force, and the answer does not depend on the order the safeguards were applied
// in.
func TestClampListIsTheUnion(t *testing.T) {
	got := ClampList([]string{"read", "populated"}, []string{"populated", "unit"})
	want := []string{"populated", "read", "unit"}
	if !slices.Equal(got, want) {
		t.Fatalf("ClampList = %v, want %v", got, want)
	}
	// The value in force is not edited in place: a caller holding the slice it
	// passed in still holds what it passed.
	value := []string{"unit"}
	ClampList([]string{"read"}, value)
	if !slices.Equal(value, []string{"unit"}) {
		t.Fatalf("ClampList edited its input: %v", value)
	}
}

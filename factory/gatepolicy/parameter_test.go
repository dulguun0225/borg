package gatepolicy

import (
	"errors"
	"slices"
	"testing"
)

// TestSevenRows is the count gate policy states about itself, held against a
// list of eight parameters: one row carries two values and every other carries
// one.
func TestSevenRows(t *testing.T) {
	rows := Rows()
	if len(rows) != 7 {
		t.Fatalf("Definitions name %d rows, and gate policy is seven: %v", len(rows), rows)
	}
	if len(Definitions) != 8 {
		t.Fatalf("Definitions hold %d parameters, want eight over the seven rows", len(Definitions))
	}
	shared := 0
	for _, row := range rows {
		n := 0
		for _, d := range Definitions {
			if d.Row == row {
				n++
			}
		}
		if n == 2 {
			shared++
		} else if n != 1 {
			t.Errorf("row %q carries %d parameters, want one or the two of the window's row", row, n)
		}
	}
	if shared != 1 {
		t.Errorf("%d rows carry two parameters, want the window's size and confidence alone", shared)
	}
}

// TestEveryParameterIsDefinedOnce: Define answers for each of the eight, no
// name is listed twice, and a name outside them is refused rather than
// resolving to a zero definition.
func TestEveryParameterIsDefinedOnce(t *testing.T) {
	seen := map[Parameter]bool{}
	for _, d := range Definitions {
		if seen[d.Parameter] {
			t.Errorf("%q is defined twice", d.Parameter)
		}
		seen[d.Parameter] = true
		if d.Row == "" || d.Kind == "" || d.Direction == "" || d.Scope == "" {
			t.Errorf("%q is missing part of its definition: %+v", d.Parameter, d)
		}
		got, err := Define(d.Parameter)
		if err != nil || got != d {
			t.Errorf("Define(%q) = %+v, %v", d.Parameter, got, err)
		}
	}
	if _, err := Define("no_such_parameter"); !errors.Is(err, ErrUnknown) {
		t.Errorf("Define of an unknown name = %v, want ErrUnknown", err)
	}
}

// TestOnlyTheThresholdAddsAHuman: the risk threshold's pin adds a human and
// carries no bound, and every other parameter's pin is a number or a list that
// clamps.
func TestOnlyTheThresholdAddsAHuman(t *testing.T) {
	for _, d := range Definitions {
		adds := d.Direction == DirectionAddsAHuman
		if adds != (d.Parameter == RiskThreshold) {
			t.Errorf("%q has direction %q", d.Parameter, d.Direction)
		}
	}
}

// TestOnlyTheCatalogIsAList: a list-valued parameter is clamped by union, and
// the catalog is the only one, so nothing else reaches ClampList.
func TestOnlyTheCatalogIsAList(t *testing.T) {
	for _, d := range Definitions {
		if (d.Kind == KindList) != (d.Parameter == PredicateCatalog) {
			t.Errorf("%q is of kind %q", d.Parameter, d.Kind)
		}
	}
}

// TestAPinNeverWidens is the rule stated as arithmetic: a ceiling over a value
// already lower leaves it, a floor under a value already higher leaves it, and
// neither moves a value the wrong way.
func TestAPinNeverWidens(t *testing.T) {
	cases := []struct {
		direction Direction
		bound     float64
		value     float64
		want      float64
	}{
		{DirectionCeiling, 5, 2, 2},        // the authored two stands against a pinned five
		{DirectionCeiling, 2, 5, 2},        // the pin caps the wider value
		{DirectionFloor, 0.9, 0.95, 0.95},  // the authored confidence is already higher
		{DirectionFloor, 0.9, 0.5, 0.9},    // the pin raises the weaker value
		{DirectionAddsAHuman, 0, 0.3, 0.3}, // a threshold pin moves no number
	}
	for _, c := range cases {
		if got := Clamp(c.direction, c.bound, c.value); got != c.want {
			t.Errorf("Clamp(%s, %v, %v) = %v, want %v", c.direction, c.bound, c.value, got, c.want)
		}
	}
}

// TestClampListIsTheUnion: a pinned catalog may only extend the value in force,
// and the answer does not depend on the order the pins were applied in.
func TestClampListIsTheUnion(t *testing.T) {
	got := ClampList([]string{"status", "field-present"}, []string{"field-present", "schema"})
	want := []string{"field-present", "schema", "status"}
	if !slices.Equal(got, want) {
		t.Fatalf("ClampList = %v, want %v", got, want)
	}
	// The value in force is not edited in place: a caller holding the slice it
	// passed in still holds what it passed.
	value := []string{"schema"}
	ClampList([]string{"status"}, value)
	if !slices.Equal(value, []string{"schema"}) {
		t.Fatalf("ClampList edited its input: %v", value)
	}
}

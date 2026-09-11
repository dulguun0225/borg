// The published convention this factory version composes for the Go
// extractor: that it names all five of the mark, the backfill, the schema
// change, a screen's transition function, and the mutation tool, beside the
// mirror consumercontract states of its own. Reaches no database.
package main

import (
	"strings"
	"testing"
)

func TestThePublishedGoConventionNamesAllFive(t *testing.T) {
	for _, want := range []struct{ what, substring string }{
		{"the mark", "deprecated"},
		{"the backfill", "backfill."},
		{"the schema change", "migrations"},
		{"a screen's transition function", "Transition"},
		{"the mutation tool", "mutation tool"},
	} {
		if !strings.Contains(publishedGoConvention, want.substring) {
			t.Errorf("the published convention does not name %s (%q): %s",
				want.what, want.substring, publishedGoConvention)
		}
	}
}

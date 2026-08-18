package intent

import (
	"strings"
	"testing"
)

// TestDDLListsEverySourceAndState keeps the CHECK constraints and the two Go
// lists from disagreeing, the way TestDDLListsEveryShape does for the
// decision log's shapes.
func TestDDLListsEverySourceAndState(t *testing.T) {
	ddl := strings.Join(DDL, "\n")

	sources := listed(t, ddl, "source in (")
	if len(sources) != len(Sources) {
		t.Fatalf("the constraint lists %d sources, Sources has %d", len(sources), len(Sources))
	}
	for n, s := range Sources {
		if got, want := sources[n], "'"+string(s)+"'"; got != want {
			t.Errorf("the constraint lists %s where Sources has %s", got, want)
		}
	}

	states := listed(t, ddl, "state in (")
	if len(states) != len(States) {
		t.Fatalf("the constraint lists %d states, States has %d", len(states), len(States))
	}
	for n, s := range States {
		if got, want := states[n], "'"+string(s)+"'"; got != want {
			t.Errorf("the constraint lists %s where States has %s", got, want)
		}
	}
}

// listed is the comma-separated list that follows open in ddl, trimmed.
func listed(t *testing.T, ddl, open string) []string {
	t.Helper()
	i := strings.Index(ddl, open)
	if i < 0 {
		t.Fatalf("the DDL has no %q list", open)
	}
	rest := ddl[i+len(open):]
	j := strings.Index(rest, ")")
	if j < 0 {
		t.Fatalf("the %q list is not closed", open)
	}
	parts := strings.Split(rest[:j], ",")
	for n := range parts {
		parts[n] = strings.TrimSpace(parts[n])
	}
	return parts
}

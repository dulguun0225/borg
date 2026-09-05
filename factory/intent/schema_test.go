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

// TestDDLListsEveryKindAndPattern keeps the kind_known and pattern_known CHECK
// constraints and [Kinds] and [Patterns] from disagreeing. The pattern list
// carries one entry [Patterns] does not, the empty pattern a statement fitting
// none of the six is admitted with, and the test checks for it after the six.
func TestDDLListsEveryKindAndPattern(t *testing.T) {
	ddl := strings.Join(DDL, "\n")

	kinds := listed(t, ddl, "kind_known check (kind in")
	if len(kinds) != len(Kinds) {
		t.Fatalf("the constraint lists %d kinds, Kinds has %d", len(kinds), len(Kinds))
	}
	for n, k := range Kinds {
		if got, want := kinds[n], "'"+string(k)+"'"; got != want {
			t.Errorf("the constraint lists %s where Kinds has %s", got, want)
		}
	}

	patterns := listed(t, ddl, "pattern_known check (pattern in")
	if len(patterns) != len(Patterns)+1 {
		t.Fatalf("the constraint lists %d patterns, Patterns has %d plus the empty one", len(patterns), len(Patterns))
	}
	for n, p := range Patterns {
		if got, want := patterns[n], "'"+string(p)+"'"; got != want {
			t.Errorf("the constraint lists %s where Patterns has %s", got, want)
		}
	}
	if got := patterns[len(Patterns)]; got != "''" {
		t.Errorf("the constraint's last pattern is %s, want the empty pattern a fit-none statement escapes with", got)
	}
}

// listed is the comma-separated list that follows open in ddl, trimmed. open
// may end at the list's own opening paren (as "source in (" does) or stop
// short of it (as "pattern_known check (pattern in" does, the list opening on
// the next line): either way, listed skips to the first "(" it finds after
// open and reads up to the matching ")".
func listed(t *testing.T, ddl, open string) []string {
	t.Helper()
	i := strings.Index(ddl, open)
	if i < 0 {
		t.Fatalf("the DDL has no %q list", open)
	}
	rest := strings.TrimLeft(ddl[i+len(open):], " \t\r\n")
	rest = strings.TrimPrefix(rest, "(")
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

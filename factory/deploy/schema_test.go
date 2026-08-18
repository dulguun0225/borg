package deploy

import (
	"strings"
	"testing"
)

// TestDDLListsEveryStrategyAndStatus keeps the CHECK constraints and the Go
// lists from disagreeing, the way decisionlog's TestDDLListsEveryShape does
// for shapes.
func TestDDLListsEveryStrategyAndStatus(t *testing.T) {
	ddl := strings.Join(DDL, "\n")

	assertList := func(open string, want []string) {
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
		listed := strings.Split(rest[:j], ",")
		if len(listed) != len(want) {
			t.Fatalf("the constraint lists %d values, the Go list has %d", len(listed), len(want))
		}
		for n, value := range want {
			if got := strings.TrimSpace(listed[n]); got != "'"+value+"'" {
				t.Errorf("the constraint lists %s where the Go list has '%s'", got, value)
			}
		}
	}

	strategies := make([]string, len(Strategies))
	for n, s := range Strategies {
		strategies[n] = string(s)
	}
	assertList("strategy in (", strategies)

	statuses := make([]string, len(Statuses))
	for n, s := range Statuses {
		statuses[n] = string(s)
	}
	assertList("status in (", statuses)
}

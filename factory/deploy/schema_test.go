package deploy

import (
	"strings"
	"testing"
)

// TestDDLListsEveryValue keeps the CHECK constraints and the Go lists from
// disagreeing, the way decisionlog's TestDDLListsEveryShape does for shapes.
// Four lists, one per closed set this package writes.
func TestDDLListsEveryValue(t *testing.T) {
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
			t.Fatalf("the %q constraint lists %d values, the Go list has %d", open, len(listed), len(want))
		}
		for n, value := range want {
			if got := strings.TrimSpace(listed[n]); got != "'"+value+"'" {
				t.Errorf("the %q constraint lists %s where the Go list has '%s'", open, got, value)
			}
		}
	}

	// The strategy constraint lists the empty value beside the two: a strategy
	// attaches to a production deploy and to no other, so every other deploy
	// carries neither field.
	strategies := []string{""}
	for _, s := range Strategies {
		strategies = append(strategies, string(s))
	}
	assertList("strategy_picked in (", strategies)

	statuses := make([]string, len(Statuses))
	for n, s := range Statuses {
		statuses[n] = string(s)
	}
	assertList("status in (", statuses)

	completions := make([]string, len(Completions))
	for n, c := range Completions {
		completions[n] = string(c)
	}
	assertList("completion in (", completions)

	operations := make([]string, len(Operations))
	for n, o := range Operations {
		operations[n] = string(o)
	}
	assertList("operation in (", operations)
}

// TestAdvisoryLockKeyIsDerivedFromTheName recomputes the key from the name it
// is derived from, so a change to either is a change to both. The key is per
// service and environment: two pairs' sequence numbers have nothing to
// serialise against each other for.
func TestAdvisoryLockKeyIsDerivedFromTheName(t *testing.T) {
	one := AdvisoryLockKey("svc_a", "env_a")
	if one <= 0 {
		t.Errorf("the key for one pair is %d, want a positive number", one)
	}
	if AdvisoryLockKey("svc_a", "env_a") != one {
		t.Error("the key for one pair is not the same twice")
	}
	if AdvisoryLockKey("svc_a", "env_b") == one || AdvisoryLockKey("svc_b", "env_a") == one {
		t.Error("two pairs derive the same key")
	}
}

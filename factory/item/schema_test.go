package item

import (
	"strings"
	"testing"
)

// TestDDLListsEveryStage keeps the two stage CHECK constraints — one per
// table — and [StageOrder] from disagreeing, the way TestDDLListsEveryShape
// does for the decision log's shapes.
//
// The count it ends on is the two tables and not every statement of [DDL],
// which also creates the index the reading of an intent's items follows.
func TestDDLListsEveryStage(t *testing.T) {
	const open = "stage in ("
	const tables = 2
	found := 0
	for _, statement := range DDL {
		i := strings.Index(statement, open)
		if i < 0 {
			continue
		}
		found++
		rest := statement[i+len(open):]
		j := strings.Index(rest, ")")
		if j < 0 {
			t.Fatalf("the %q list is not closed", open)
		}
		listed := strings.Split(rest[:j], ",")
		if len(listed) != len(EveryStage) {
			t.Fatalf("a constraint lists %d stages, EveryStage has %d", len(listed), len(EveryStage))
		}
		for n, s := range EveryStage {
			if got, want := strings.TrimSpace(listed[n]), "'"+string(s)+"'"; got != want {
				t.Errorf("a constraint lists %s where EveryStage has %s", got, want)
			}
		}
	}
	if found != tables {
		t.Fatalf("%d of %d tables carry the stage CHECK, want every one", found, tables)
	}
}

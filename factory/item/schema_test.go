package item

import (
	"strings"
	"testing"
)

// TestDDLListsEveryStage keeps the two stage CHECK constraints — one per
// table — and [StageOrder] from disagreeing, the way TestDDLListsEveryShape
// does for the decision log's shapes.
func TestDDLListsEveryStage(t *testing.T) {
	const open = "stage in ("
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
		if len(listed) != len(StageOrder) {
			t.Fatalf("a constraint lists %d stages, StageOrder has %d", len(listed), len(StageOrder))
		}
		for n, s := range StageOrder {
			if got, want := strings.TrimSpace(listed[n]), "'"+string(s)+"'"; got != want {
				t.Errorf("a constraint lists %s where StageOrder has %s", got, want)
			}
		}
	}
	if found != len(DDL) {
		t.Fatalf("%d of %d tables carry the stage CHECK, want every one", found, len(DDL))
	}
}

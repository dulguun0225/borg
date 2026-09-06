package score

import (
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
)

// TestAHumansUndoIsTheRollbackTheFactoryDidNotCallFor: the per-author prior
// counts the changes a human undid after they shipped, and a rollback names the
// source that called for it. The two the factory calls for itself are not undos:
// the health monitor's is already counted as the window's failed exit, and the
// search's returns traffic to where it was.
func TestAHumansUndoIsTheRollbackTheFactoryDidNotCallFor(t *testing.T) {
	undo := func(source, failed string) deploy.Deploy {
		return deploy.Deploy{Undoing: deploy.Undoing{FailedReleaseID: failed, Source: source}}
	}
	for _, c := range []struct {
		what string
		roll deploy.Deploy
		want bool
	}{
		{"the health monitor at the failed exit", undo(deploy.SourceHealthMonitorAtFailed, "rel-1"), false},
		{"the search", undo(deploy.SourceSearch, "rel-1"), false},
		{"a named human at Ops", undo(deploy.SourceOfHuman("person:ops", "the feature was wrong"), "rel-1"), true},
		{"a deploy that failed nothing", undo(deploy.SourceOfHuman("person:ops", "x"), ""), false},
	} {
		if got := humansUndo(c.roll); got != c.want {
			t.Errorf("a rollback from %s reads as a human's undo = %v, want %v", c.what, got, c.want)
		}
	}
}

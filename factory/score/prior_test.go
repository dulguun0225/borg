package score

import (
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
)

// TestAHumansUndoIsTheRollbackTheFactoryDidNotCallFor: the per-author prior
// counts the changes a human undid after they shipped, and a rollback names the
// source that called for it. The one the factory calls for itself is not an
// undo: the health monitor's is already counted as the window's failed exit.
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
		{"a named human at Ops", undo(deploy.SourceOfHuman("person:ops", "the feature was wrong"), "rel-1"), true},
		{"a deploy that failed nothing", undo(deploy.SourceOfHuman("person:ops", "x"), ""), false},
	} {
		if got := humansUndo(c.roll); got != c.want {
			t.Errorf("a rollback from %s reads as a human's undo = %v, want %v", c.what, got, c.want)
		}
	}
}

// TestOnlyABatchOfSeveralReleasesUnderOneWindowRulesNothingOut: what excludes a
// release from the prior is that one window's close was a comparison over
// several changes, which is the deploy that window watched having delivered
// several. It is not what raised the item the deploy was of: a revert is an
// item the factory raised itself, and so is every item a detector raised, whose
// own release is watched by a window of its own and rules something out like
// any other.
func TestOnlyABatchOfSeveralReleasesUnderOneWindowRulesNothingOut(t *testing.T) {
	for _, c := range []struct {
		what      string
		delivered []string
		want      bool
	}{
		{"a revert's deploy delivering the releases the hold was holding", []string{"rel-1", "rel-2", "rel-3"}, true},
		{"a deploy of one release", []string{"rel-1"}, false},
		{"a deploy naming nothing delivered", nil, false},
		{"a batch this release was not in", []string{"rel-2", "rel-3"}, false},
	} {
		if got := underOneWindow("rel-1", c.delivered); got != c.want {
			t.Errorf("%s reads as delivered in a batch = %v, want %v", c.what, got, c.want)
		}
	}
}

// TestAnUnseenAuthorStartsAtThePriorTheProductShippedForItsModelVersion: an
// author the factory has not seen starts wide, or at the prior the product
// shipped for that model version where the version carries one, and narrows
// from there as its own closes support something narrower.
func TestAnUnseenAuthorStartsAtThePriorTheProductShippedForItsModelVersion(t *testing.T) {
	unseen := Version{}
	if width, from := priorFloor(unseen, "model-1", 0); width != 1 || from != "" {
		t.Errorf("an unseen author with no shipped prior starts at %v (%q), want the width no closes support", width, from)
	}

	shippedFor := Version{ShippedPriors: map[string]float64{"model-1": 0.4}}
	width, from := priorFloor(shippedFor, "model-1", 0)
	if !near(width, 0.4) {
		t.Errorf("an author of a model version the product shipped a prior for starts at %v, want 0.4", width)
	}
	if from == "" {
		t.Error("the reading does not say the width came from the prior the product shipped")
	}
	if other, _ := priorFloor(shippedFor, "model-2", 0); other != 1 {
		t.Errorf("an author of another model version starts at %v, want the width no closes support", other)
	}

	// Evidence narrows it past what the product shipped, and the width rule is
	// what it narrows by from there: a shipped prior is where the prior starts
	// and not a floor under its own closes.
	narrowed, _ := priorFloor(shippedFor, "model-1", 15)
	if !near(narrowed, priorWidth(15)) {
		t.Errorf("fifteen closes leave the prior at %v, want the width they support (%v)", narrowed, priorWidth(15))
	}
}

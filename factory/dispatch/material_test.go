// TestMaterialAClassTheEntryDoesNotNameIsWithheldBeforeTheRun is the classes of
// material a fleet entry may be handed, read at every dispatch. Split from
// db_test.go by subject, sharing its fixtures and its package.
package dispatch_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/intent"
)

// TestMaterialAClassTheEntryDoesNotNameIsWithheldBeforeTheRun is
// ../../end-goal/how-the-factory-works/10-fleet/01-what-an-agent-runs-on.md:
// a class the entry does not name is withheld before the selection rule selects
// anything, the manifest records each withheld source as excluded with the
// entry as the reason, and the run record's sources name only what was sent.
func TestMaterialAClassTheEntryDoesNotNameIsWithheldBeforeTheRun(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
	c.withdrawEveryEntry(t)
	c.anEntryPerRole(t, "", []string{fleetentry.ClassRepository})
	it := c.oneItem(t, intent.StateRefined)

	_, run, err := c.dispatch.SpecAuthor(c.ctx, on(it), []inputmanifest.Material{
		{Class: fleetentry.ClassIntentStatement, Reference: it.IntentID, Bytes: 17},
		{Class: fleetentry.ClassRepository, Reference: oneService, Bytes: 40},
	}, agent.Refining{Statement: "s"})
	if err != nil {
		t.Fatalf("SpecAuthor: %v", err)
	}

	manifest, err := inputmanifest.Get(c.ctx, c.pool, run.InputManifestID)
	if err != nil {
		t.Fatalf("Get the manifest: %v", err)
	}
	if len(manifest.Materials) != 1 || manifest.Materials[0].Class != fleetentry.ClassRepository {
		t.Fatalf("the manifest was handed %+v, want the one class the entry names", manifest.Materials)
	}
	if len(manifest.Excluded) != 1 || manifest.Excluded[0].What != it.IntentID {
		t.Fatalf("the manifest excluded %+v, want the withheld source by reference", manifest.Excluded)
	}
	reason := manifest.Excluded[0].Reason
	if !strings.Contains(reason, run.Entry.ID) || !strings.Contains(reason, fleetentry.ClassIntentStatement) {
		t.Errorf("the reason is %q, want the entry and the class it does not name", reason)
	}
	// The bound the entry carries is on the manifest, which is what says a read
	// was made under it.
	if manifest.ReadAtOnceBound == nil || *manifest.ReadAtOnceBound != run.Entry.ReadsAtOnce {
		t.Errorf("the manifest's read-at-once bound is %v, want the entry's", manifest.ReadAtOnceBound)
	}

	runs, err := agentrun.ForItem(c.ctx, c.pool, it.ID)
	if err != nil {
		t.Fatalf("ForItem: %v", err)
	}
	if len(runs) != 1 || len(runs[0].Sources) != 1 || runs[0].Sources[0] != oneService {
		t.Errorf("the run names sources %v, want only what was handed over", runs)
	}

	// A class no fleet entry could ever name is refused rather than withheld:
	// the classes an entry names and the classes a stage hands over are one
	// vocabulary.
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, on(it),
		[]inputmanifest.Material{{Class: "a class nobody defined", Reference: "x"}},
		agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrMaterialClassUnknown) {
		t.Errorf("material of an unknown class = %v, want ErrMaterialClassUnknown", err)
	}
}

// A human-raised revert, and which item is one.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// TestARevertARequestNamesPassesTheEvidenceOn is a human-raised revert at Ops:
// the named human names the failed release, and intake writes that link as the
// intent's evidence the same way it does for a detector's own revert. Whether
// the resulting item is a revert is read off that link and never off which of
// the two sources raised it.
func TestARevertARequestNamesPassesTheEvidenceOn(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	shipped := only(t, res)

	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}
	svc, err := service.Get(ctx, d.pool, shipped.svc.ID)
	if err != nil {
		t.Fatalf("reading the service: %v", err)
	}

	if err := p.revertIntent(ctx, svc, shipped.releaseID, "it broke checkout"); err != nil {
		t.Fatalf("revertIntent: %v", err)
	}
	revert, found, err := intent.OnEvidence(ctx, d.pool, intent.Evidence{ServiceID: svc.ID, ReleaseID: shipped.releaseID})
	if err != nil || !found {
		t.Fatalf("OnEvidence over the revert = found %v, %v", found, err)
	}
	if revert.Source != intent.SourceOwner {
		t.Errorf("the revert's source is %s, want owner", revert.Source)
	}

	revertItem, err := item.NewDecomposition(d.pool, d.token).Create(ctx, decompositionActor, item.New{
		IntentID: revert.ID, ServiceID: svc.ID, Branch: "item/revert",
	}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing the revert item: %v", err)
	}
	isRevert, err := p.IsARevert(ctx, revertItem)
	if err != nil {
		t.Fatalf("IsARevert on the revert item: %v", err)
	}
	if !isRevert {
		t.Error("IsARevert on a human's revert item = false, want true: the evidence names the release it undoes")
	}

	// An ordinary item, decomposed from the intent the run itself authored,
	// carries no evidence and is not a revert.
	ordinary, err := item.NewDecomposition(d.pool, d.token).Create(ctx, decompositionActor, item.New{
		IntentID: res.decompositions[0].intentID, ServiceID: svc.ID, Branch: "item/ordinary",
	}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing the ordinary item: %v", err)
	}
	isRevert, err = p.IsARevert(ctx, ordinary)
	if err != nil {
		t.Fatalf("IsARevert on the ordinary item: %v", err)
	}
	if isRevert {
		t.Error("IsARevert on an ordinary item = true, want false: it carries no evidence")
	}
}

// TestRollbackRevertRefusedWithoutARelease is the CLI's own refusal: -revert
// names the release it undoes, and asking for one without naming it is refused
// before anything opens the store.
func TestRollbackRevertRefusedWithoutARelease(t *testing.T) {
	err := rollbackCommand([]string{"demo", "-reason", "it broke checkout", "-revert"})
	if err == nil {
		t.Fatal("rollback -revert with no -release was accepted")
	}
	if want := "-release"; !strings.Contains(err.Error(), want) {
		t.Errorf("the refusal is %q, want it to name %q", err.Error(), want)
	}
}

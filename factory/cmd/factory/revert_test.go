// A human-raised revert, and which item is one.
package main

import (
	"net/http"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/screens"
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

	s := newScreens(t, ctx, d, out)
	p := s.p
	svc, err := service.Get(ctx, d.pool, shipped.svc.ID)
	if err != nil {
		t.Fatalf("reading the service: %v", err)
	}

	s.mustCall(t, "raiseRevert", screens.RaiseRevertArgs{
		ServiceID: svc.ID, ReleaseID: shipped.releaseID, Reason: "it broke checkout",
	})
	revert, found, err := intent.OnEvidence(ctx, d.pool, intent.Evidence{ServiceID: svc.ID, ReleaseID: shipped.releaseID})
	if err != nil || !found {
		t.Fatalf("OnEvidence over the revert = found %v, %v", found, err)
	}
	if revert.Source != intent.SourceOwner {
		t.Errorf("the revert's source is %s, want owner", revert.Source)
	}

	revertItem, err := item.NewDecomposition(d.pool, d.token).Create(ctx, decompositionActor, item.New{
		IntentID: revert.ID, ServiceID: svc.ID, Branch: "item/revert",
		RequirementsAnswered: oneRequirement,
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
		RequirementsAnswered: oneRequirement,
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

// TestARevertIsRefusedWithoutAReasonOrTheRightRelease is what Ops refuses
// before anything is written: the record says what the undo was for, and the
// release it undoes is a release of the service it names.
func TestARevertIsRefusedWithoutAReasonOrTheRightRelease(t *testing.T) {
	ctx, d, out := newPathOn(t, theAnswer+"\n"+approvals, theService, theSecondService)
	s := newScreens(t, ctx, d, out)
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("ByName(%s) = found %v, %v", theService, found, err)
	}
	other, found, err := service.ByName(ctx, d.pool, theSecondService)
	if err != nil || !found {
		t.Fatalf("ByName(%s) = found %v, %v", theSecondService, found, err)
	}

	if status, body := s.call(t, "raiseRevert", screens.RaiseRevertArgs{
		ServiceID: svc.ID, ReleaseID: "rel_1",
	}); status == http.StatusNoContent || status == http.StatusOK {
		t.Errorf("a revert with no reason was accepted: %s", body)
	}
	if status, body := s.call(t, "raiseRevert", screens.RaiseRevertArgs{
		ServiceID: other.ID, ReleaseID: "rel_1", Reason: "it broke checkout",
	}); status == http.StatusNoContent || status == http.StatusOK {
		t.Errorf("a revert naming a release nobody minted was accepted: %s", body)
	}
}

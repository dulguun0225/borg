// The deployer's fifth reachability field: whether the emission already
// reported traffic when the deployer adopted the service.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

func TestAdoptionResolvesSourceAtSpecOnceAndLaterItemsWeighIt(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading the service: found %t, %v", found, err)
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning adoption: %v", err)
	}
	if err := service.Adopt(ctx, tx, d.token, owner(t, ctx, d.pool, d.token, d.human), svc.ID,
		service.Reachability{TargetReached: true, InstancesReplaceable: true,
			RollbackPathPresent: true, EmissionReadable: true, TakingTraffic: true}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("recording the existing service shape: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing adoption: %v", err)
	}

	first, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the adoption run stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, first)
	opening := openingOf(t, ctx, d, c.specGate.opening)
	if !hasResolution(opening.Resolutions, score.CauseAdoptedRepository) {
		t.Fatalf("adoption Spec did not resolve its source: %+v", opening.Resolutions)
	}

	second, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the post-adoption run stopped: %v\noutput so far:\n%s", err, out)
	}
	post := only(t, second)
	postOpening := openingOf(t, ctx, d, post.specGate.opening)
	if hasResolution(postOpening.Resolutions, score.CauseAdoptedRepository) {
		t.Fatal("the later item resolved its source as adoption again")
	}
}

func hasResolution(resolved []score.Resolution, cause score.Cause) bool {
	for _, one := range resolved {
		if one.Cause == cause {
			return true
		}
	}
	return false
}

// TestAdoptionSetsTakingTrafficFromTheEmission: the fixture's deployed process
// emits continuously, so by the time the first production deploy adopts the
// service the emission already reports a request rate above zero, and the
// deployer reads that off the emission rather than writing a fixed true.
func TestAdoptionSetsTakingTrafficFromTheEmission(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the run stopped: %v\noutput so far:\n%s", err, out)
	}

	svc, err := service.Get(ctx, d.pool, res.serviceID)
	if err != nil {
		t.Fatalf("reading the service: %v", err)
	}
	if !svc.Reachability.Written() {
		t.Fatal("the deployer wrote nothing on the service, want every field written at the first release")
	}
	if !svc.Reachability.TakingTraffic {
		t.Error("Reachability.TakingTraffic = false, want true: the fixture's process emits continuously")
	}
	if !svc.Reachability.EmissionReadable {
		t.Error("Reachability.EmissionReadable = false, want true: the same emission answers both on this platform")
	}
}

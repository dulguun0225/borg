// The deployer's fifth reachability field: whether the emission already
// reported traffic when the deployer adopted the service.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/service"
)

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

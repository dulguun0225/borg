// Decomposition reaching a service record that already exists rather than
// creating a second one.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/service"
)

// TestDecompositionReachesAnExistingService writes the service record before the run
// and asserts the path reaches it rather than creating a second one. The
// service's name is unique in the store, so a decomposition that created every run
// would be refused by that constraint from the second item on that service
// onwards — a later change, or this one run again after a reject.
func TestDecompositionReachesAnExistingService(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	// The service record is already there — newPath writes it, the analysis window's
	// four being fields of it and having to be authored before the first window opens.
	// What this test is about is what decomposition does with one it finds.
	before, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading the service before the run: found %v, %v", found, err)
	}

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	if res.serviceID != before.ID {
		t.Errorf("decomposition used service %s, the record it should have reached is %s", res.serviceID, before.ID)
	}

	var services int
	if err := d.pool.QueryRow(ctx, `select count(*) from `+service.Table+` where name = $1`, theService).Scan(&services); err != nil {
		t.Fatalf("counting the services named %q: %v", theService, err)
	}
	if services != 1 {
		t.Errorf("%d services are named %q, decomposition writes a service's identity once", services, theService)
	}
}

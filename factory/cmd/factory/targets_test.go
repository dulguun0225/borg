// The set every reader of targets reads: the service's own, and the
// environment's whole list where the service record names none.
package main

import (
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/service"
)

// TestEveryReaderReadsTheServicesOwnTargets: a project is not one placement, so
// an environment's targets and a service's are different sets, and every reader
// here reads the second. A service naming none runs on every target of the
// environment, which is what an unwritten field means, and an address the
// environment does not hold serves no share.
func TestEveryReaderReadsTheServicesOwnTargets(t *testing.T) {
	env := environment.Environment{Targets: []environment.Target{
		{Address: "edge", ServesAShare: true},
		{Address: "batch"},
		{Address: "regional", ServesAShare: true},
	}}

	runsOnTwo := service.Service{Targets: []string{"batch", "regional"}}
	if got := serviceAddresses(env, runsOnTwo); !slices.Equal(got, []string{"batch", "regional"}) {
		t.Errorf("the addresses of a service on two of three targets are %v, want batch and regional", got)
	}

	namesNone := service.Service{}
	if got := serviceAddresses(env, namesNone); !slices.Equal(got, []string{"edge", "batch", "regional"}) {
		t.Errorf("the addresses of a service naming no target are %v, want every target of the environment", got)
	}

	shares := serviceTargets(env, runsOnTwo)
	if len(shares) != 2 || shares[0].ServesAShare || !shares[1].ServesAShare {
		t.Errorf("the targets of a service on batch and regional are %+v, want what the environment declares of each", shares)
	}

	unheld := service.Service{Targets: []string{"regional", "withdrawn"}}
	got := serviceTargets(env, unheld)
	if len(got) != 2 || got[1].Address != "withdrawn" || got[1].ServesAShare {
		t.Errorf("an address the environment does not hold reads as %+v, want a target serving no share", got)
	}
}

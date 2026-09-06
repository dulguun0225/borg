// What ends a service and what ends a project: the owner's write on the service
// record, the removal it calls the deployer for, and the project ended once
// every service in it is retired.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/service"
)

// TestRetiringAServiceRemovesItAndEndsTheProject is
// ../../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/04-retirement.md's
// "A service ends the way it began, by an owner's write on its record", reached
// through the entrance an owner has: the write is refused while an unmerged item
// names the service, it calls the deployer's removal once nothing does, the
// service then has no current release, and the project ends with production's
// environment for it.
func TestRetiringAServiceRemovesItAndEndsTheProject(t *testing.T) {
	ctx, d, out := newPath(t, approvals)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)

	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}
	svc, err := service.Get(ctx, d.pool, c.svc.ID)
	if err != nil {
		t.Fatalf("reading the service: %v", err)
	}

	// The run's item merged, so nothing unmerged names the service and the
	// three counts are nothing. A retirement while one stood is what the write
	// is refused on, which the counts carry to package service.
	if _, found, err := deploy.Current(ctx, d.pool, svc.ID, p.production.ID, p.productionAddresses()); err != nil || !found {
		t.Fatalf("the service has no current release before it is retired = found %v, %v", found, err)
	}
	if err := p.retire(ctx, svc); err != nil {
		t.Fatalf("retiring %s: %v\noutput:\n%s", svc.Name, err, out)
	}

	read, err := service.Get(ctx, d.pool, svc.ID)
	if err != nil {
		t.Fatalf("reading the service back: %v", err)
	}
	if !read.Retired() {
		t.Errorf("the service reads as standing after the owner retired it: %+v", read)
	}
	// The removal is what changes what every reader reads: current release is
	// the newest complete deploy record, and a removal's names none.
	if _, found, err := deploy.Current(ctx, d.pool, svc.ID, p.production.ID, p.productionAddresses()); err != nil || found {
		t.Errorf("the retired service still has a current release = found %v, %v", found, err)
	}

	// The project ends once every service in it is retired, and production's
	// environment for it is withdrawn in the same write.
	running, err := servicesWithACurrentRelease(ctx, p)
	if err != nil {
		t.Fatalf("counting what still runs: %v", err)
	}
	if running != 0 {
		t.Fatalf("%d service(s) still have a current release, and every one of them was retired", running)
	}
	if _, err := p.factory.EndProject(ctx, p.human, p.projectID, running); err != nil {
		t.Fatalf("ending the project: %v", err)
	}
	ended, err := project.Get(ctx, d.pool, p.projectID)
	if err != nil {
		t.Fatalf("reading the project: %v", err)
	}
	if ended.EndedAt == "" {
		t.Errorf("the project reads as standing after it was ended: %+v", ended)
	}
	production, found, err := environment.Production(ctx, d.pool, p.projectID)
	if err != nil || !found {
		t.Fatalf("Production = found %v, %v", found, err)
	}
	if production.WithdrawnAt == "" {
		t.Errorf("production's environment stands after the project ended: %+v", production)
	}
}

// TestTheRetirementSubcommandsAreReachable: both are on the switch and both
// refuse an invocation naming too little to act on, which is the shape every
// other owner-write subcommand takes.
func TestTheRetirementSubcommandsAreReachable(t *testing.T) {
	if err := retireCommand(nil); err == nil || !strings.Contains(err.Error(), "one argument") {
		t.Errorf("retire with no service = %v, want the argument named", err)
	}
	if err := retireCommand([]string{"demo"}); err == nil || !strings.Contains(err.Error(), "-secrets") {
		t.Errorf("retire with no secrets = %v, want the flag named", err)
	}
	if err := endProjectCommand(nil); err == nil || !strings.Contains(err.Error(), "-secrets") {
		t.Errorf("end-project with no secrets = %v, want the flag named", err)
	}
	for _, name := range []string{"retire", "end-project"} {
		if err := chosen([]string{name}); err != nil && strings.Contains(err.Error(), subcommands) {
			t.Errorf("%s is not one of the subcommands the switch answers", name)
		}
	}
}

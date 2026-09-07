// What ends a service and what ends a project: the owner's write on the service
// record at Factory, the removal it calls the deployer for, and the project
// ended once every service in it is retired.
package main

import (
	"net/http"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
)

// TestRetiringAServiceRemovesItAndEndsTheProject is
// ../../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/04-retirement.md's
// "A service ends the way it began, by an owner's write on its record", reached
// through the entrance the design gives it — Factory — over HTTP: the write
// calls the deployer's removal, the service then has no current release, and
// the project ends with production's environment for it.
func TestRetiringAServiceRemovesItAndEndsTheProject(t *testing.T) {
	ctx, d, out := newPath(t, approvals)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)

	s := newScreens(t, ctx, d, out)
	svc, err := service.Get(ctx, d.pool, c.svc.ID)
	if err != nil {
		t.Fatalf("reading the service: %v", err)
	}

	// The run's item merged, so nothing unmerged names the service and the
	// three counts are nothing. A retirement while one stood is what the write
	// is refused on, which the counts carry to package service.
	if _, found, err := deploy.Current(ctx, d.pool, svc.ID, s.p.production.ID, serviceAddresses(s.p.production, svc)); err != nil || !found {
		t.Fatalf("the service has no current release before it is retired = found %v, %v", found, err)
	}

	// A project cannot end while a service in it still holds a current
	// release: retiring each is what removes it, and the order is the owner's.
	if status, body := s.call(t, "endProject", screens.EndProjectArgs{}); status == http.StatusNoContent ||
		status == http.StatusOK {
		t.Fatalf("the project ended while a service still held a current release: %s", body)
	}

	s.mustCall(t, "retireService", screens.RetireServiceArgs{ServiceID: svc.ID})

	read, err := service.Get(ctx, d.pool, svc.ID)
	if err != nil {
		t.Fatalf("reading the service back: %v", err)
	}
	if !read.Retired() {
		t.Errorf("the service reads as standing after the owner retired it: %+v", read)
	}
	// The removal is what changes what every reader reads: current release is
	// the newest complete deploy record, and a removal's names none.
	if _, found, err := deploy.Current(ctx, d.pool, svc.ID, s.p.production.ID, serviceAddresses(s.p.production, svc)); err != nil || found {
		t.Errorf("the retired service still has a current release = found %v, %v", found, err)
	}

	// The project ends once every service in it is retired, and production's
	// environment for it is withdrawn in the same write.
	running, err := servicesWithACurrentRelease(ctx, s.p, s.p.projectID)
	if err != nil {
		t.Fatalf("counting what still runs: %v", err)
	}
	if running != 0 {
		t.Fatalf("%d service(s) still have a current release, and every one of them was retired", running)
	}
	s.mustCall(t, "endProject", screens.EndProjectArgs{})

	ended, err := project.Get(ctx, d.pool, s.p.projectID)
	if err != nil {
		t.Fatalf("reading the project: %v", err)
	}
	if ended.EndedAt == "" {
		t.Errorf("the project reads as standing after it was ended: %+v", ended)
	}
	production, found, err := environment.Production(ctx, d.pool, s.p.projectID)
	if err != nil || !found {
		t.Fatalf("Production = found %v, %v", found, err)
	}
	if production.WithdrawnAt == "" {
		t.Errorf("production's environment stands after the project ended: %+v", production)
	}
}

// The deployer's fifth reachability field: whether the emission already
// reported traffic when the deployer adopted the service.
package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
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

// TestAdoptionKeepsTheCustomerBuildAsTheControl: the first factory deploy of
// an adopted service has no factory release below it, so the deployer reads the
// customer's running build and names the adoption as its control.
func TestAdoptionKeepsTheCustomerBuildAsTheControl(t *testing.T) {
	ctx, d, out := newPath(t, ctxInput(t))
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading the service: found %v, %v", found, err)
	}
	production, found, err := environment.Production(ctx, d.pool, svc.ProjectID)
	if err != nil || !found {
		t.Fatalf("reading production: found %v, %v", found, err)
	}
	shareDir := t.TempDir()
	ownerActor := owner(t, ctx, d.pool, d.token, d.human)
	if err := environment.NewWriter(d.pool, d.token).AddTarget(ctx, ownerActor, production.ID,
		environment.Target{Address: shareDir, ServesAShare: true}); err != nil {
		t.Fatalf("adding the traffic target: %v", err)
	}
	if _, err := newFactory(d.pool, d.token).SetServiceTargets(ctx, ownerActor, svc.ID,
		[]string{shareDir}, []string{shareDir}); err != nil {
		t.Fatalf("assigning the service to the traffic target: %v", err)
	}
	d.targets.make = func(dir string) targetseam.Target { return localtarget.New(dir) }
	buildCustomer(t, shareDir)
	customer := localtarget.New(shareDir)
	if _, err := customer.Deploy(ctx, deployerPrincipal, targetseam.Deployment{
		Service: svc.Name, Build: "customer_head", Credential: d.credential,
		Configuration: targetseam.ValueSet{Names: []string{targetseam.DeployIDName}, Values: []string{"customer"}},
	}); err != nil {
		t.Fatalf("starting the customer's build: %v", err)
	}
	t.Cleanup(func() {
		if _, err := customer.Stop(context.Background(), deployerPrincipal, svc.Name, d.credential); err != nil {
			t.Errorf("stopping the customer's build: %v", err)
		}
	})
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning adoption: %v", err)
	}
	if err := service.Adopt(ctx, tx, d.token, ownerActor, svc.ID, service.Reachability{
		TargetReached: true, InstancesReplaceable: true, RollbackPathPresent: true,
		EmissionReadable: true, TakingTraffic: true,
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("recording adoption: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing adoption: %v", err)
	}
	if _, err := newFactory(d.pool, d.token).AuthorHeldOutSampleRate(ctx, ownerActor, 0.99); err != nil {
		t.Fatalf("authoring the held-out sample rate: %v", err)
	}
	d.draw = alwaysDraw{}
	d.decide = scriptedAtWork(approvals).decide
	result, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the adoption run stopped: %v\noutput so far:\n%s", err, out)
	}
	candidate := only(t, result)
	dep, err := deploy.Get(ctx, d.pool, candidate.deployID)
	if err != nil {
		t.Fatalf("reading the adoption deploy: %v", err)
	}
	if dep.StrategyPerformed != deploy.StrategyWithControl {
		t.Fatalf("adoption strategy performed = %q, want with control", dep.StrategyPerformed)
	}
	targets, err := deploy.Targets(ctx, d.pool, dep.ID)
	if err != nil {
		t.Fatalf("reading adoption targets: %v", err)
	}
	for _, target := range targets {
		if !target.NotRunHere && (target.ControlReleaseID != dep.ReleaseID || target.ControlBuildID != "customer_head") {
			t.Fatalf("adoption control = release %q build %q, want adoption %q and customer_head",
				target.ControlReleaseID, target.ControlBuildID, dep.ReleaseID)
		}
	}
	opening := openingOf(t, ctx, d, candidate.deployGate.opening)
	if opening.Strategy.Strategy != gate.StrategyWithControl || opening.Strategy.Share == 0 {
		t.Fatalf("adoption pick = %+v, want a picked share with control", opening.Strategy)
	}
}

func ctxInput(t *testing.T) string {
	t.Helper()
	return theAnswer + "\n" + approvals
}

func buildCustomer(t *testing.T, dir string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(source, []byte("package main\nimport \"time\"\nfunc main(){time.Sleep(time.Hour)}\n"), 0o600); err != nil {
		t.Fatalf("writing customer source: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "customer_head"), source)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building customer source: %v\n%s", err, output)
	}
}

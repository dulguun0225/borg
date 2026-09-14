package main

import (
	"os"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/window"
)

// TestHeldOutReleaseTakesAControlOnATrafficShiftingTarget holds the second
// release out of the gate it would have gated, then verifies that its deploy
// starts the first release as control and asks the target for a bounded share.
func TestHeldOutReleaseTakesAControlOnATrafficShiftingTarget(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
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
		t.Fatalf("adding the traffic-shifting target: %v", err)
	}
	if _, err := newFactory(d.pool, d.token).SetServiceTargets(ctx, ownerActor, svc.ID,
		[]string{shareDir}, []string{shareDir}); err != nil {
		t.Fatalf("assigning the service to the traffic-shifting target: %v", err)
	}
	d.targets.make = func(dir string) targetseam.Target { return localtarget.New(dir) }

	first, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the first release stopped: %v\noutput so far:\n%s", err, out)
	}
	if _, err := newFactory(d.pool, d.token).AuthorGateThreshold(ctx, ownerActor, production.ID,
		gate.DeployToProduction.String(), 0.1); err != nil {
		t.Fatalf("authoring the production threshold: %v", err)
	}
	if _, err := newFactory(d.pool, d.token).AuthorHeldOutSampleRate(ctx, ownerActor, 0.99); err != nil {
		t.Fatalf("authoring the held-out sample rate: %v", err)
	}
	d.draw = alwaysDraw{}
	d.decide = scriptedAtWork(approvals).decide
	second, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the held-out release stopped: %v\noutput so far:\n%s", err, out)
	}
	firstCandidate, secondCandidate := only(t, first), only(t, second)
	dep, err := deploy.Get(ctx, d.pool, secondCandidate.deployID)
	if err != nil {
		t.Fatalf("reading the held-out deploy: %v", err)
	}
	if dep.StrategyPicked != deploy.StrategyWithControl || dep.StrategyPerformed != deploy.StrategyWithControl {
		opening := openingOf(t, ctx, d, secondCandidate.deployGate.opening)
		t.Fatalf("held-out deploy strategies are picked %q and performed %q, want with control; gate strategy=%+v number=%v threshold=%v held_out=%v why=%q candidate=%v merge=%v",
			dep.StrategyPicked, dep.StrategyPerformed, opening.Strategy,
			opening.Number, opening.Threshold,
			secondCandidate.deployGate.heldOut, secondCandidate.deployGate.whyHeldOut,
			secondCandidate.candidateGate.heldOut, secondCandidate.mergeGate.heldOut)
	}
	targets, err := deploy.Targets(ctx, d.pool, dep.ID)
	if err != nil {
		t.Fatalf("reading the held-out deploy targets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("held-out deploy has %d target rows, want the environment's two rows", len(targets))
	}
	var reached deploy.Target
	for _, target := range targets {
		if !target.NotRunHere {
			reached = target
		}
	}
	if reached.ControlReleaseID == "" || reached.ControlBuildID == "" || reached.Fleets.Control.Instances == 0 {
		t.Fatalf("held-out target names no running control: %+v", reached)
	}
	firstDeploy, err := deploy.Get(ctx, d.pool, firstCandidate.deployID)
	if err != nil {
		t.Fatalf("reading the first deploy: %v", err)
	}
	if reached.ControlReleaseID != firstCandidate.releaseID || reached.ControlBuildID != firstDeploy.BuildID {
		t.Errorf("control is release %s build %s, want release %s build %s",
			reached.ControlReleaseID, reached.ControlBuildID, firstCandidate.releaseID, firstDeploy.BuildID)
	}
	opened, found, err := window.ForRelease(ctx, d.pool, secondCandidate.releaseID)
	if err != nil || !found {
		t.Fatalf("reading the held-out window: found %v, %v", found, err)
	}
	if !opened.HeldOut || opened.PassedAvailable {
		t.Errorf("held-out window is held_out=%v passed_available=%v, want true and false", opened.HeldOut, opened.PassedAvailable)
	}
	traffic, err := os.ReadFile(localtarget.TrafficFile(shareDir, svc.Name))
	if err != nil {
		t.Fatalf("reading the target's traffic file: %v", err)
	}
	if !strings.Contains(string(traffic), dep.BuildID+" 0.1") {
		t.Fatalf("traffic file is %q, want the held-out build at the picked share", traffic)
	}
}

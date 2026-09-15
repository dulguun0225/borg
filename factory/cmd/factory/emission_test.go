package main

import (
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/window"
)

// TestM11BadLatencyCrossesWithAControl is the command-level proof of the
// versioned emission path: the bad M4 release keeps its error rate flat, but
// one operation's latency moves across the boundary while the target retains
// the control build.
func TestM11BadLatencyCrossesWithAControl(t *testing.T) {
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

	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the control release stopped: %v\noutput so far:\n%s", err, out)
	}
	if _, _, err := newFactory(d.pool, d.token).AddSafeguard(ctx, ownerActor,
		gatepolicy.StrategyDefault, safeguard.Subject{Kind: safeguard.SubjectService, ID: svc.ID},
		safeguard.Bound{}, safeguard.Routing{}); err != nil {
		t.Fatalf("keeping a control for the service: %v", err)
	}

	d.decide = scriptedAtWork(approvals).decide
	d.model = &fakeModel{interviewCalls: 1, slow: true}
	path := p(ctx, t, d)
	bad := authorOne(t, ctx, path, theSecondStatement, out)
	if err := path.candidateEnvironment(ctx, bad); err != nil {
		t.Fatalf("building the bad M4 release: %v\noutput so far:\n%s", err, out)
	}
	if err := path.mergeGate(ctx, bad); err != nil {
		t.Fatalf("merging the bad M4 release: %v\noutput so far:\n%s", err, out)
	}
	if _, err := path.runQueue(ctx, theServiceRecord(t, ctx, path)); err != nil {
		t.Fatalf("running the merge queue: %v\noutput so far:\n%s", err, out)
	}
	if err := path.productionDeploy(ctx, bad); err != nil {
		t.Fatalf("deploying the bad M4 release: %v\noutput so far:\n%s", err, out)
	}
	dep, err := deploy.Get(ctx, d.pool, bad.deployID)
	if err != nil {
		t.Fatalf("reading the bad deploy: %v", err)
	}
	if dep.StrategyPerformed != deploy.StrategyWithControl {
		t.Fatalf("bad deploy performed strategy %q, want with control", dep.StrategyPerformed)
	}
	targets, err := deploy.Targets(ctx, d.pool, dep.ID)
	if err != nil {
		t.Fatalf("reading bad deploy targets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("bad deploy target does not retain the control fleet: %+v", targets)
	}
	var controlInstances int
	for _, target := range targets {
		controlInstances += target.Fleets.Control.Instances
	}
	if controlInstances == 0 {
		t.Fatalf("bad deploy target does not retain the control fleet: %+v", targets)
	}
	controlBuild := ""
	signalDir := ""
	for _, target := range targets {
		if target.ControlBuildID != "" {
			controlBuild = target.ControlBuildID
		}
		if !target.NotRunHere {
			signalDir = target.Address
		}
	}
	if controlBuild == "" || signalDir == "" {
		t.Fatalf("bad deploy target names no reached control target: %+v", targets)
	}

	// The store does not report an interval until its unfinished deadline has
	// passed since the interval ended.
	time.Sleep(1200 * time.Millisecond)
	reading, err := healthmonitor.NewFileEmission(signalDir, localtarget.SignalFile).Read(ctx, healthmonitor.Reading{
		ServiceName: svc.Name, Target: signalDir,
		Release:  healthmonitor.Arm{BuildID: dep.BuildID, DeployID: dep.ID},
		Baseline: healthmonitor.Arm{BuildID: controlBuild, DeployID: dep.ID},
	})
	if err != nil || reading.EmissionVersionRelease != "emission/3" ||
		reading.EmissionVersionBaseline != "emission/3" || len(reading.Operations) == 0 {
		t.Fatalf("reading the written release and control signals: series=%+v, err=%v", reading, err)
	}
	var watched []healthmonitor.Watched
	for range 15 {
		watched, err = path.healthMonitor.Watch(ctx, healthmonitor.Watching{
			ID: svc.ID, Name: svc.Name, EnvironmentID: production.ID,
		})
		if err != nil || (len(watched) == 1 && watched[0].Exit != "") {
			break
		}
		time.Sleep(60 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("watching the latency crossing: %v\noutput so far:\n%s", err, out)
	}
	if len(watched) != 1 || watched[0].Exit != window.ExitFailed {
		t.Fatalf("latency-only emission produced %+v, want one failed window", watched)
	}
	if watched[0].Evaluated.Crossed == nil {
		t.Fatal("latency-only emission failed the window without a crossing")
	}
}

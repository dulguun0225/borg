package main

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/window"
)

func TestPassRecordsFewerKeptInstancesAsAMismatch(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	first := shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	closeWindowOver(ctx, t, s.factory, token, svc.ID, first, dir, window.ExitTimedOut)
	rolling := startReleaseKeeping(ctx, t, s.factory, token, svc, env, "c2", 2)
	openWindowOver(ctx, t, s.factory, token, svc.ID, rolling, dir, 3600)
	recordRunning(t, dir, testServiceName, rolling.BuildID)
	recordKept(t, dir, testServiceName, first.BuildID)

	out := &strings.Builder{}
	if err := pass(ctx, s, out, credential, func(dir string) targetseam.Target { return localtarget.New(dir) }); err != nil {
		t.Fatalf("pass: %v", err)
	}
	uncleared, err := driftdetector.Uncleared(ctx, s.own, svc.ID)
	if err != nil {
		t.Fatalf("Uncleared: %v", err)
	}
	if len(uncleared) != 1 {
		t.Fatalf("Uncleared = %+v, want one kept-fleet mismatch\n%s", uncleared, out)
	}
	if !strings.Contains(out.String(), "holds that service's production deploys") {
		t.Errorf("pass output does not page and hold production deploys:\n%s", out)
	}
	if uncleared[0].RunningKeptInstances != 1 || uncleared[0].RecordedKeptInstances != 2 {
		t.Errorf("mismatch counts = %d and %d, want 1 running and 2 recorded",
			uncleared[0].RunningKeptInstances, uncleared[0].RecordedKeptInstances)
	}
}

func TestPassAcceptsTheRecordedKeptFleet(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	first := shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	closeWindowOver(ctx, t, s.factory, token, svc.ID, first, dir, window.ExitTimedOut)
	rolling := startReleaseKeeping(ctx, t, s.factory, token, svc, env, "c2", 1)
	openWindowOver(ctx, t, s.factory, token, svc.ID, rolling, dir, 3600)
	recordRunning(t, dir, testServiceName, rolling.BuildID)
	recordKept(t, dir, testServiceName, first.BuildID)

	out := &strings.Builder{}
	if err := pass(ctx, s, out, credential, func(dir string) targetseam.Target { return localtarget.New(dir) }); err != nil {
		t.Fatalf("pass: %v", err)
	}
	held, why, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil || held {
		t.Errorf("Mismatch = %v %q, %v; the recorded kept fleet is present", held, why, err)
	}
}

func TestPassAcceptsFewerInstancesWhileMitigationStands(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	first := shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	closeWindowOver(ctx, t, s.factory, token, svc.ID, first, dir, window.ExitTimedOut)
	rolling := startReleaseKeeping(ctx, t, s.factory, token, svc, env, "c2", 2)
	openWindowOver(ctx, t, s.factory, token, svc.ID, rolling, dir, 3600)
	if _, err := deploy.NewWriter(s.factory, token).BeginMitigation(ctx,
		record.Actor{Kind: record.KindHuman, Key: "operator", Basis: record.BasisClaimed}, deploy.Mitigation{
			Operation: deploy.OperationSetInstanceCount, Address: dir, DeployID: rolling.ID, Count: 1,
		}); err != nil {
		t.Fatalf("BeginMitigation: %v", err)
	}
	recordRunning(t, dir, testServiceName, rolling.BuildID)
	recordKept(t, dir, testServiceName, first.BuildID)

	out := &strings.Builder{}
	if err := pass(ctx, s, out, credential, func(dir string) targetseam.Target { return localtarget.New(dir) }); err != nil {
		t.Fatalf("pass: %v", err)
	}
	held, why, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil || held {
		t.Errorf("Mismatch = %v %q, %v; a standing instance-count mitigation is intended state", held, why, err)
	}
}

func recordKept(t *testing.T, dir, svc, build string) {
	t.Helper()
	content := build + " " + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(localtarget.KeptFile(dir, svc), []byte(content), 0o644); err != nil {
		t.Fatalf("recording the kept build for %s in %s: %v", svc, dir, err)
	}
}

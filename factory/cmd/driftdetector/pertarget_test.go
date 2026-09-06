// The first comparison read per target: what the deploy record marks for one
// target and not what it marks for the whole service. fixtures_test.go holds the
// two stores, the environment and the service, and the releases these tests ship.
//
// This file does not skip when its database is unreachable, for the reason
// pass_test.go does not.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// TestARolloutCompleteOnOneTargetAndNotTheNextAgreesOnBoth is the comparison per
// target. A rollout the deployer has completed on the first target and not yet
// reached on the second leaves the two running different builds, and each is
// what the deploy record marks for it: the release under rollout on the first,
// the release below on the second. Read over the whole target set instead, the
// service is "not yet current" on the newer release everywhere, so the target
// that took it correctly disagrees — a mismatch that holds the service's
// production deploys and pages until a human clears it, on a rollout doing
// exactly what it was told.
//
// No window is open here, so nothing is excused: what makes the pass agree is
// the reading and not the rollout exemption.
func TestARolloutCompleteOnOneTargetAndNotTheNextAgreesOnBoth(t *testing.T) {
	ctx, s, token := newStores(t)
	first, second := t.TempDir(), t.TempDir()
	env, svc, credential := setUpOn(ctx, t, s.factory, token, first, second)

	below := shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	rolling := startRelease(ctx, t, s.factory, token, svc, env, "c2")
	if err := deploy.NewWriter(s.factory, token).CompleteTarget(ctx, rolling.ID, first,
		targetseam.ReplacementDrained); err != nil {
		t.Fatalf("completing the first target of the rollout: %v", err)
	}
	recordRunning(t, first, testServiceName, rolling.BuildID)
	recordRunning(t, second, testServiceName, below.BuildID)

	out := &strings.Builder{}
	targetAt := func(dir string) targetseam.Target { return localtarget.New(dir) }
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}

	if strings.Contains(out.String(), "MISMATCH") {
		t.Errorf("a rollout mid-flight raised a mismatch:\n%s", out)
	}
	held, why, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil || held {
		t.Errorf("Mismatch = %v %q, %v; each target runs what the record marks for it", held, why, err)
	}

	// Each target's last check names the release the record marks for that
	// target, which is what makes the two readings visible to a human.
	checks, err := driftdetector.LastChecks(ctx, s.own, svc.ID)
	if err != nil || len(checks) != 2 {
		t.Fatalf("LastChecks = %+v, %v, want one per target", checks, err)
	}
	recorded := map[string]string{}
	for _, c := range checks {
		if !c.Agreed {
			t.Errorf("the check on %s does not agree: running %q recorded %q",
				c.Target, c.RunningBuild, c.RecordedBuildID)
		}
		recorded[c.Target] = c.RecordedBuildID
	}
	if recorded[first] != rolling.BuildID {
		t.Errorf("the check on the completed target records %q, the record marks %q there",
			recorded[first], rolling.BuildID)
	}
	if recorded[second] != below.BuildID {
		t.Errorf("the check on the target the rollout has not reached records %q, the record marks %q there",
			recorded[second], below.BuildID)
	}
}

// TestATargetTheRolloutHasNotReachedRunningTheNewBuildStillDisagrees is the
// other direction of the same reading: per target does not mean excused. A
// target the deploy record marks not reached that is nevertheless running the
// new build is a record disagreeing with what runs, and with no window open
// nothing accounts for it.
func TestATargetTheRolloutHasNotReachedRunningTheNewBuildStillDisagrees(t *testing.T) {
	ctx, s, token := newStores(t)
	first, second := t.TempDir(), t.TempDir()
	env, svc, credential := setUpOn(ctx, t, s.factory, token, first, second)

	shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	rolling := startRelease(ctx, t, s.factory, token, svc, env, "c2")
	if err := deploy.NewWriter(s.factory, token).CompleteTarget(ctx, rolling.ID, first,
		targetseam.ReplacementDrained); err != nil {
		t.Fatalf("completing the first target of the rollout: %v", err)
	}
	recordRunning(t, first, testServiceName, rolling.BuildID)
	recordRunning(t, second, testServiceName, rolling.BuildID)

	out := &strings.Builder{}
	targetAt := func(dir string) targetseam.Target { return localtarget.New(dir) }
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}

	uncleared, err := driftdetector.Uncleared(ctx, s.own, svc.ID)
	if err != nil || len(uncleared) != 1 {
		t.Fatalf("Uncleared = %+v, %v, want the one mismatch on the target the rollout has not reached:\n%s",
			uncleared, err, out)
	}
	if uncleared[0].Target != second {
		t.Errorf("the mismatch names %s, the target the record marks not reached is %s",
			uncleared[0].Target, second)
	}
}

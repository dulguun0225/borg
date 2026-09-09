// The restart: what the deployer's restart does with the records it stopped
// in the middle of — completing or returning each, forward in target order
// where it can rebuild the record's performance, and the one case it cannot
// decide. The two rollbacks are restore_test.go.
package deploy_test

import (
	"context"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// TestTheRestartCompletesOrFailsWhatItCannotCarry: every component's restart
// is a read of its own records, and the deployer's is the deploy records no
// target has finished. A record every target of which is complete is
// completed; a record with something still owed and no [deploy.Rebuilding]
// to carry it forward or back with — a partial deploy and one no target
// reached alike — is marked failed at [deploy.StepCannotBeCarried] rather
// than left standing, which is the third disposition Resume no longer has.
func TestTheRestartCompletesOrFailsWhatItCannotCarry(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)

	begin := func() deploy.Deploy {
		t.Helper()
		d, err := w.Start(ctx, deployer, deploy.Beginning{
			ServiceID: serviceID, EnvironmentID: productionID,
			What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
			IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
		})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		return d
	}

	finished := begin()
	completeOn(t, ctx, w, finished.ID, "/srv/one", "/srv/two")
	partial := begin()
	completeOn(t, ctx, w, partial.ID, "/srv/one")
	stopped := begin()

	if err := deploy.Resume(ctx, w, nil, nil); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	read, err := deploy.Get(ctx, pool, finished.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusComplete {
		t.Errorf("a record every target of which finished is %s, want complete", read.Status)
	}
	if read, err = deploy.Get(ctx, pool, stopped.ID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusFailed || read.FailedStep != deploy.StepCannotBeCarried {
		t.Errorf("a record no target reached and no way to carry it is %s at %q, want failed at StepCannotBeCarried",
			read.Status, read.FailedStep)
	}
	if read, err = deploy.Get(ctx, pool, partial.ID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusFailed || read.FailedStep != deploy.StepCannotBeCarried {
		t.Errorf("the recorded partial deploy with no way to carry it is %s at %q, want failed at StepCannotBeCarried",
			read.Status, read.FailedStep)
	}

	// A second restart reads only the started ones, both now failed and no
	// longer read at all.
	if err := deploy.Resume(ctx, w, nil, nil); err != nil {
		t.Fatalf("a second Resume: %v", err)
	}
	if read, err = deploy.Get(ctx, pool, partial.ID); err != nil || read.Status != deploy.StatusFailed {
		t.Errorf("the partial deploy changed on a second restart: %+v, %v", read, err)
	}
}

// TestARecordNoTargetReachedIsReturnedAcrossEveryTarget: nothing is running
// from a record that reached no target at all, and no kept fleet was
// disturbed, so there is nothing for finishing forward to mean — every
// target the service runs on, read off the live seams [deploy.Rebuilding]
// supplies, is returned to the release that was current before it, which
// completes the record rather than leaving it standing or finishing it
// forward.
func TestARecordNoTargetReachedIsReturnedAcrossEveryTarget(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	below := mintRelease(t, ctx, pool, token, serviceID)
	next := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)

	first := performance(serviceID, below, reaches)
	if _, err := deploy.Perform(ctx, w, first); err != nil {
		t.Fatalf("the first deploy, of the release this one stops above: %v", err)
	}

	stopped, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(next.ID, next.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	rebuild := rebuilding{
		found:       false,
		performance: func(deploy.Deploy) deploy.Performance { return performance(serviceID, below, reaches) },
	}

	if err := deploy.Resume(ctx, w, nil, rebuild); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	read, err := deploy.Get(ctx, pool, stopped.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusComplete {
		t.Errorf("the record no target reached is %s, want complete once it is returned across every target", read.Status)
	}
	targets, err := deploy.Targets(ctx, pool, stopped.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	for _, target := range targets {
		if target.Completion != deploy.CompletionRolledBack {
			t.Errorf("%s reads %s, want rolled back", target.Address, target.Completion)
		}
	}
	for _, fake := range fakes {
		for _, call := range fake.Calls() {
			if call.Op != targetseam.OpShiftTraffic {
				t.Errorf("the return path called %s, want a traffic shift onto what it kept", call.Op)
			}
		}
	}
	current, found, err := deploy.CurrentOnTarget(ctx, pool, serviceID, productionID, "/srv/one")
	if err != nil || !found || current.ReleaseID != below.ID {
		t.Errorf("CurrentOnTarget(/srv/one) = %+v, found %v, %v, want %s returned to", current, found, err, below.ID)
	}
}

// TestResumeFinishesARecordForwardInTargetOrder: the restart's forward
// disposition is [deploy.Resume] itself continuing the walk, over the
// [deploy.Rebuilding] the caller supplies, past the target the record already
// completed and on to the ones it owed — in order, and not deployed to twice.
func TestResumeFinishesARecordForwardInTargetOrder(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := threeFakes()

	begin := make([]deploy.Reaching, len(reaches))
	for n, reach := range reaches {
		begin[n] = deploy.Reaching{Address: reach.Address, ReleaseInstances: reach.ReleaseInstances,
			KeptInstances: reach.KeptInstances}
	}
	started, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: begin,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	completeOn(t, ctx, w, started.ID, "/srv/one")

	toFinish := performance(serviceID, r, reaches)
	rebuild := rebuilding{found: true, performance: func(deploy.Deploy) deploy.Performance { return toFinish }}

	if err := deploy.Resume(ctx, w, nil, rebuild); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	read, err := deploy.Get(ctx, pool, started.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusComplete {
		t.Fatalf("the record is %s, want complete once the two remaining targets are reached", read.Status)
	}
	if len(fakes[0].Calls()) != 0 {
		t.Errorf("the target already complete was reached again: %+v", fakes[0].Calls())
	}
	for n, fake := range fakes[1:] {
		if len(fake.Calls()) == 0 || fake.Calls()[0].Op != targetseam.OpDeploy {
			t.Errorf("target %d, owed before Resume, was not deployed to: %+v", n+2, fake.Calls())
		}
	}
}

// TestResumeReturnsTheReachedTargetWhenItCannotRebuild: where
// [deploy.Rebuilding] cannot rebuild the Performance to finish a stopped
// record forward, Resume takes the package's own return path instead — here
// the fast way, the target it reached keeping the release below standing —
// and the target it never reached is left exactly as it was.
func TestResumeReturnsTheReachedTargetWhenItCannotRebuild(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	below := mintRelease(t, ctx, pool, token, serviceID)
	next := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)

	first := performance(serviceID, below, reaches)
	if _, err := deploy.Perform(ctx, w, first); err != nil {
		t.Fatalf("the first deploy, of the release this one stops above: %v", err)
	}
	calledSoFar := len(fakes[0].Calls())

	stopped, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(next.ID, next.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	completeOn(t, ctx, w, stopped.ID, "/srv/one")

	rebuild := rebuilding{
		found:       false,
		performance: func(deploy.Deploy) deploy.Performance { return performance(serviceID, below, reaches) },
	}

	if err := deploy.Resume(ctx, w, nil, rebuild); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	targets, err := deploy.Targets(ctx, pool, stopped.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if targets[0].Completion != deploy.CompletionRolledBack {
		t.Errorf("the target this record reached reads %s, want rolled back", targets[0].Completion)
	}
	if targets[1].Completion != deploy.CompletionNotReached {
		t.Errorf("the target this record never reached reads %s, want left untouched", targets[1].Completion)
	}
	current, found, err := deploy.CurrentOnTarget(ctx, pool, serviceID, productionID, "/srv/one")
	if err != nil || !found || current.ReleaseID != below.ID {
		t.Errorf("CurrentOnTarget(/srv/one) = %+v, found %v, %v, want %s returned to", current, found, err, below.ID)
	}
	for _, call := range fakes[0].Calls()[calledSoFar:] {
		if call.Op != targetseam.OpShiftTraffic {
			t.Errorf("the return path called %s on the target it reached, want a traffic shift onto what it kept", call.Op)
		}
	}
}

// TestTheOneCaseTheRestartCannotDecide: a record carrying a schema change it
// does not mark complete is marked failed with the previous release left
// current, on the store rule rather than on the deployer knowing how far the
// change got.
func TestTheOneCaseTheRestartCannotDecide(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)

	stopped, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
		SchemaChanges: []string{"0003-drop-the-old-column"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := deploy.Resume(ctx, w, nil, nil); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	read, err := deploy.Get(ctx, pool, stopped.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusFailed || read.FailedStep != deploy.StepSchemaChangeNotComplete {
		t.Errorf("the record is %s at %q, want failed on the store rule", read.Status, read.FailedStep)
	}
	if _, found, err := deploy.Current(ctx, pool, serviceID, productionID, addressesOf(twoTargets)); err != nil || found {
		t.Errorf("Current = found %v, %v, want the previous release left current", found, err)
	}

	// A record whose changes completed is not that case: it reached nothing
	// either, and with no [deploy.Rebuilding] to carry it, that is
	// [deploy.StepCannotBeCarried] rather than the store rule's own step.
	next, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
		SchemaChanges: []string{"0003-drop-the-old-column"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := w.MarkSchemaChangesComplete(ctx, next.ID); err != nil {
		t.Fatalf("MarkSchemaChangesComplete: %v", err)
	}
	if err := deploy.Resume(ctx, w, nil, nil); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	read, err = deploy.Get(ctx, pool, next.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusFailed || read.FailedStep != deploy.StepCannotBeCarried {
		t.Errorf("the record whose changes completed is %s at %q, want failed at StepCannotBeCarried, not the store rule's",
			read.Status, read.FailedStep)
	}
}

// running is what each target answers the restart: the build it is running for
// the service, by address. It is [deploy.Reading] as a test supplies it.
type running map[string]string

func (r running) RunningBuild(_ context.Context, _ deploy.Deploy, address string) (string, error) {
	return r[address], nil
}

// TestTheRestartAsksEachTargetWhichBuildItRuns: the deployer's restart reads
// its unfinished records and asks each of their targets which build it is
// running, and it is that reading that completes a target reached before the
// stop — the row is marked complete from what the target says, so the
// forward disposition [carryOn] then takes skips it rather than deploying to
// it again, and reaches only the target still owed.
func TestTheRestartAsksEachTargetWhichBuildItRuns(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)

	stopped, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// The deployer called the first target and stopped before the row said so.
	if err := w.ReachTarget(ctx, stopped.ID, "/srv/one"); err != nil {
		t.Fatalf("ReachTarget: %v", err)
	}

	toFinish := performance(serviceID, r, reaches)
	rebuild := rebuilding{found: true, performance: func(deploy.Deploy) deploy.Performance { return toFinish }}

	if err := deploy.Resume(ctx, w, running{"/srv/one": r.BuildID, "/srv/two": "bl_the_one_before"}, rebuild); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if len(fakes[0].Calls()) != 0 {
		t.Errorf("the target the reading already found running this build was deployed to: %+v", fakes[0].Calls())
	}
	if len(fakes[1].Calls()) == 0 || fakes[1].Calls()[0].Op != targetseam.OpDeploy {
		t.Errorf("the target still owed was not deployed to: %+v", fakes[1].Calls())
	}
	read, err := deploy.Get(ctx, pool, stopped.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusComplete {
		t.Errorf("the record is %s, want complete", read.Status)
	}
}

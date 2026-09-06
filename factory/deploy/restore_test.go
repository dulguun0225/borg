// The slow rollback and the restart: what the rollback verifies before it puts
// anything back, what it advances as it completes on each target, and what the
// deployer's restart does with the records it stopped in the middle of.
package deploy_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// TestARollbackVerifiesTheArtifactsDigestBeforeItRestoresAnything: redeploying
// by name alone restores a name and not the bytes it was verified under, so a
// digest that differs shifts no traffic, marks the rollback's record failed at
// that step, and pages — production is running a release the factory has just
// failed and nothing the factory has will improve it.
func TestARollbackVerifiesTheArtifactsDigestBeforeItRestoresAnything(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	returnedTo := mintRelease(t, ctx, pool, token, serviceID)
	failed := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)

	paged := &pages{}
	p := performance(serviceID, returnedTo, reaches)
	p.Notifier = paged
	restoration := deploy.Restoration{
		Performance:    p,
		Undoing:        deploy.Undoing{FailedReleaseID: failed.ID, Source: deploy.SourceHealthMonitorAtFailed},
		RecordedDigest: "the digest the build recorded",
		Artifacts:      artifacts{returnedTo.BuildID: "something else entirely"},
	}

	if _, err := deploy.Restore(ctx, w, restoration); !errors.Is(err, deploy.ErrDigestDiffers) {
		t.Fatalf("Restore over an artifact that no longer digests the same = %v, want ErrDigestDiffers", err)
	}
	if len(paged.reasons) != 1 {
		t.Errorf("the deployer paged %d times, want once at that exit: %v", len(paged.reasons), paged.reasons)
	}
	for n, fake := range fakes {
		for _, call := range fake.Calls() {
			if call.Op == targetseam.OpDeploy {
				t.Errorf("target %d was deployed to over an unverified artifact", n+1)
			}
		}
	}
	assertFailedAt(t, ctx, pool, serviceID, deploy.StepArtifactDigest)

	// A rollback that verifies nothing is refused before a record exists at all.
	restoration.Artifacts = nil
	if _, err := deploy.Restore(ctx, w, restoration); !errors.Is(err, deploy.ErrDigestDiffers) {
		t.Errorf("Restore with nothing to verify against = %v, want ErrDigestDiffers", err)
	}
}

// TestARollbackAppliesNoSchemaChange: the schema moves only forward, staying at
// the newest release's form however far traffic moves back. A restoration
// carrying a change is refused before a record exists, rather than resting on
// the caller having left the field empty.
func TestARollbackAppliesNoSchemaChange(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	returnedTo := mintRelease(t, ctx, pool, token, serviceID)
	failed := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)

	p := performance(serviceID, returnedTo, reaches)
	p.SchemaChanges = []targetseam.SchemaChange{{Change: "0003-drop-the-old-column", Text: "drop the old column"}}
	_, err := deploy.Restore(ctx, w, deploy.Restoration{
		Performance:    p,
		Undoing:        deploy.Undoing{FailedReleaseID: failed.ID, Source: deploy.SourceHealthMonitorAtFailed},
		RecordedDigest: "the digest the build recorded",
		Artifacts:      artifacts{returnedTo.BuildID: "the digest the build recorded"},
	})
	if !errors.Is(err, deploy.ErrSchemaChangeAtARollback) {
		t.Fatalf("Restore over a rollback naming a schema change = %v, want ErrSchemaChangeAtARollback", err)
	}
	for n, fake := range fakes {
		for _, call := range fake.Calls() {
			if call.Op == targetseam.OpApplySchemaChange || call.Op == targetseam.OpDeploy {
				t.Errorf("target %d was reached with %s by a refused rollback", n+1, call.Op)
			}
		}
	}
	unfinished, err := deploy.Unfinished(ctx, pool)
	if err != nil {
		t.Fatalf("Unfinished: %v", err)
	}
	if len(unfinished) != 0 {
		t.Errorf("the refusal left %d record(s) behind, want the refusal to come before the record", len(unfinished))
	}
}

// TestARollbackAdvancesTheDeploysItUndoesTargetByTarget: the rolled-back value
// is written target by target as the record of the rollback that undid it
// completes on each, so a rollback that stopped undoes nothing on the record
// beyond the targets it reached.
func TestARollbackAdvancesTheDeploysItUndoesTargetByTarget(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	returnedTo := mintRelease(t, ctx, pool, token, serviceID)
	failed := mintRelease(t, ctx, pool, token, serviceID)

	// The failed release's own deploy, complete on both targets.
	undone, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(failed.ID, failed.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	})
	if err != nil {
		t.Fatalf("starting the deploy the rollback undoes: %v", err)
	}
	completeOn(t, ctx, w, undone.ID, "/srv/one", "/srv/two")
	if err := w.Complete(ctx, undone.ID); err != nil {
		t.Fatalf("completing the deploy the rollback undoes: %v", err)
	}

	reaches, _ := twoFakes(false)
	p := performance(serviceID, returnedTo, reaches)
	p.UndoneDeployIDs = []string{undone.ID}
	rollback, err := deploy.Restore(ctx, w, deploy.Restoration{
		Performance:    p,
		Undoing:        deploy.Undoing{FailedReleaseID: failed.ID, Source: deploy.SourceHealthMonitorAtFailed},
		RecordedDigest: "the digest the build recorded",
		Artifacts:      artifacts{returnedTo.BuildID: "the digest the build recorded"},
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if rollback.Status != deploy.StatusComplete {
		t.Fatalf("the rollback is %s, want complete", rollback.Status)
	}

	targets, err := deploy.Targets(ctx, pool, undone.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	for _, target := range targets {
		if target.Completion != deploy.CompletionRolledBack {
			t.Errorf("target %s of the undone deploy is %s, want rolled back", target.Address, target.Completion)
		}
	}

	read, err := deploy.Get(ctx, pool, rollback.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Undoing.FailedReleaseID != failed.ID || read.Undoing.Source != deploy.SourceHealthMonitorAtFailed {
		t.Errorf("the rollback names %+v, want the release it failed and the source that called for it", read.Undoing)
	}
	current, found, err := deploy.Current(ctx, pool, serviceID, productionID, addressesOf(twoTargets))
	if err != nil || !found || current.ReleaseID != returnedTo.ID {
		t.Errorf("the current release is %+v (found %v, %v), want the release the rollback returned to",
			current.ReleaseID, found, err)
	}
}

// TestTheRestartCompletesFinishesAndReturnsWhatItStoppedInTheMiddleOf: every
// component's restart is a read of its own records, and the deployer's is the
// deploy records no target has finished — one every target of which is complete
// is completed, one no target reached is marked failed at the step that says the
// deployer stopped, and one in between stays started as the recorded partial
// deploy it is.
func TestTheRestartCompletesFinishesAndReturnsWhatItStoppedInTheMiddleOf(t *testing.T) {
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

	carryOn, err := deploy.Resume(ctx, w)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(carryOn) != 1 || carryOn[0].ID != partial.ID {
		t.Fatalf("the restart returned %+v, want the one recorded partial deploy", carryOn)
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
	if read.Status != deploy.StatusFailed || read.FailedStep != deploy.StepStopped {
		t.Errorf("a record no target reached is %s at %q, want failed at the step that says the deployer stopped",
			read.Status, read.FailedStep)
	}
	if read, err = deploy.Get(ctx, pool, partial.ID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusStarted {
		t.Errorf("the recorded partial deploy is %s, want started", read.Status)
	}

	owed, err := deploy.Partial(ctx, pool, partial.ID)
	if err != nil {
		t.Fatalf("Partial: %v", err)
	}
	if len(owed) != 1 || owed[0].Address != "/srv/two" {
		t.Errorf("the targets still owed are %+v, want the one the deployer never reached", owed)
	}

	// A second restart leaves the failed record alone.
	if _, err := deploy.Resume(ctx, w); err != nil {
		t.Fatalf("a second Resume: %v", err)
	}
}

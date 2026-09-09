// The two rollbacks: what the slow one verifies before it puts anything back,
// what each advances as it completes on a target, and what the fast one
// shifts traffic onto. The restart is resume_test.go.
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

// TestTheConfigurationDigestIsStableAndTheFastRollbackCarriesItNamed: the
// configuration digest is over the resolved value set alone — the token
// minted fresh at every deploy is not among it — so an unchanged configuration
// digests the same at the deploy that first ran it and at the slow rollback
// that resolves it again; and the fast rollback mints nothing, carrying the
// digests the deploy record that placed the kept instances already named.
func TestTheConfigurationDigestIsStableAndTheFastRollbackCarriesItNamed(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	below := mintRelease(t, ctx, pool, token, serviceID)
	failed := mintRelease(t, ctx, pool, token, serviceID)
	reaches, _ := twoFakes(true)

	configuration := targetseam.ValueSet{
		Names:  []string{"DATABASE_URL"},
		Values: []string{"postgres://one"},
	}

	first := performance(serviceID, below, reaches)
	first.Configuration = configuration
	firstDeploy, err := deploy.Perform(ctx, w, first)
	if err != nil {
		t.Fatalf("the first deploy: %v", err)
	}

	replacing := performance(serviceID, failed, reaches)
	replacing.Configuration = configuration
	shipped, err := deploy.Perform(ctx, w, replacing)
	if err != nil {
		t.Fatalf("the deploy that replaced it, keeping its instances: %v", err)
	}

	wantDigest := deploy.DigestConfiguration(configuration)
	firstRead, err := deploy.Get(ctx, pool, firstDeploy.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	shippedRead, err := deploy.Get(ctx, pool, shipped.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if firstRead.ConfigurationDigest != wantDigest || shippedRead.ConfigurationDigest != wantDigest {
		t.Fatalf("the digests read %q and %q, want both %q — the same unchanged configuration at both deploys",
			firstRead.ConfigurationDigest, shippedRead.ConfigurationDigest, wantDigest)
	}
	if firstRead.WayInTokenDigest == shippedRead.WayInTokenDigest {
		t.Error("two deploys minted the same way-in token digest, want a fresh one each time")
	}

	// The slow rollback: a deploy of below again, over the same unchanged
	// configuration, digests the same too.
	returning := performance(serviceID, below, reaches)
	returning.Configuration = configuration
	slow, err := deploy.Restore(ctx, w, deploy.Restoration{
		Performance:    returning,
		Undoing:        deploy.Undoing{FailedReleaseID: failed.ID, Source: deploy.SourceHealthMonitorAtFailed},
		RecordedDigest: "the digest the build recorded",
		Artifacts:      artifacts{below.BuildID: "the digest the build recorded"},
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	slowRead, err := deploy.Get(ctx, pool, slow.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if slowRead.ConfigurationDigest != wantDigest {
		t.Errorf("the slow rollback's digest reads %q, want %q — the same unchanged configuration",
			slowRead.ConfigurationDigest, wantDigest)
	}

	// The fast rollback: it mints nothing, and carries the digests named on
	// the deploy record that placed the kept instances.
	fast, err := deploy.ShiftBack(ctx, w, deploy.Returning{
		Performance: performance(serviceID, below, reaches),
		Undoing:     deploy.Undoing{FailedReleaseID: failed.ID, Source: deploy.SourceHealthMonitorAtFailed},
		KeptBy:      shipped.ID,
	})
	if err != nil {
		t.Fatalf("ShiftBack: %v", err)
	}
	fastRead, err := deploy.Get(ctx, pool, fast.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fastRead.ConfigurationDigest != shippedRead.ConfigurationDigest {
		t.Errorf("the fast rollback's configuration digest reads %q, want %q named on %s",
			fastRead.ConfigurationDigest, shippedRead.ConfigurationDigest, shipped.ID)
	}
	if fastRead.WayInTokenDigest != shippedRead.WayInTokenDigest {
		t.Errorf("the fast rollback's way-in token digest reads %q, want %q named on %s",
			fastRead.WayInTokenDigest, shippedRead.WayInTokenDigest, shipped.ID)
	}
}

// TestTheFastRollbackShiftsTrafficOntoTheKeptInstances: a rollback is a deploy
// event and not a version event — where the deploy that replaced a release kept
// its instances, returning to it is a traffic shift onto instances already
// running, with nothing put on a target and no number minted.
func TestTheFastRollbackShiftsTrafficOntoTheKeptInstances(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	below := mintRelease(t, ctx, pool, token, serviceID)
	failed := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(true)
	addresses := addressesOf(twoTargets)

	// The deploy that replaced the release below, keeping its instances.
	replacing := performance(serviceID, failed, reaches)
	shipped, err := deploy.Perform(ctx, w, replacing)
	if err != nil {
		t.Fatalf("the deploy that replaced it: %v", err)
	}

	returning := deploy.Returning{
		Performance: performance(serviceID, below, reaches),
		Undoing: deploy.Undoing{
			FailedReleaseID: failed.ID,
			Source:          deploy.SourceHealthMonitorAtFailed,
		},
		KeptBy: shipped.ID,
	}
	returning.UndoneDeployIDs = []string{shipped.ID}

	rolled, err := deploy.ShiftBack(ctx, w, returning)
	if err != nil {
		t.Fatalf("ShiftBack: %v", err)
	}
	if rolled.Status != deploy.StatusComplete {
		t.Fatalf("the rollback is %s, want complete", rolled.Status)
	}

	for n, fake := range fakes {
		var shifted, deployed int
		for _, call := range fake.Calls() {
			switch call.Op {
			case targetseam.OpShiftTraffic:
				shifted++
				if call.Build != below.BuildID || call.Share != 1 {
					t.Errorf("target %d was shifted onto build %q at %v, want all of it onto the release returned to",
						n+1, call.Build, call.Share)
				}
			case targetseam.OpDeploy:
				deployed++
			}
		}
		if shifted != 1 {
			t.Errorf("target %d took %d shift(s), want the one the rollback is", n+1, shifted)
		}
		if deployed != 1 {
			t.Errorf("target %d was deployed to %d time(s), want the one the rollout did and nothing from the rollback",
				n+1, deployed)
		}
	}

	// The deploy it undoes is rolled back on every target it reached, and the
	// release below is current again.
	undone, err := deploy.Targets(ctx, pool, shipped.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	for _, target := range undone {
		if target.Completion != deploy.CompletionRolledBack {
			t.Errorf("%s of the undone deploy is %s, want rolled back", target.Address, target.Completion)
		}
	}
	current, found, err := deploy.Current(ctx, pool, serviceID, productionID, addresses)
	if err != nil || !found || current.ReleaseID != below.ID {
		t.Errorf("Current = %+v, found %v, %v, want the release the rollback returned to", current, found, err)
	}

	// A target keeping nothing is the slow rollback's, and this refuses before
	// it writes a record.
	nothingKept, _ := twoFakes(true)
	for n := range nothingKept {
		nothingKept[n].KeptInstances = 0
	}
	bare := performance(serviceID, failed, nothingKept)
	cold, err := deploy.Perform(ctx, w, bare)
	if err != nil {
		t.Fatalf("a deploy keeping nothing: %v", err)
	}
	returning.KeptBy = cold.ID
	if _, err := deploy.ShiftBack(ctx, w, returning); !errors.Is(err, deploy.ErrNothingKeptToReturnTo) {
		t.Errorf("a rollback onto a target keeping nothing = %v, want ErrNothingKeptToReturnTo", err)
	}
}

// The step before any traffic moves, as against what the record holds: the
// snapshot before a change that destroys stored data, the changes the store's
// history lacks, the revert's deploy carrying more than one, and the adoption's
// changes written into the history and applied to nothing.
package deploy_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// TestASnapshotThatCannotBeVerifiedStopsTheDeployAndPages: a snapshot the
// deployer cannot take and verify is a deploy not performed — the record is
// marked failed at that step, no target is marked complete, and a page fires.
func TestASnapshotThatCannotBeVerifiedStopsTheDeployAndPages(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, _ := twoFakes(false)

	paged := &pages{}
	p := performance(serviceID, r, reaches)
	p.Notifier = paged
	p.SchemaChanges = []targetseam.SchemaChange{{
		Service: "checkout", Change: "0003-drop-the-old-column", Text: "drop", Destroys: true,
		Credential: credential,
	}}
	// No snapshot name, so there is no copy to take and verify.
	_, err := deploy.Perform(ctx, w, p)
	if !errors.Is(err, deploy.ErrSnapshotRefused) {
		t.Fatalf("Perform = %v, want ErrSnapshotRefused", err)
	}
	if len(paged.reasons) != 1 {
		t.Errorf("the deployer paged %d times, want once at that exit: %v", len(paged.reasons), paged.reasons)
	}
	assertFailedAt(t, ctx, pool, serviceID, deploy.StepSnapshot)
}

// TestASchemaChangeThatFailsToApplyStopsTheDeploy: a change that fails to apply
// stops the deploy before any traffic shifts, no target is marked complete, the
// previous release stays current, and the failure stands on the record for Ops.
func TestASchemaChangeThatFailsToApplyStopsTheDeploy(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, _ := twoFakes(false)

	p := performance(serviceID, r, reaches)
	p.SchemaChanges = []targetseam.SchemaChange{{
		Service: "checkout", Change: "", Text: "add", Credential: credential,
	}}
	_, err := deploy.Perform(ctx, w, p)
	if !errors.Is(err, deploy.ErrSchemaChangeRefused) {
		t.Fatalf("Perform = %v, want ErrSchemaChangeRefused", err)
	}
	assertFailedAt(t, ctx, pool, serviceID, deploy.StepSchemaChange)
}

// TestAChangeTheHistoryAlreadyHoldsIsNotApplied: which changes a store carries
// is read from the schema history the deployer keeps in the store and never
// from a deploy record, so a deploy applies the changes its build declares that
// the history lacks.
func TestAChangeTheHistoryAlreadyHoldsIsNotApplied(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)
	fakes[0].SchemaHistory["checkout"] = []targetseam.SchemaChangeApplied{
		{Change: "0001-add-the-column", Checksum: "a", Widened: true},
	}

	p := performance(serviceID, r, reaches)
	p.SchemaChanges = []targetseam.SchemaChange{
		{Service: "checkout", Change: "0001-add-the-column", Credential: credential},
		{Service: "checkout", Change: "0002-backfill", Credential: credential},
	}

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	applied := 0
	for _, call := range fakes[0].Calls() {
		if call.Op == targetseam.OpApplySchemaChange {
			applied++
			if call.Change != "0002-backfill" {
				t.Errorf("the deploy applied %s, want the change the history lacks", call.Change)
			}
		}
	}
	if applied != 1 {
		t.Errorf("the deploy applied %d changes, want the one the history lacks", applied)
	}
	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.SchemaChangesCompleted || len(read.SchemaChanges) != 2 {
		t.Errorf("the record says %v completed %v, want every change the build carried",
			read.SchemaChanges, read.SchemaChangesCompleted)
	}
}

// TestADeployOwingNothingIsCompletedAndNotLeftLookingLikeAFailure: where the
// store's history already holds every change the build declares, the deploy
// applies none and the record says they completed — a record naming changes and
// saying they did not complete is the record of a change that failed to apply,
// and a reader cannot be left unable to tell the two apart.
func TestADeployOwingNothingIsCompletedAndNotLeftLookingLikeAFailure(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)
	fakes[0].SchemaHistory["checkout"] = []targetseam.SchemaChangeApplied{
		{Release: "rel_earlier", Change: "0001-add-the-column", Checksum: "a", Widened: true},
	}

	p := performance(serviceID, r, reaches)
	p.SchemaChanges = []targetseam.SchemaChange{
		{Service: "checkout", Change: "0001-add-the-column", Credential: credential},
	}

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	for _, call := range fakes[0].Calls() {
		if call.Op == targetseam.OpApplySchemaChange {
			t.Errorf("the deploy applied %s, which the history already holds", call.Change)
		}
	}
	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.SchemaChangesCompleted {
		t.Error("a deploy that owed nothing says its change did not complete")
	}
}

// TestARevertsDeployNamesEveryChangeItApplied: the revert's deploy is the one
// deploy that can carry more than one change — it delivers releases that never
// deployed on their own and applies each of their changes that no deploy applied
// — and a record naming one of several reports a deploy that did less than it
// did.
func TestARevertsDeployNamesEveryChangeItApplied(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)

	p := performance(serviceID, r, reaches)
	p.SchemaChanges = []targetseam.SchemaChange{
		{Service: "checkout", Change: "0001-add-the-column", Credential: credential},
		{Service: "checkout", Change: "0002-backfill", Credential: credential},
		{Service: "checkout", Change: "0003-move-the-reads", Credential: credential},
	}

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	var applied []string
	for _, call := range fakes[0].Calls() {
		if call.Op == targetseam.OpApplySchemaChange {
			applied = append(applied, call.Change)
		}
	}
	if len(applied) != 3 {
		t.Fatalf("the deploy applied %v, want each change in release order", applied)
	}
	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(read.SchemaChanges) != 3 || read.SchemaChanges[0] != "0001-add-the-column" ||
		read.SchemaChanges[2] != "0003-move-the-reads" {
		t.Errorf("the record names %v, want every change it applied in order", read.SchemaChanges)
	}
	history := fakes[0].SchemaHistory["checkout"]
	if len(history) != 3 || history[0].Release != r.ID || history[0].FoundApplied {
		t.Errorf("the history reads %+v, want each row naming the release that shipped it, applied", history)
	}
}

// TestAnAdoptionsChangesAreWrittenIntoTheHistoryAndAppliedToNothing: an adopted
// service's store arrives at the schema its head declares, so the deploy of the
// adoption item's release writes one row per declared change, naming that
// release and marked as found applied, and applies none of them to a live store.
func TestAnAdoptionsChangesAreWrittenIntoTheHistoryAndAppliedToNothing(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)

	p := performance(serviceID, r, reaches)
	p.Adoption = true
	p.SchemaChanges = []targetseam.SchemaChange{
		{Service: "checkout", Change: "0001-create", Credential: credential},
		{Service: "checkout", Change: "0002-add-the-column", Credential: credential},
	}

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	history := fakes[0].SchemaHistory["checkout"]
	if len(history) != 2 {
		t.Fatalf("the history holds %+v, want one row per change the build declares", history)
	}
	for _, row := range history {
		if !row.FoundApplied || row.Release != r.ID {
			t.Errorf("the history row is %+v, want the adoption's release, found applied", row)
		}
	}
	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.SchemaChangesCompleted || len(read.SchemaChanges) != 2 {
		t.Errorf("the record says %v completed %v, want both changes on the record and complete",
			read.SchemaChanges, read.SchemaChangesCompleted)
	}
}

// TestTheDeployerDeletesTheSnapshotAtTheEndOfTheRetention: the copy taken before
// a destructive change is deleted by the deployer on its own pass at the end of
// the retention the service record authors, through the seam, and the deletion
// is written on the record that named it — so the record says where what the
// change destroyed could be read and, after that, that it no longer can.
func TestTheDeployerDeletesTheSnapshotAtTheEndOfTheRetention(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)

	p := performance(serviceID, r, reaches)
	p.SnapshotName = "before-the-drop"
	p.SchemaChanges = []targetseam.SchemaChange{{
		Service: "checkout", Change: "0003-drop-the-old-column", Text: "drop", Destroys: true,
		Credential: credential,
	}}
	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	if len(fakes[0].Snapshots) != 1 {
		t.Fatalf("the target holds %d copy(ies), want the one taken before the change", len(fakes[0].Snapshots))
	}

	pass := deploy.Pass{
		Principal: deployerCalls, ServiceID: serviceID, ServiceName: "checkout",
		Target: fakes[0], Credential: credential,
	}

	// A copy inside the retention is kept, and a service that authored none
	// keeps every copy it has.
	pass.Retention = time.Hour
	if deleted, err := deploy.DeleteExpiredSnapshots(ctx, w, pass); err != nil || len(deleted) != 0 {
		t.Fatalf("the pass deleted %v (%v) inside the retention, want nothing", deleted, err)
	}
	none := pass
	none.Retention = 0
	if deleted, err := deploy.DeleteExpiredSnapshots(ctx, w, none); err != nil || len(deleted) != 0 {
		t.Fatalf("the pass deleted %v (%v) with no retention authored, want nothing", deleted, err)
	}

	pass.Retention = time.Nanosecond
	deleted, err := deploy.DeleteExpiredSnapshots(ctx, w, pass)
	if err != nil {
		t.Fatalf("DeleteExpiredSnapshots: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != d.ID {
		t.Fatalf("the pass deleted %v, want the record naming the copy past its retention", deleted)
	}

	asked := false
	for _, call := range fakes[0].Calls() {
		if call.Op == targetseam.OpDeleteSnapshot && call.Change == "before-the-drop" {
			asked = true
		}
	}
	if !asked {
		t.Errorf("the seam was asked %+v, want the copy deleted through it", fakes[0].Calls())
	}
	if len(fakes[0].Snapshots) != 0 {
		t.Errorf("the target still holds %+v, want the copy gone", fakes[0].Snapshots)
	}
	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Snapshot.Name != "before-the-drop" || read.Snapshot.DeletedAt == "" {
		t.Errorf("the record reads %+v, want the copy it named and when it was deleted", read.Snapshot)
	}

	// A second pass deletes nothing: the deletion is on the record.
	if deleted, err := deploy.DeleteExpiredSnapshots(ctx, w, pass); err != nil || len(deleted) != 0 {
		t.Errorf("the second pass deleted %v (%v), want nothing", deleted, err)
	}
	// And a record naming no copy has nothing to delete.
	bare, err := deploy.Perform(ctx, w, performance(serviceID, r, reaches))
	if err != nil {
		t.Fatalf("a deploy carrying no change: %v", err)
	}
	err = deploy.DeleteSnapshot(ctx, w, deploy.Deleting{
		Principal: deployerCalls, DeployID: bare.ID, ServiceName: "checkout",
		Target: fakes[0], Credential: credential,
	})
	if !errors.Is(err, deploy.ErrNoSnapshot) {
		t.Errorf("deleting the copy of a record naming none = %v, want ErrNoSnapshot", err)
	}
}

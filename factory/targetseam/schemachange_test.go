package targetseam

import (
	"context"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/secretref"
)

// TestTheSchemaHistoryIsReadFromTheTarget: which changes a store carries is
// read from the store's own history and never from the deploy record, so the
// read operation is what answers it.
func TestTheSchemaHistoryIsReadFromTheTarget(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")
	fake := NewFake()
	fake.Instances = 4

	if _, err := fake.Deploy(ctx, deployer, Deployment{
		Service: "checkout", Build: "r-7", Credential: credential, Configuration: deployIDOnly,
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if err := fake.ApplySchemaChange(ctx, deployer, SchemaChange{
		Service: "checkout", Change: "0001-add-the-column", Release: "rel_1", Text: "add",
		Credential: credential,
	}); err != nil {
		t.Fatalf("ApplySchemaChange: %v", err)
	}

	running, err := fake.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if len(running.SchemaHistory) != 1 || running.SchemaHistory[0].Change != "0001-add-the-column" {
		t.Errorf("the history reads %+v, want the one change applied", running.SchemaHistory)
	}
	if !running.SchemaHistory[0].Widened {
		t.Error("a change that destroys nothing reads as not having widened the store")
	}
	if running.Instances != 4 {
		t.Errorf("Instances = %d, want the capacity the platform reports", running.Instances)
	}
}

// TestAChangeThatDestroysNamesTheSnapshotTakenBeforeIt: the copy is required at
// the seam, so a caller that forgot it reaches no store, and a change found
// applied applies nothing and needs none.
func TestAChangeThatDestroysNamesTheSnapshotTakenBeforeIt(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.production")
	fake := NewFake()

	destroying := SchemaChange{
		Service: "checkout", Change: "0004-drop-the-old-column", Release: "rel_9",
		Text: "drop", Destroys: true, Credential: credential,
	}
	if err := fake.ApplySchemaChange(ctx, deployer, destroying); !errors.Is(err, ErrNoSnapshotBeforeIt) {
		t.Fatalf("a destroying change with no copy = %v, want ErrNoSnapshotBeforeIt", err)
	}
	destroying.Snapshot = Snapshot{Name: "before-the-drop", Digest: "0f"}
	if err := fake.ApplySchemaChange(ctx, deployer, destroying); err != nil {
		t.Fatalf("a destroying change naming the copy: %v", err)
	}

	found := SchemaChange{
		Service: "checkout", Change: "0005-drop-another", Release: "rel_9",
		Text: "drop", Destroys: true, FoundApplied: true, Credential: credential,
	}
	if err := fake.ApplySchemaChange(ctx, deployer, found); err != nil {
		t.Fatalf("a destroying change found applied: %v", err)
	}
}

// TestAHistoryRowNamesTheBuildAndTheReleaseWhereThereIsOne: every row names the
// build the change was applied under, and the release as well wherever one
// exists — a candidate's deploy and the search's name a build and no release,
// and their rows stand on it. A change naming neither is refused.
func TestAHistoryRowNamesTheBuildAndTheReleaseWhereThereIsOne(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.production")
	fake := NewFake()

	err := fake.ApplySchemaChange(ctx, deployer, SchemaChange{
		Service: "checkout", Change: "0001-add-the-column", Text: "add", Credential: credential,
	})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("a change naming neither a release nor a build = %v, want ErrIncomplete", err)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("the refused change was recorded: %+v", calls)
	}

	// The search's deploy, and a candidate's: a build and no release.
	if err := fake.ApplySchemaChange(ctx, deployer, SchemaChange{
		Service: "checkout", Change: "0001-add-the-column", Build: "bl_7", Text: "add",
		Credential: credential,
	}); err != nil {
		t.Fatalf("a change under a build and no release: %v", err)
	}
	running, err := fake.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if len(running.SchemaHistory) != 1 {
		t.Fatalf("the history holds %d row(s), want the one written", len(running.SchemaHistory))
	}
	if row := running.SchemaHistory[0]; row.Build != "bl_7" || row.Release != "" {
		t.Errorf("the row is %+v, want the build it was applied under and no release", row)
	}
}

// TestASchemaHistoryRowNamesItsReleaseAndWhetherItWasFoundApplied: which changes
// a store carries is read from the history, and a row says which release shipped
// the change and whether the deployer applied it or took it on the adoption's
// word — an adopted service's store arrives at its head's schema, so its first
// release writes rows and applies nothing.
func TestASchemaHistoryRowNamesItsReleaseAndWhetherItWasFoundApplied(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.production")
	deployer := principal.OfComponent("deployer")
	fake := NewFake()

	if err := fake.ApplySchemaChange(ctx, deployer, SchemaChange{
		Service: "checkout", Change: "0001-create", Release: "rel_1", Text: "create",
		FoundApplied: true, Credential: credential,
	}); err != nil {
		t.Fatalf("the adoption's row: %v", err)
	}
	if err := fake.ApplySchemaChange(ctx, deployer, SchemaChange{
		Service: "checkout", Change: "0002-add-the-column", Release: "rel_2", Build: "bl_2", Text: "add",
		Credential: credential,
	}); err != nil {
		t.Fatalf("ApplySchemaChange: %v", err)
	}

	running, err := fake.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if len(running.SchemaHistory) != 2 {
		t.Fatalf("the history holds %d row(s), want two", len(running.SchemaHistory))
	}
	if got := running.SchemaHistory[0]; got.Release != "rel_1" || !got.FoundApplied {
		t.Errorf("the adoption's row is %+v, want rel_1 found applied", got)
	}
	if got := running.SchemaHistory[1]; got.Release != "rel_2" || got.FoundApplied || got.Build != "bl_2" {
		t.Errorf("the applied row is %+v, want rel_2's build applied by the deployer", got)
	}
}

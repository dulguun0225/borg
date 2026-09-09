// The copy the deployer takes before a change that destroys stored data, and
// deletes at the end of retention or an owner's call: it has to exist and to
// have been verified before its name is worth anything, and the store it was
// taken of is untouched by a deletion. The rest of the store, and the drain
// and refusals beside it, are store_test.go.
package localtarget_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// TestASnapshotCopiesTheStoreAndVerifiesIt: the deploy record names where what
// a destructive change destroyed can still be read, so the copy has to exist
// and to have been verified before the name is worth anything.
func TestASnapshotCopiesTheStoreAndVerifiesIt(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	store := localtarget.DataDir(dir, "checkout")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatalf("making the store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store, "rows"), []byte("what the change destroys"), 0o644); err != nil {
		t.Fatalf("writing the store: %v", err)
	}

	taken, err := local.Snapshot(ctx, deployer, targetseam.SnapshotRequest{
		Service: "checkout", Name: "before-the-drop", Credential: credential,
	})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if taken.Name != "before-the-drop" || len(taken.Digest) != 64 {
		t.Fatalf("Snapshot = %+v, want the name and a digest", taken)
	}
	copied, err := os.ReadFile(filepath.Join(localtarget.SnapshotDir(dir, "checkout", "before-the-drop"), "rows"))
	if err != nil || string(copied) != "what the change destroys" {
		t.Fatalf("the snapshot holds %q, %v, want a copy of the store", copied, err)
	}

	// The same store snapshots to the same digest, and a store that has moved
	// since does not — which is what verifying by digest is.
	again, err := local.Snapshot(ctx, deployer, targetseam.SnapshotRequest{
		Service: "checkout", Name: "second", Credential: credential,
	})
	if err != nil || again.Digest != taken.Digest {
		t.Fatalf("the second snapshot digests %q, %v, want %q", again.Digest, err, taken.Digest)
	}
	if err := os.WriteFile(filepath.Join(store, "rows"), []byte("something else"), 0o644); err != nil {
		t.Fatalf("writing the store: %v", err)
	}
	moved, err := local.Snapshot(ctx, deployer, targetseam.SnapshotRequest{
		Service: "checkout", Name: "third", Credential: credential,
	})
	if err != nil || moved.Digest == taken.Digest {
		t.Fatalf("a snapshot of a moved store digests %q, %v, want a different digest", moved.Digest, err)
	}
}

// TestTheDeployerDeletesTheCopyThroughTheSeam: the deployer deletes a snapshot
// at the end of the service's retention on its own pass and earlier at an
// owner's call, so the target has the operation; a copy that is not there is not
// an error, and the store the copy was taken of is untouched.
func TestTheDeployerDeletesTheCopyThroughTheSeam(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	store := localtarget.DataDir(dir, "checkout")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatalf("making the store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store, "rows"), []byte("what the change destroys"), 0o644); err != nil {
		t.Fatalf("writing the store: %v", err)
	}
	taken, err := local.Snapshot(ctx, deployer, targetseam.SnapshotRequest{
		Service: "checkout", Name: "before-the-drop", Credential: credential,
	})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	copied := localtarget.SnapshotDir(dir, "checkout", taken.Name)
	if _, err := os.Stat(copied); err != nil {
		t.Fatalf("the copy is not there to delete: %v", err)
	}

	deleting := targetseam.SnapshotRequest{
		Service: "checkout", Name: taken.Name, Credential: credential,
	}
	if err := local.DeleteSnapshot(ctx, deployer, deleting); err != nil {
		t.Fatalf("DeleteSnapshot: %v", err)
	}
	if _, err := os.Stat(copied); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the copy reads %v after the deletion, want it gone", err)
	}
	if _, err := os.ReadFile(filepath.Join(store, "rows")); err != nil {
		t.Errorf("the store was touched by the deletion: %v", err)
	}

	// A pass that runs twice deletes once: what the operation promises already
	// holds the second time.
	if err := local.DeleteSnapshot(ctx, deployer, deleting); err != nil {
		t.Errorf("deleting a copy that is already gone = %v, want the promise to hold", err)
	}
	// The boundary is the directory here as everywhere else.
	err = local.DeleteSnapshot(ctx, deployer, targetseam.SnapshotRequest{
		Service: "checkout", Name: filepath.Join("..", "elsewhere"), Credential: credential,
	})
	if !errors.Is(err, localtarget.ErrNameNotLocal) {
		t.Errorf("deleting a copy outside the directory = %v, want ErrNameNotLocal", err)
	}
}

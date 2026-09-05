// These tests are the operations beside the process: the store the service
// keeps, the drain and the cut a replacement reports, and the two operations
// this platform refuses.
package localtarget_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// holderSource is a service that ignores the ask to end and runs on, which is
// what a request held open across a replacement looks like from here. It writes
// the file it is told to emit into once the handler is installed, so a test can
// wait for that rather than for a duration.
const holderSource = `package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	signal.Ignore(syscall.SIGTERM)
	_ = os.WriteFile(os.Getenv("BORG_SIGNAL"), []byte("holding\n"), 0o644)
	time.Sleep(time.Hour)
}
`

// waitForFile waits for a started process to say it is ready, which is a poll
// because a process starting is on no schedule of ours.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s was not written ten seconds after the process started", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// writeScript places a schema script the service ships, which writes a file
// into the store directory it is given.
func writeScript(t *testing.T, dir, service, change, writes string) {
	t.Helper()
	script := "#!/bin/sh\nprintf '" + writes + "' > \"$1\"/" + change + "\n"
	if err := os.WriteFile(localtarget.SchemaScript(dir, service, change), []byte(script), 0o700); err != nil {
		t.Fatalf("writing the schema script: %v", err)
	}
}

// TestADrainThatCannotFinishIsRecordedAsACut: the replacement stops new
// requests reaching the instance and lets the ones it holds finish; a platform
// that cannot hold one open across the replacement performs a cut, and the
// deploy reports which it was.
func TestADrainThatCannotFinishIsRecordedAsACut(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	local.DrainWait = 100 * time.Millisecond
	buildProgram(t, dir, "rel_one", holderSource)
	buildProgram(t, dir, "rel_two", sleeperSource)

	if _, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_one", Credential: credential,
	}); err != nil {
		t.Fatalf("Deploy rel_one: %v", err)
	}
	waitForFile(t, localtarget.SignalFile(dir, "rel_one"))

	placed, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_two", Credential: credential,
	})
	if err != nil {
		t.Fatalf("Deploy rel_two: %v", err)
	}
	if placed.Replacement != targetseam.ReplacementCut {
		t.Errorf("replacing an instance that would not end reports %q, want a cut", placed.Replacement)
	}

	running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Build != "rel_two" {
		t.Errorf("ReadRunning names %q, want rel_two", running.Build)
	}
}

// TestReadRunningReportsTheDigestAndTheCapacity: a rollback verifies the
// artifact's digest before it deploys, and a kept-instance count is computed
// from the capacity the platform reports, so the read operation answers both.
func TestReadRunningReportsTheDigestAndTheCapacity(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_one", sleeperSource)

	if _, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_one", Credential: credential,
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if len(running.ArtifactDigest) != 64 {
		t.Errorf("ArtifactDigest = %q, want the sha256 of the artifact", running.ArtifactDigest)
	}
	if running.Instances != 1 {
		t.Errorf("Instances = %d, want the one instance this platform runs", running.Instances)
	}
}

// TestASchemaChangeRunsTheServicesScriptAndIsInTheHistory: which changes a
// store carries is read from the history the deployer keeps in the store, so a
// change that ran is in it and a change with no script is applied by nothing.
func TestASchemaChangeRunsTheServicesScriptAndIsInTheHistory(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	writeScript(t, dir, "checkout", "0001-add-the-column", "added")

	if err := local.ApplySchemaChange(ctx, deployer, targetseam.SchemaChange{
		Service: "checkout", Change: "0001-add-the-column", Credential: credential,
	}); err != nil {
		t.Fatalf("ApplySchemaChange: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(localtarget.DataDir(dir, "checkout"), "0001-add-the-column"))
	if err != nil || string(written) != "added" {
		t.Fatalf("the script wrote %q, %v, want it to have run against the store", written, err)
	}

	running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if len(running.SchemaHistory) != 1 || running.SchemaHistory[0].Change != "0001-add-the-column" {
		t.Fatalf("the history reads %+v, want the change applied", running.SchemaHistory)
	}
	if !running.SchemaHistory[0].Widened || running.SchemaHistory[0].Checksum == "" {
		t.Errorf("the history row is %+v, want a checksum and a widening", running.SchemaHistory[0])
	}

	err = local.ApplySchemaChange(ctx, deployer, targetseam.SchemaChange{
		Service: "checkout", Change: "0002-nobody-shipped-this", Credential: credential,
	})
	if !errors.Is(err, localtarget.ErrNoSchemaScript) {
		t.Errorf("ApplySchemaChange with no script = %v, want ErrNoSchemaScript", err)
	}
	after, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if len(after.SchemaHistory) != 1 {
		t.Errorf("the history reads %+v after a change nothing applied, want the one change", after.SchemaHistory)
	}
}

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

// TestThePlatformRefusesWhatItCannotDo: this platform moves a process rather
// than traffic, so it serves no share, and a shift reported as performed would
// be a rollout recorded as having compared two builds while one served nothing.
func TestThePlatformRefusesWhatItCannotDo(t *testing.T) {
	ctx := t.Context()
	local, _ := newTarget(t, "checkout")

	err := local.ShiftTraffic(ctx, deployer, targetseam.Shift{
		Service: "checkout", Build: "rel_one", Share: 0.1, Credential: credential,
	})
	if !errors.Is(err, localtarget.ErrNoShare) {
		t.Errorf("ShiftTraffic = %v, want ErrNoShare", err)
	}
	if err != nil && !strings.Contains(err.Error(), "process") {
		t.Errorf("the refusal reads %q, want it to say what the platform does instead", err)
	}

	err = local.SetInstanceCount(ctx, deployer, targetseam.InstanceCount{
		Service: "checkout", Build: "rel_one", Count: 3, Credential: credential,
	})
	if !errors.Is(err, localtarget.ErrOneInstance) {
		t.Errorf("SetInstanceCount(3) = %v, want ErrOneInstance", err)
	}
	if err := local.SetInstanceCount(ctx, deployer, targetseam.InstanceCount{
		Service: "checkout", Build: "rel_one", Count: 1, Credential: credential,
	}); err != nil {
		t.Errorf("SetInstanceCount(1) = %v, want the count this platform already runs", err)
	}
}

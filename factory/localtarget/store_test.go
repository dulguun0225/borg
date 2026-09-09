// These tests are the operations beside the process: the store the service
// keeps, the drain and the cut a replacement reports, and the two operations
// this platform refuses.
// The copy taken and verified, and deleted through the seam, is
// snapshot_test.go.
package localtarget_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// holderSource is a service holding a request open across a replacement: asked
// to end, it stops taking new work and takes [heldFor] to finish what it holds,
// then writes the file its configuration names and exits. It writes the file it
// is told to emit into once the handler is installed, so a test can wait for
// that rather than for a duration.
//
// It holds for longer than any bound this package used to put on a drain, which
// is what makes the finished file the proof that the replacement waited rather
// than ended it.
const holderSource = `package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ending := make(chan os.Signal, 1)
	signal.Notify(ending, syscall.SIGTERM)
	_ = os.WriteFile(os.Getenv("BORG_SIGNAL"), []byte("holding\n"), 0o644)
	<-ending
	time.Sleep(2500 * time.Millisecond)
	_ = os.WriteFile(os.Getenv("HOLDER_FINISHED"), []byte("finished\n"), 0o644)
}
`

// heldFor is how long holderSource takes to finish what it holds after it is
// asked to end.
const heldFor = 2500 * time.Millisecond

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

// TestAReplacementWaitsForTheRequestsItHolds: neither rollout row drops a
// request, so the replacement asks the instance to end and waits for it to
// finish what it holds however long that takes, and reports the drain. The
// finished file is the proof: the instance writes it after it has held for
// longer than any bound this package once put on the wait, so a replacement
// that ended it instead leaves the file absent.
func TestAReplacementWaitsForTheRequestsItHolds(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_one", holderSource)
	buildProgram(t, dir, "rel_two", sleeperSource)
	finished := filepath.Join(dir, "held.finished")

	if _, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_one", Credential: credential, DeployID: "dep_1",
		Configuration: targetseam.ValueSet{
			Names: []string{"HOLDER_FINISHED"}, Values: []string{finished},
		},
	}); err != nil {
		t.Fatalf("Deploy rel_one: %v", err)
	}
	waitForFile(t, localtarget.SignalFile(dir, "rel_one"))

	began := time.Now()
	placed, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_two", Credential: credential, DeployID: "dep_2",
	})
	if err != nil {
		t.Fatalf("Deploy rel_two: %v", err)
	}
	if placed.Replacement != targetseam.ReplacementDrained {
		t.Errorf("the replacement reports %q, want the drain", placed.Replacement)
	}
	if waited := time.Since(began); waited < heldFor {
		t.Errorf("the replacement returned after %v, want at least the %v the instance held", waited, heldFor)
	}
	if _, err := os.Stat(finished); err != nil {
		t.Errorf("the instance replaced never finished what it held: %v", err)
	}

	running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Build != "rel_two" {
		t.Errorf("ReadRunning names %q, want rel_two", running.Build)
	}
}

// TestAHeldRequestTheCallerWillNotWaitForIsAnError: the wait ends where the
// caller cancels and nowhere else — nothing here ends an instance that is still
// finishing what it holds, so what a caller unwilling to wait gets is the
// cancellation and never a drain it can write on a record.
func TestAHeldRequestTheCallerWillNotWaitForIsAnError(t *testing.T) {
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_one", holderSource)
	buildProgram(t, dir, "rel_two", sleeperSource)
	finished := filepath.Join(dir, "held.finished")

	if _, err := local.Deploy(t.Context(), deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_one", Credential: credential, DeployID: "dep_1",
		Configuration: targetseam.ValueSet{
			Names: []string{"HOLDER_FINISHED"}, Values: []string{finished},
		},
	}); err != nil {
		t.Fatalf("Deploy rel_one: %v", err)
	}
	waitForFile(t, localtarget.SignalFile(dir, "rel_one"))

	ctx, giveUp := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer giveUp()
	if _, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_two", Credential: credential, DeployID: "dep_2",
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a replacement the caller would not wait for = %v, want the cancellation", err)
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
		Service: "checkout", Build: "rel_one", Credential: credential, DeployID: "dep_1",
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "rel_one"))
	if err != nil {
		t.Fatalf("reading the deployed binary back: %v", err)
	}
	sum := sha256.Sum256(content)
	want := "sha256:" + hex.EncodeToString(sum[:])
	if running.ArtifactDigest != want {
		t.Errorf("ArtifactDigest = %q, want %q, the sha256 of the deployed binary", running.ArtifactDigest, want)
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
		Service: "checkout", Change: "0001-add-the-column", Release: "rel_4", Build: "bl_4",
		Credential: credential,
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
	if running.SchemaHistory[0].Release != "rel_4" || running.SchemaHistory[0].FoundApplied {
		t.Errorf("the history row is %+v, want the release that shipped it and applied by the deployer",
			running.SchemaHistory[0])
	}
	if running.SchemaHistory[0].Build != "bl_4" {
		t.Errorf("the history row is %+v, want the build the change was applied under",
			running.SchemaHistory[0])
	}

	err = local.ApplySchemaChange(ctx, deployer, targetseam.SchemaChange{
		Service: "checkout", Change: "0002-nobody-shipped-this", Release: "rel_5", Build: "bl_5",
		Credential: credential,
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

// TestAnAdoptedStoresChangesAreWrittenIntoTheHistoryAndAppliedToNothing: an
// adopted service arrives with its store at the schema its head declares, so the
// deploy of the adoption item's release writes one row per change the build
// declares, naming that release and marked as found applied, and applies none.
func TestAnAdoptedStoresChangesAreWrittenIntoTheHistoryAndAppliedToNothing(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	writeScript(t, dir, "checkout", "0001-create-the-table", "created")

	if err := local.ApplySchemaChange(ctx, deployer, targetseam.SchemaChange{
		Service: "checkout", Change: "0001-create-the-table", Release: "rel_1", Build: "bl_1",
		FoundApplied: true, Credential: credential,
	}); err != nil {
		t.Fatalf("ApplySchemaChange found applied: %v", err)
	}

	if _, err := os.ReadFile(filepath.Join(localtarget.DataDir(dir, "checkout"), "0001-create-the-table")); err == nil {
		t.Error("the script ran against the store, and a change found applied is applied to nothing")
	}

	running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if len(running.SchemaHistory) != 1 {
		t.Fatalf("the history reads %+v, want the one row the adoption wrote", running.SchemaHistory)
	}
	row := running.SchemaHistory[0]
	if row.Change != "0001-create-the-table" || row.Release != "rel_1" || !row.FoundApplied || row.Checksum == "" {
		t.Errorf("the history row is %+v, want rel_1's change found applied with a checksum", row)
	}
}

// TestTheHistoryLivesInTheStoreSoASnapshotCarriesIt: the history is not a record
// of the graph — it lives where the schema does, which here is the store
// directory, so the copy taken before a destructive change holds the history the
// store had when it was taken.
func TestTheHistoryLivesInTheStoreSoASnapshotCarriesIt(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	writeScript(t, dir, "checkout", "0001-add-the-column", "added")

	if err := local.ApplySchemaChange(ctx, deployer, targetseam.SchemaChange{
		Service: "checkout", Change: "0001-add-the-column", Release: "rel_4", Build: "bl_4",
		Credential: credential,
	}); err != nil {
		t.Fatalf("ApplySchemaChange: %v", err)
	}
	store := localtarget.DataDir(dir, "checkout")
	if history := localtarget.HistoryFile(dir, "checkout"); !strings.HasPrefix(history, store+string(filepath.Separator)) {
		t.Fatalf("the history is at %s, want it inside the store at %s", history, store)
	}

	taken, err := local.Snapshot(ctx, deployer, targetseam.SnapshotRequest{
		Service: "checkout", Name: "before-the-drop", Credential: credential,
	})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	copied := filepath.Join(localtarget.SnapshotDir(dir, "checkout", taken.Name), "schema-history")
	held, err := os.ReadFile(copied)
	if err != nil || !strings.Contains(string(held), "0001-add-the-column") {
		t.Fatalf("the snapshot holds %q, %v, want the history the store carried", held, err)
	}
}

// TestAChangeThatDestroysIsAppliedOnlyAfterItsSnapshotVerifies: the copy the
// change names is read here before anything is applied, so a name pointing at
// nothing, or at a copy that has moved since, leaves the store as it was.
func TestAChangeThatDestroysIsAppliedOnlyAfterItsSnapshotVerifies(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	writeScript(t, dir, "checkout", "0003-drop-the-column", "dropped")
	store := localtarget.DataDir(dir, "checkout")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatalf("making the store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store, "rows"), []byte("what the change destroys"), 0o644); err != nil {
		t.Fatalf("writing the store: %v", err)
	}

	dropping := targetseam.SchemaChange{
		Service: "checkout", Change: "0003-drop-the-column", Release: "rel_9", Build: "bl_9",
		Destroys: true, Credential: credential,
	}
	dropping.Snapshot = targetseam.Snapshot{Name: "never-taken", Digest: "00"}
	if err := local.ApplySchemaChange(ctx, deployer, dropping); !errors.Is(err, localtarget.ErrSnapshotGone) {
		t.Fatalf("a change naming a copy nothing took = %v, want ErrSnapshotGone", err)
	}

	taken, err := local.Snapshot(ctx, deployer, targetseam.SnapshotRequest{
		Service: "checkout", Name: "before-the-drop", Credential: credential,
	})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	moved := taken
	moved.Digest = strings.Repeat("0", len(taken.Digest))
	dropping.Snapshot = moved
	if err := local.ApplySchemaChange(ctx, deployer, dropping); !errors.Is(err, localtarget.ErrSnapshotUnverified) {
		t.Fatalf("a change naming a copy that digests otherwise = %v, want ErrSnapshotUnverified", err)
	}
	if _, err := os.Stat(filepath.Join(store, "0003-drop-the-column")); err == nil {
		t.Fatal("the change was applied without a verified copy of what it destroys")
	}

	dropping.Snapshot = taken
	if err := local.ApplySchemaChange(ctx, deployer, dropping); err != nil {
		t.Fatalf("a change naming the copy taken before it: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "0003-drop-the-column")); err != nil {
		t.Fatalf("the change named a verified copy and did not run: %v", err)
	}
}

// TestARowADeployNamingNoReleaseWritesStandsOnTheBuild: a candidate's deploy and
// the search's name a build and no release, and the history row they write names
// that build — a line is read by its fields, so the release's field carries the
// placeholder rather than nothing.
func TestARowADeployNamingNoReleaseWritesStandsOnTheBuild(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	writeScript(t, dir, "checkout", "0001-add-the-column", "added")

	if err := local.ApplySchemaChange(ctx, deployer, targetseam.SchemaChange{
		Service: "checkout", Change: "0001-add-the-column", Build: "bl_a_commit_on_no_branch",
		Credential: credential,
	}); err != nil {
		t.Fatalf("ApplySchemaChange under a build and no release: %v", err)
	}

	running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if len(running.SchemaHistory) != 1 {
		t.Fatalf("the history reads %+v, want the one change applied", running.SchemaHistory)
	}
	row := running.SchemaHistory[0]
	if row.Build != "bl_a_commit_on_no_branch" || row.Release != "" {
		t.Errorf("the row is %+v, want the build it was applied under and no release", row)
	}

	written, err := os.ReadFile(localtarget.HistoryFile(dir, "checkout"))
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	if fields := strings.Fields(string(written)); len(fields) != 6 {
		t.Errorf("the line reads %q, want six fields", written)
	}
}

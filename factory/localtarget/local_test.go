// These tests are where a real process runs: they build a tiny Go binary that
// sleeps, deploy it through the seam, and read it back running. Every process
// a test starts is killed in cleanup, so a failing test leaves nothing
// behind.
package localtarget_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// sleeperSource is a service that runs until killed.
const sleeperSource = `package main

import "time"

func main() { time.Sleep(time.Hour) }
`

// exiterSource is a service that dies on its own, immediately.
const exiterSource = `package main

func main() {}
`

// buildProgram compiles source into dir/name, which is where [localtarget.New]'s
// Deploy expects the binary for release "name".
func buildProgram(t *testing.T, dir, name, source string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(src, []byte(source), 0o600); err != nil {
		t.Fatalf("writing the program's source: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, name), src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", name, err, out)
	}
}

// newTarget returns a target over its own directory, with a cleanup that
// stops whatever the test left running for the named services.
func newTarget(t *testing.T, services ...string) (*localtarget.Local, string) {
	t.Helper()
	dir := t.TempDir()
	local := localtarget.New(dir)
	t.Cleanup(func() {
		for _, service := range services {
			if err := local.Stop(context.Background(), service, credential); err != nil {
				t.Errorf("stopping service %q in cleanup: %v", service, err)
			}
		}
	})
	return local, dir
}

var credential = secretref.MustNew("target.local")

func TestDeployRunsAndStopKills(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_one", sleeperSource)

	err := local.Deploy(ctx, targetseam.Deployment{Service: "checkout", Release: "rel_one", Credential: credential})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	running, err := local.ReadRunning(ctx, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Release != "rel_one" {
		t.Fatalf("ReadRunning names %q, want rel_one", running.Release)
	}

	if err := local.Stop(ctx, "checkout", credential); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	running, err = local.ReadRunning(ctx, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Release != "" {
		t.Fatalf("ReadRunning names %q after Stop, want nothing running", running.Release)
	}

	// Stopping a service with nothing running is not an error: what Stop
	// promises already holds.
	if err := local.Stop(ctx, "checkout", credential); err != nil {
		t.Errorf("Stop with nothing running: %v", err)
	}
}

func TestASecondDeployReplacesTheFirst(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_one", sleeperSource)
	buildProgram(t, dir, "rel_two", sleeperSource)

	if err := local.Deploy(ctx, targetseam.Deployment{Service: "checkout", Release: "rel_one", Credential: credential}); err != nil {
		t.Fatalf("Deploy rel_one: %v", err)
	}
	if err := local.Deploy(ctx, targetseam.Deployment{Service: "checkout", Release: "rel_two", Credential: credential}); err != nil {
		t.Fatalf("Deploy rel_two: %v", err)
	}

	running, err := local.ReadRunning(ctx, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Release != "rel_two" {
		t.Fatalf("ReadRunning names %q, want the replacement, rel_two", running.Release)
	}
}

// TestADeadProcessReadsAsNothingRunning deploys a binary that exits on its
// own and waits for the target to say so. The wait is a poll because the exit
// is the process's own act, on no schedule of ours.
func TestADeadProcessReadsAsNothingRunning(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_dies", exiterSource)

	if err := local.Deploy(ctx, targetseam.Deployment{Service: "checkout", Release: "rel_dies", Credential: credential}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		running, err := local.ReadRunning(ctx, "checkout", credential)
		if err != nil {
			t.Fatalf("ReadRunning: %v", err)
		}
		if running.Release == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ReadRunning still names %q ten seconds after the process exited", running.Release)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAReleaseOutsideTheDirectoryIsRefused deploys a release string that
// traverses out of the targets directory. The target joins the release onto its
// directory, so without the check it would run whatever the traversal names.
// The refusal comes before the stop, so what was running is still running.
func TestAReleaseOutsideTheDirectoryIsRefused(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_one", sleeperSource)

	// A binary outside dir, at the path the traversal would reach.
	outside := filepath.Dir(dir)
	buildProgram(t, outside, "planted", sleeperSource)

	if err := local.Deploy(ctx, targetseam.Deployment{Service: "checkout", Release: "rel_one", Credential: credential}); err != nil {
		t.Fatalf("Deploy rel_one: %v", err)
	}

	for _, release := range []string{
		filepath.Join("..", "planted"),
		filepath.Join(outside, "planted"),
		"/bin/sh",
		"",
	} {
		err := local.Deploy(ctx, targetseam.Deployment{Service: "checkout", Release: release, Credential: credential})
		if release == "" {
			// An empty release is the seam's own refusal, before this one.
			if !errors.Is(err, targetseam.ErrIncomplete) {
				t.Errorf("Deploy of %q = %v, want %v", release, err, targetseam.ErrIncomplete)
			}
			continue
		}
		if !errors.Is(err, localtarget.ErrReleaseNotLocal) {
			t.Errorf("Deploy of %q = %v, want %v", release, err, localtarget.ErrReleaseNotLocal)
		}
	}

	running, err := local.ReadRunning(ctx, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Release != "rel_one" {
		t.Errorf("ReadRunning names %q after the refusals, want rel_one still running", running.Release)
	}
}

func TestTheSeamsChecksHold(t *testing.T) {
	ctx := t.Context()
	local, _ := newTarget(t, "checkout")

	err := local.Deploy(ctx, targetseam.Deployment{Service: "checkout", Release: "rel_one"})
	if !errors.Is(err, targetseam.ErrIncomplete) {
		t.Errorf("Deploy with no credential = %v, want %v", err, targetseam.ErrIncomplete)
	}
	err = local.Deploy(ctx, targetseam.Deployment{Service: "checkout", Credential: credential})
	if !errors.Is(err, targetseam.ErrIncomplete) {
		t.Errorf("Deploy with no release = %v, want %v", err, targetseam.ErrIncomplete)
	}
	if err := local.Stop(ctx, "", credential); !errors.Is(err, targetseam.ErrIncomplete) {
		t.Errorf("Stop with no service = %v, want %v", err, targetseam.ErrIncomplete)
	}
	if _, err := local.ReadRunning(ctx, "checkout", secretref.Ref{}); !errors.Is(err, targetseam.ErrIncomplete) {
		t.Errorf("ReadRunning with no credential = %v, want %v", err, targetseam.ErrIncomplete)
	}

	// A release whose binary was never placed in dir fails at the start, and
	// nothing runs for the service afterwards.
	err = local.Deploy(ctx, targetseam.Deployment{Service: "checkout", Release: "rel_missing", Credential: credential})
	if err == nil {
		t.Error("Deploy of a release with no binary succeeded")
	}
	running, err := local.ReadRunning(ctx, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Release != "" {
		t.Errorf("ReadRunning names %q after a failed deploy, want nothing", running.Release)
	}
}

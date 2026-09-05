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
	"github.com/dulguun0225/borg/factory/principal"
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
// Deploy expects the binary for build "name".
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
			if err := local.Stop(context.Background(), deployer, service, credential); err != nil {
				t.Errorf("stopping service %q in cleanup: %v", service, err)
			}
		}
	})
	return local, dir
}

var credential = secretref.MustNew("target.local")

// deployer is the principal every call here is made as. No agent reaches a
// deploy target, so the one caller of this seam is the deployer, calling as
// itself.
var deployer = principal.OfComponent("deployer")

func TestDeployRunsAndStopKills(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_one", sleeperSource)

	placed, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_one", Credential: credential,
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if placed.Replacement != targetseam.ReplacementDrained {
		t.Errorf("Deploy onto an empty target reports %q, want a drain", placed.Replacement)
	}
	running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Build != "rel_one" {
		t.Fatalf("ReadRunning names %q, want rel_one", running.Build)
	}

	if err := local.Stop(ctx, deployer, "checkout", credential); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	running, err = local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Build != "" {
		t.Fatalf("ReadRunning names %q after Stop, want nothing running", running.Build)
	}

	// Stopping a service with nothing running is not an error: what Stop
	// promises already holds.
	if err := local.Stop(ctx, deployer, "checkout", credential); err != nil {
		t.Errorf("Stop with nothing running: %v", err)
	}
}

func TestASecondDeployReplacesTheFirst(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_one", sleeperSource)
	buildProgram(t, dir, "rel_two", sleeperSource)

	if _, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_one", Credential: credential,
	}); err != nil {
		t.Fatalf("Deploy rel_one: %v", err)
	}
	if _, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_two", Credential: credential,
	}); err != nil {
		t.Fatalf("Deploy rel_two: %v", err)
	}

	running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Build != "rel_two" {
		t.Fatalf("ReadRunning names %q, want the replacement, rel_two", running.Build)
	}
}

// TestADeadProcessReadsAsNothingRunning deploys a binary that exits on its
// own and waits for the target to say so. The wait is a poll because the exit
// is the process's own act, on no schedule of ours.
func TestADeadProcessReadsAsNothingRunning(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_dies", exiterSource)

	if _, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_dies", Credential: credential,
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
		if err != nil {
			t.Fatalf("ReadRunning: %v", err)
		}
		if running.Build == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ReadRunning still names %q ten seconds after the process exited", running.Build)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAReleaseOutsideTheDirectoryIsRefused deploys a build string that
// traverses out of the targets directory. The target joins the build onto its
// directory, so without the check it would run whatever the traversal names.
// The refusal comes before the stop, so what was running is still running.
func TestAReleaseOutsideTheDirectoryIsRefused(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_one", sleeperSource)

	// A binary outside dir, at the path the traversal would reach.
	outside := filepath.Dir(dir)
	buildProgram(t, outside, "planted", sleeperSource)

	if _, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_one", Credential: credential,
	}); err != nil {
		t.Fatalf("Deploy rel_one: %v", err)
	}

	for _, build := range []string{
		filepath.Join("..", "planted"),
		filepath.Join(outside, "planted"),
		"/bin/sh",
		"",
	} {
		_, err := local.Deploy(ctx, deployer, targetseam.Deployment{
			Service: "checkout", Build: build, Credential: credential,
		})
		if build == "" {
			// An empty build is the seam's own refusal, before this one.
			if !errors.Is(err, targetseam.ErrIncomplete) {
				t.Errorf("Deploy of %q = %v, want %v", build, err, targetseam.ErrIncomplete)
			}
			continue
		}
		if !errors.Is(err, localtarget.ErrBuildNotLocal) {
			t.Errorf("Deploy of %q = %v, want %v", build, err, localtarget.ErrBuildNotLocal)
		}
	}

	running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Build != "rel_one" {
		t.Errorf("ReadRunning names %q after the refusals, want rel_one still running", running.Build)
	}
}

func TestTheSeamsChecksHold(t *testing.T) {
	ctx := t.Context()
	local, _ := newTarget(t, "checkout")

	_, err := local.Deploy(ctx, deployer, targetseam.Deployment{Service: "checkout", Build: "rel_one"})
	if !errors.Is(err, targetseam.ErrIncomplete) {
		t.Errorf("Deploy with no credential = %v, want %v", err, targetseam.ErrIncomplete)
	}
	_, err = local.Deploy(ctx, deployer, targetseam.Deployment{Service: "checkout", Credential: credential})
	if !errors.Is(err, targetseam.ErrIncomplete) {
		t.Errorf("Deploy with no build = %v, want %v", err, targetseam.ErrIncomplete)
	}
	_, err = local.Deploy(ctx, principal.Principal{}, targetseam.Deployment{
		Service: "checkout", Build: "rel_one", Credential: credential,
	})
	if !errors.Is(err, targetseam.ErrNoPrincipal) {
		t.Errorf("Deploy with no principal = %v, want %v", err, targetseam.ErrNoPrincipal)
	}
	if err := local.Stop(ctx, deployer, "", credential); !errors.Is(err, targetseam.ErrIncomplete) {
		t.Errorf("Stop with no service = %v, want %v", err, targetseam.ErrIncomplete)
	}
	if _, err := local.ReadRunning(ctx, deployer, "checkout", secretref.Ref{}); !errors.Is(err, targetseam.ErrIncomplete) {
		t.Errorf("ReadRunning with no credential = %v, want %v", err, targetseam.ErrIncomplete)
	}

	// A build whose binary was never placed in dir fails at the start, and
	// nothing runs for the service afterwards.
	_, err = local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_missing", Credential: credential,
	})
	if err == nil {
		t.Error("Deploy of a build with no binary succeeded")
	}
	running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Build != "" {
		t.Errorf("ReadRunning names %q after a failed deploy, want nothing", running.Build)
	}
}

package localtarget

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// Local is a [targetseam.Target] that runs each service's build as one local
// process, in one directory. There is a target per environment rather than per
// install: an environment names the addresses a deploy into it is performed
// against, and for this substrate an address is a directory — so a candidate
// environment gets a [Local] of its own and two candidates of one service run side
// by side without either reading the other's.
//
// What is running is on disk and not in this value, which is what lets a second
// process read it. doc.go says why that had to change.
type Local struct {
	dir string
}

var _ targetseam.Target = (*Local)(nil)

// SignalEnv is the environment variable each started process is told the file to
// emit its quantity into. The comparison reads that file, so the name is here — one
// place, named by the substrate that wires it, rather than agreed between the target
// and whatever reads it.
const SignalEnv = "BORG_SIGNAL"

// SignalFile is where the build running in dir emits its quantity. One file per
// build, so a release's own counts are told apart from the counts of the build that
// ran there before it — which is what the comparison's baseline is.
func SignalFile(dir, build string) string { return filepath.Join(dir, build+".signal") }

// RunningFile is where the target records what it started for one service: the
// build, a space, and the process id. It is a file rather than a field, so that a
// process which did not start the software can still read what is running there —
// which is exactly what the reconciler is, and what the seam's read operation is
// for.
func RunningFile(dir, service string) string { return filepath.Join(dir, service+".running") }

var (
	// ErrBuildNotLocal is returned by [Local.Deploy] for a build that is not a
	// local path — one with a parent traversal in it, an absolute one, or a root.
	// The build string reaches this from the store, and a target that joins
	// whatever it is handed runs whatever that names: "../../usr/bin/whatever"
	// under dir is a program outside the targets directory. What the check
	// confines is the path this package builds and nothing else; the credential
	// still reaches nothing, and the binary at a local path is trusted to be what
	// the build put there.
	ErrBuildNotLocal = errors.New("localtarget: the build is not a local path")
	// ErrServiceNotLocal is returned for a service name that is not a local path
	// element, for the same reason and about the same join: the name is part of the
	// file this target records what is running in.
	ErrServiceNotLocal = errors.New("localtarget: the service name is not a local path")
)

// New returns a target over dir, where the deployable binary for a build is
// placed before Deploy is called, named exactly by the build string.
func New(dir string) *Local { return &Local{dir: dir} }

// Deploy stops whatever runs for the service and starts dir/<build>, so a
// deploy is a replacement and two builds of one service never run at once. The
// process is started knowing the file it emits its quantity into, which is what
// makes the software the factory wrote observable at all.
//
// A build or a service name that is not a local path is refused before the stop,
// so what runs is left running. A binary missing from dir is an error from the
// start instead, with nothing left running for the service — there the stop has
// already happened.
func (l *Local) Deploy(_ context.Context, d targetseam.Deployment) error {
	if err := d.Validate(); err != nil {
		return err
	}
	// What this confines is the two joins below: the build string and the service
	// name both reach here from the store, and a target that joins whatever it is
	// handed runs whatever that names, so dir is the boundary and filepath.IsLocal is
	// what holds it — no parent traversal, no absolute path, no root.
	if !filepath.IsLocal(d.Build) {
		return fmt.Errorf("%w: %q", ErrBuildNotLocal, d.Build)
	}
	if !filepath.IsLocal(d.Service) {
		return fmt.Errorf("%w: %q", ErrServiceNotLocal, d.Service)
	}
	if err := l.stop(d.Service); err != nil {
		return err
	}

	cmd := exec.Command(filepath.Join(l.dir, d.Build))
	cmd.Env = append(os.Environ(), SignalEnv+"="+SignalFile(l.dir, d.Build))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("localtarget: starting %s for service %q: %w", d.Build, d.Service, err)
	}
	// Reap the process when it exits. An exited child that nobody waits on
	// stays in the process table as a zombie, and a zombie still answers
	// signal 0 as though it were alive — so without this, a process that died
	// on its own would read as running forever. A process started by an earlier
	// factory run has no waiter here, which [Local.ReadRunning] states the cost of.
	go func() { _ = cmd.Wait() }()

	record := d.Build + " " + strconv.Itoa(cmd.Process.Pid)
	if err := os.WriteFile(RunningFile(l.dir, d.Service), []byte(record), 0o644); err != nil {
		return fmt.Errorf("localtarget: recording what runs for service %q: %w", d.Service, err)
	}
	return nil
}

// Stop kills the service's process and removes what says it runs. A service with
// nothing running is not an error: what Stop promises is that nothing runs after it
// returns, and that already holds.
func (l *Local) Stop(_ context.Context, service string, credential secretref.Ref) error {
	if err := check(service, credential); err != nil {
		return err
	}
	return l.stop(service)
}

func (l *Local) stop(service string) error {
	build, pid, running, err := l.read(service)
	if err != nil {
		return err
	}
	if running && pid > 0 {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil &&
			!errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("localtarget: stopping build %s of service %q: %w", build, service, err)
		}
	}
	if err := os.Remove(RunningFile(l.dir, service)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("localtarget: clearing what runs for service %q: %w", service, err)
	}
	return nil
}

// ReadRunning is the build whose process is still alive, checked with signal 0 —
// delivered to nothing, refused where the process is gone. A dead process reads as
// nothing running: the target reports what runs, not what was started.
//
// It reads the file the deploy wrote rather than this value's own memory, so a
// process that did not perform the deploy gets the same answer — which is what the
// reconciler needs and the one thing the design requires of this operation. What it
// costs is that a process nobody is waiting on may sit in the process table as a
// zombie after it exits and answer signal 0, so a build started by an earlier
// factory run and since crashed can read as running until something reaps it.
func (l *Local) ReadRunning(_ context.Context, service string, credential secretref.Ref) (targetseam.Running, error) {
	if err := check(service, credential); err != nil {
		return targetseam.Running{}, err
	}
	build, pid, running, err := l.read(service)
	if err != nil {
		return targetseam.Running{}, err
	}
	if !running {
		return targetseam.Running{Service: service}, nil
	}
	if err := syscall.Kill(pid, syscall.Signal(0)); err != nil {
		return targetseam.Running{Service: service}, nil
	}
	return targetseam.Running{Service: service, Build: build}, nil
}

// read is what the file says: the build and the process id, and false where
// nothing has been started for the service in this directory. A file this package
// cannot read as those two is an error rather than nothing running — something
// changed the target underneath, which is what the reconciler exists to raise and
// not something to report as an empty target.
func (l *Local) read(service string) (string, int, bool, error) {
	content, err := os.ReadFile(RunningFile(l.dir, service))
	if errors.Is(err, os.ErrNotExist) {
		return "", 0, false, nil
	} else if err != nil {
		return "", 0, false, fmt.Errorf("localtarget: reading what runs for service %q: %w", service, err)
	}
	build, id, found := strings.Cut(strings.TrimSpace(string(content)), " ")
	if !found || build == "" {
		return "", 0, false, fmt.Errorf("localtarget: what runs for service %q reads %q, not a build and a process id",
			service, content)
	}
	pid, err := strconv.Atoi(id)
	if err != nil {
		return "", 0, false, fmt.Errorf("localtarget: the process id for service %q reads %q: %w", service, id, err)
	}
	return build, pid, true, nil
}

// check is what Stop and ReadRunning require: a service, and a credential
// reference. The seam requires the reference on every operation; doc.go says
// what this target does with it, which is nothing.
func check(service string, credential secretref.Ref) error {
	switch {
	case service == "":
		return fmt.Errorf("%w: it names no service", targetseam.ErrIncomplete)
	case credential.IsZero():
		return fmt.Errorf("%w: service %q references no credential", targetseam.ErrIncomplete, service)
	case !filepath.IsLocal(service):
		return fmt.Errorf("%w: %q", ErrServiceNotLocal, service)
	}
	return nil
}

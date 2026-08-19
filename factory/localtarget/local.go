package localtarget

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// Local is a [targetseam.Target] that runs each service's build as one local
// process, in one directory. There is a target per environment rather than per
// install: an environment names the addresses a deploy into it is performed
// against, and for this substrate an address is a directory — so a candidate
// environment gets a [Local] of its own and two candidates of one service run
// side by side without either reading the other's.
//
// It is not safe for concurrent use: nothing guards the process table, because
// the caller is the one crude path the surfaces are deferred with, which deploys
// one thing at a time, and a mutex would protect calls the design never makes at
// once.
type Local struct {
	dir     string
	running map[string]*process
}

var _ targetseam.Target = (*Local)(nil)

// process is one started process and the build it runs.
type process struct {
	cmd   *exec.Cmd
	build string
}

// ErrBuildNotLocal is returned by [Local.Deploy] for a build that is not a
// local path — one with a parent traversal in it, an absolute one, or a root.
// The build string reaches this from the store, and a target that joins
// whatever it is handed runs whatever that names: "../../usr/bin/whatever"
// under dir is a program outside the targets directory. What the check
// confines is the path this package builds and nothing else; the credential
// still reaches nothing, and the binary at a local path is trusted to be what
// the build put there.
var ErrBuildNotLocal = errors.New("localtarget: the build is not a local path")

// New returns a target over dir, where the deployable binary for a build is
// placed before Deploy is called, named exactly by the build string.
func New(dir string) *Local {
	return &Local{dir: dir, running: make(map[string]*process)}
}

// Deploy stops whatever runs for the service and starts dir/<build>, so a
// deploy is a replacement and two builds of one service never run at once.
// A build that is not a local path is [ErrBuildNotLocal], refused before
// the stop, so what runs is left running. A binary missing from dir is an
// error from the start instead, with nothing left running for the service —
// there the stop has already happened.
func (l *Local) Deploy(_ context.Context, d targetseam.Deployment) error {
	if err := d.Validate(); err != nil {
		return err
	}
	// What this confines is the join below: the build string reaches here
	// from the store, and a target that joins whatever it is handed runs
	// whatever that names, so dir is the boundary and filepath.IsLocal is what
	// holds it — no parent traversal, no absolute path, no root.
	if !filepath.IsLocal(d.Build) {
		return fmt.Errorf("%w: %q", ErrBuildNotLocal, d.Build)
	}
	if err := l.stop(d.Service); err != nil {
		return err
	}

	cmd := exec.Command(filepath.Join(l.dir, d.Build))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("localtarget: starting %s for service %q: %w", d.Build, d.Service, err)
	}
	// Reap the process when it exits. An exited child that nobody waits on
	// stays in the process table as a zombie, and a zombie still answers
	// signal 0 as though it were alive — so without this, a process that died
	// on its own would read as running forever.
	go func() { _ = cmd.Wait() }()

	l.running[d.Service] = &process{cmd: cmd, build: d.Build}
	return nil
}

// Stop kills the service's process and forgets it. A service with nothing
// running is not an error: what Stop promises is that nothing runs after it
// returns, and that already holds.
func (l *Local) Stop(_ context.Context, service string, credential secretref.Ref) error {
	if err := check(service, credential); err != nil {
		return err
	}
	return l.stop(service)
}

func (l *Local) stop(service string) error {
	p, ok := l.running[service]
	if !ok {
		return nil
	}
	if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("localtarget: stopping service %q: %w", service, err)
	}
	delete(l.running, service)
	return nil
}

// ReadRunning is the build whose process is still alive, checked with
// signal 0 — delivered to nothing, refused where the process is gone. A dead
// process reads as nothing running: the target reports what runs, not what
// was started.
func (l *Local) ReadRunning(_ context.Context, service string, credential secretref.Ref) (targetseam.Running, error) {
	if err := check(service, credential); err != nil {
		return targetseam.Running{}, err
	}
	p, ok := l.running[service]
	if !ok {
		return targetseam.Running{Service: service}, nil
	}
	if err := p.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return targetseam.Running{Service: service}, nil
	}
	return targetseam.Running{Service: service, Build: p.build}, nil
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
	}
	return nil
}

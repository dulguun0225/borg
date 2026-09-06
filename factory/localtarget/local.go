package localtarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// Local is a [targetseam.Target] that runs each service's build as one local
// process, in one directory. There is one Local per target and not one per
// environment: an environment names the addresses a deploy into it is performed
// against, plural and ordered, and on this platform an address is a directory —
// so an environment with three targets is three Locals, which the deployer
// reaches in the environment's order, and a candidate environment gets one of
// its own so that two candidates of one service run side by side without either
// reading the other's.
//
// What is running is on disk and not in this value, which is what lets a second
// process read it.
type Local struct {
	dir string
	// DrainWait is how long [Local.Deploy] gives the instance it replaces to
	// finish the requests it holds after it stops taking new ones. A process
	// still alive at the end of it is ended outright, and the deploy reports a
	// cut rather than a drain. It is a field so that a caller with a longer
	// slowest request can say so.
	DrainWait time.Duration
}

// DefaultDrainWait is what [New] sets [Local.DrainWait] to: long enough for a
// request a local demonstration holds open, and short enough that a rollout
// over several targets is not a wait per target.
const DefaultDrainWait = 2 * time.Second

var _ targetseam.Target = (*Local)(nil)

// SignalEnv is the environment variable each started process is told the file to
// emit its quantity into. The health monitor reads that file, so the name is here — one
// place, named by the platform that wires it, rather than agreed between the target
// and whatever reads it.
const SignalEnv = "BORG_SIGNAL"

// SignalFile is where the build running in dir emits its quantity. One file per
// build, so a release's own counts are told apart from the counts of the build that
// ran there before it — which is what the comparison's baseline is.
func SignalFile(dir, build string) string { return filepath.Join(dir, build+".signal") }

// ExchangeEnv is the environment variable each started process is told the file to
// write its exchange documents into. It is here beside [SignalEnv] and for the same
// reason: the name belongs to the platform that wires it, not to an agreement
// between the target and whatever reads it.
const ExchangeEnv = "BORG_EXCHANGE"

// ExchangeFile is where the build running in dir writes one document per unit of
// work — what it published, as the elements its contract names. One file per build,
// for the reason the signal file is one per build: a candidate's own documents are
// what a consumer contract is decided against, and the documents of the
// build that ran there before it are not.
//
// It is a second file rather than a second field of the signal's lines. The signal
// is what the health monitor counts and the exchange is what a predicate is decided
// against, and folding them into one format would make every reader of either parse
// the other's — and would rewrite a mechanism a milestone already built.
func ExchangeFile(dir, build string) string { return filepath.Join(dir, build+".exchange") }

// DeployEnv is the environment variable each started process is told the deploy
// record's own identity through. The health monitor's emission names it, which
// is what tells the instances this deploy placed from the instances of the same
// build an earlier deploy placed — the control's among them.
const DeployEnv = "BORG_DEPLOY"

// WayInEnv is the environment variable the way-in token is handed to the started
// process through, beside the service's own credentials. The deployer mints the
// token at every deploy and writes only its digest on the deploy record, so this
// is the one value this target is handed rather than a reference. The way in
// that would send it and the report store that would digest it are not built.
const WayInEnv = "BORG_WAY_IN"

// RunningFile is where the target records what it started for one service: the
// build, a space, and the process id. It is a file rather than a field, so that
// a process which did not start the software can still read what is running
// there — which is exactly what the drift detector is, and what the seam's
// read operation is for.
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
func New(dir string) *Local { return &Local{dir: dir, DrainWait: DefaultDrainWait} }

// Dir is the directory this target runs in, which is the address the
// environment record names it by.
func (l *Local) Dir() string { return l.dir }

// Deploy replaces whatever runs for the service with dir/<build>, so a deploy
// is a replacement and two builds of one service never run at once. The process
// is started knowing two files: the one it emits its quantity into, which is what
// makes the software the factory wrote observable at all, and the one it writes
// its exchange documents into, which is what a consumer contract is
// decided against. Beside them it is told the deploy record's own identity and,
// where the deployment carries one, the way-in token and every value of the
// resolved configuration.
//
// The instance it replaces is drained where it can be: it is asked to end, which
// is what stops new requests reaching it, and given [Local.DrainWait] to finish
// the ones it holds. One still alive at the end of that is ended outright and
// the deploy reports a cut, which is what a platform unable to hold a request
// open across the replacement performs and what the factory records as having
// happened.
//
// A build or a service name that is not a local path is refused before the
// replacement, so what runs is left running. A binary missing from dir is an
// error from the start instead, with nothing left running for the service —
// there the replacement has already happened.
func (l *Local) Deploy(_ context.Context, p principal.Principal, d targetseam.Deployment) (targetseam.Placement, error) {
	if err := targetseam.CheckPrincipal(p); err != nil {
		return targetseam.Placement{}, err
	}
	if err := d.Validate(); err != nil {
		return targetseam.Placement{}, err
	}
	// What this confines is the two joins below: the build string and the service
	// name both reach here from the store, and a target that joins whatever it is
	// handed runs whatever that names, so dir is the boundary and filepath.IsLocal is
	// what holds it — no parent traversal, no absolute path, no root.
	if !filepath.IsLocal(d.Build) {
		return targetseam.Placement{}, fmt.Errorf("%w: %q", ErrBuildNotLocal, d.Build)
	}
	if !filepath.IsLocal(d.Service) {
		return targetseam.Placement{}, fmt.Errorf("%w: %q", ErrServiceNotLocal, d.Service)
	}
	replacement, err := l.drain(d.Service)
	if err != nil {
		return targetseam.Placement{}, err
	}

	cmd := exec.Command(filepath.Join(l.dir, d.Build))
	cmd.Env = append(os.Environ(),
		SignalEnv+"="+SignalFile(l.dir, d.Build),
		ExchangeEnv+"="+ExchangeFile(l.dir, d.Build),
		DeployEnv+"="+d.DeployID)
	if d.WayInToken != "" {
		cmd.Env = append(cmd.Env, WayInEnv+"="+d.WayInToken)
	}
	for n, name := range d.Configuration.Names {
		cmd.Env = append(cmd.Env, name+"="+d.Configuration.Values[n])
	}
	if err := cmd.Start(); err != nil {
		return targetseam.Placement{}, fmt.Errorf("localtarget: starting %s for service %q: %w", d.Build, d.Service, err)
	}
	// Reap the process when it exits. An exited child that nobody waits on
	// stays in the process table as a zombie, and a zombie still answers
	// signal 0 as though it were alive — so without this, a process that died
	// on its own would read as running forever. A process started by an earlier
	// factory run has no waiter here, which [Local.ReadRunning] states the cost of.
	go func() { _ = cmd.Wait() }()

	record := d.Build + " " + strconv.Itoa(cmd.Process.Pid)
	if err := os.WriteFile(RunningFile(l.dir, d.Service), []byte(record), 0o644); err != nil {
		return targetseam.Placement{}, fmt.Errorf("localtarget: recording what runs for service %q: %w", d.Service, err)
	}
	return targetseam.Placement{Replacement: replacement}, nil
}

// drain asks the instance running for the service to end, waits
// [Local.DrainWait] for it to finish what it holds, and ends it outright where
// it does not. It reports a drain where nothing was running, there being no
// request to drop.
func (l *Local) drain(service string) (targetseam.Replacement, error) {
	build, pid, running, err := l.read(service)
	if err != nil {
		return "", err
	}
	if !running || pid <= 0 {
		return targetseam.ReplacementDrained, l.forget(service)
	}

	replacement := targetseam.ReplacementDrained
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !gone(err) {
		return "", fmt.Errorf("localtarget: draining build %s of service %q: %w", build, service, err)
	}
	deadline := time.Now().Add(l.DrainWait)
	for syscall.Kill(pid, syscall.Signal(0)) == nil {
		if time.Now().After(deadline) {
			replacement = targetseam.ReplacementCut
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !gone(err) {
				return "", fmt.Errorf("localtarget: ending build %s of service %q: %w", build, service, err)
			}
			break
		}
		time.Sleep(drainPoll)
	}
	return replacement, l.forget(service)
}

// drainPoll is how often the drain asks whether the process it is waiting on
// has ended. It is short against [DefaultDrainWait] and long enough that the
// wait is not a spin.
const drainPoll = 10 * time.Millisecond

func gone(err error) bool {
	return errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone)
}

// Stop ends every instance of the service on this target and removes what says
// it runs. A service with nothing running is not an error: what Stop promises is
// that nothing runs after it returns, and that already holds.
//
// It ends the process outright rather than draining it. Stop is the operation
// that ends every instance — a mitigation, a removal, a teardown — and none of
// the three is replacing what it ends with something that would serve the
// requests it holds. So it reports a cut and never a drain: the deploy record of
// a removal names what this reported, and a record naming a drain here would
// assert a drain nothing performed.
func (l *Local) Stop(_ context.Context, p principal.Principal, service string, credential secretref.Ref) (targetseam.Placement, error) {
	if err := targetseam.CheckPrincipal(p); err != nil {
		return targetseam.Placement{}, err
	}
	if err := check(service, credential); err != nil {
		return targetseam.Placement{}, err
	}
	if err := l.stop(service); err != nil {
		return targetseam.Placement{}, err
	}
	return targetseam.Placement{Replacement: targetseam.ReplacementCut}, nil
}

func (l *Local) stop(service string) error {
	build, pid, running, err := l.read(service)
	if err != nil {
		return err
	}
	if running && pid > 0 {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !gone(err) {
			return fmt.Errorf("localtarget: stopping build %s of service %q: %w", build, service, err)
		}
	}
	return l.forget(service)
}

// forget removes what says a build runs for the service.
func (l *Local) forget(service string) error {
	if err := os.Remove(RunningFile(l.dir, service)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("localtarget: clearing what runs for service %q: %w", service, err)
	}
	return nil
}

// ReadRunning is the build whose process is still alive, checked with signal 0 —
// delivered to nothing, refused where the process is gone — with the digest of
// the artifact it was started from, the one instance this platform runs of it,
// and the service's schema history. A dead process reads as nothing running: the
// target reports what runs, not what was started. The history is reported
// whether or not anything runs, being a fact of the store and not of the
// process.
//
// It reads the file the deploy wrote rather than this value's own memory, so a
// process that did not perform the deploy gets the same answer — which is what
// the drift detector needs and the one thing the design requires of this
// operation. What it costs is that a process nobody is waiting on may sit in
// the process table as a zombie after it exits and answer signal 0, so a build
// started by an earlier factory run and since crashed can read as running until
// something reaps it.
func (l *Local) ReadRunning(_ context.Context, p principal.Principal, service string, credential secretref.Ref) (targetseam.Running, error) {
	if err := targetseam.CheckPrincipal(p); err != nil {
		return targetseam.Running{}, err
	}
	if err := check(service, credential); err != nil {
		return targetseam.Running{}, err
	}
	history, err := l.history(service)
	if err != nil {
		return targetseam.Running{}, err
	}
	build, pid, running, err := l.read(service)
	if err != nil {
		return targetseam.Running{}, err
	}
	if !running {
		return targetseam.Running{Service: service, SchemaHistory: history}, nil
	}
	if err := syscall.Kill(pid, syscall.Signal(0)); err != nil {
		return targetseam.Running{Service: service, SchemaHistory: history}, nil
	}
	digest, err := l.digest(build)
	if err != nil {
		return targetseam.Running{}, err
	}
	// One process per service is the whole of this platform's capacity, so the
	// count a kept-instance figure is computed from is one while the process is
	// alive. A platform that ran several would report several here.
	return targetseam.Running{
		Service: service, Build: build, ArtifactDigest: digest, Instances: 1, SchemaHistory: history,
	}, nil
}

// digest is the sha256 of the artifact at dir/<build>, in hexadecimal, which is
// what a rollback verifies against the digest the build record holds and what
// the drift detector reads bytes rather than names with.
func (l *Local) digest(build string) (string, error) {
	if !filepath.IsLocal(build) {
		return "", fmt.Errorf("%w: %q", ErrBuildNotLocal, build)
	}
	content, err := os.ReadFile(filepath.Join(l.dir, build))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("localtarget: reading the artifact of build %s: %w", build, err)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

// read is what the file says: the build and the process id, and false where
// nothing has been started for the service in this directory. A file this
// package cannot read as those two is an error rather than nothing running —
// something changed the target underneath, which is what the independent
// driftdetector exists to raise and not something to report as an empty target.
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

// check is what every operation but Deploy requires: a service, and a credential
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

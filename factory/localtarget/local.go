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
	"github.com/dulguun0225/borg/factory/wayin"
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
}

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

// WayInSocket is where the way in inside the service running in dir listens: a
// Unix socket in the target's own directory, named by the service the way
// [RunningFile] is and not by the build the way [SignalFile] and
// [ExchangeFile] are. Those two are per build because a release's counts are
// read against the counts of the build that ran there before it; a socket
// accumulates nothing, one process runs per service here, and the shorter
// name is what keeps the path inside the bound below.
//
// A Unix socket path is bounded at about a hundred characters, which is the
// operating system's bound and not this package's. A directory deep enough to
// pass it — a candidate environment's, whose name carries the item's
// identifier — leaves that build's way in unable to listen, which the started
// process reports on its own log and which costs it its way in and nothing
// else.
//
// The three names the started process is told the token, the entrance and
// this path through are package wayin's, the shipped source being what reads
// them; this target sets them and spells none of them itself.
func WayInSocket(dir, service string) string { return filepath.Join(dir, service+".way-in") }

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
func New(dir string) *Local { return &Local{dir: dir} }

// Dir is the directory this target runs in, which is the address the
// environment record names it by.
func (l *Local) Dir() string { return l.dir }

// Deploy replaces whatever runs for the service with dir/<build>, so a deploy
// is a replacement and two builds of one service never run at once. The process
// is started knowing two files: the one it emits its quantity into, which is what
// makes the software the factory wrote observable at all, and the one it writes
// its exchange documents into, which is what a consumer contract is
// decided against. Beside them it is told the deploy record's own identity and,
// where the deployment carries them, the way-in token with the entrance it is
// presented at and the socket the way in listens on, and every value of the
// resolved configuration.
//
// The instance it replaces is drained: it is asked to end, which is what stops
// new requests reaching it, and waited on for as long as it takes to finish the
// ones it holds. Nothing here ends it before it has, so the replacement takes as
// long as the longest request it waits on and no request is dropped. A caller
// that will not wait that long cancels ctx, which is an error and not a
// replacement reported.
//
// A build or a service name that is not a local path is refused before the
// replacement, so what runs is left running. A binary missing from dir is an
// error from the start instead, with nothing left running for the service —
// there the replacement has already happened.
func (l *Local) Deploy(ctx context.Context, p principal.Principal, d targetseam.Deployment) (targetseam.Placement, error) {
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
	replacement, err := l.drain(ctx, d.Service)
	if err != nil {
		return targetseam.Placement{}, err
	}

	cmd := exec.Command(filepath.Join(l.dir, d.Build))
	cmd.Env = append(os.Environ(),
		SignalEnv+"="+SignalFile(l.dir, d.Build),
		ExchangeEnv+"="+ExchangeFile(l.dir, d.Build),
		DeployEnv+"="+d.DeployID)
	// The way in starts only where all three are set. The token is one of the
	// configuration values below, handed to the service the way every other
	// value is; these two are the platform's own, and a deployment carrying no
	// entrance is a factory serving none, where the way in in this build
	// listens nowhere rather than dialling something that is not there.
	if d.WayInAddress != "" {
		cmd.Env = append(cmd.Env,
			wayin.StoreEnv+"="+d.WayInAddress,
			wayin.ListenEnv+"="+WayInSocket(l.dir, d.Service))
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

// drain asks the instance running for the service to end and waits until it
// has finished what it holds. It ends nothing itself: neither rollout row drops
// a request, so the wait is as long as the longest request the instance is
// serving, and a caller unwilling to wait cancels ctx and gets that error
// rather than a replacement. It reports a drain where nothing was running,
// there being no request to drop.
func (l *Local) drain(ctx context.Context, service string) (targetseam.Replacement, error) {
	build, pid, running, err := l.read(service)
	if err != nil {
		return "", err
	}
	if !running || pid <= 0 {
		return targetseam.ReplacementDrained, l.forget(service)
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !gone(err) {
		return "", fmt.Errorf("localtarget: draining build %s of service %q: %w", build, service, err)
	}
	for syscall.Kill(pid, syscall.Signal(0)) == nil {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("localtarget: waiting for build %s of service %q to finish what it holds: %w",
				build, service, ctx.Err())
		case <-time.After(drainPoll):
		}
	}
	return targetseam.ReplacementDrained, l.forget(service)
}

// drainPoll is how often the drain asks whether the process it is waiting on
// has ended. It is long enough that the wait is not a spin and short against
// the request it is waiting for.
const drainPoll = 10 * time.Millisecond

func gone(err error) bool {
	return errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone)
}

// Stop ends every instance of the service on this target and removes what says
// it runs. A service with nothing running is not an error: what Stop promises is
// that nothing runs after it returns, and that already holds.
//
// It ends the instance the way a replacement does — asked to end, then waited
// on until it has finished what it holds — and reports the drain that is. The
// deploy record of a removal names what this reported, so nothing here may
// report a request finished that was dropped; a caller unwilling to wait
// cancels ctx.
func (l *Local) Stop(ctx context.Context, p principal.Principal, service string, credential secretref.Ref) (targetseam.Placement, error) {
	if err := targetseam.CheckPrincipal(p); err != nil {
		return targetseam.Placement{}, err
	}
	if err := check(service, credential); err != nil {
		return targetseam.Placement{}, err
	}
	ended, err := l.drain(ctx, service)
	if err != nil {
		return targetseam.Placement{}, err
	}
	return targetseam.Placement{Replacement: ended}, nil
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

// digest is the sha256 of the artifact at dir/<build>, "sha256:" and then
// hexadecimal — the form [targetseam.Running.ArtifactDigest] and the build
// record share, which is what a rollback verifies against the digest the
// build record holds and what the drift detector reads bytes rather than
// names with.
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
	return "sha256:" + hex.EncodeToString(sum[:]), nil
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

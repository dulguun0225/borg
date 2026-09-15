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
	"time"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/wayin"
)

var (
	// ErrNoPreviousBuild is returned when a share is requested without a build
	// already running to serve as its control.
	ErrNoPreviousBuild = errors.New("localtarget: no previous build is available as a control")
	// ErrOneInstance is returned by [Local.SetInstanceCount] for any count but
	// the one release instance this platform runs.
	ErrOneInstance = errors.New("localtarget: this platform runs one release instance and cannot run another number")
)

func (l *Local) validateDeployment(p principal.Principal, d targetseam.Deployment) error {
	if err := l.signalError(); err != nil {
		return err
	}
	if err := targetseam.CheckPrincipal(p); err != nil {
		return err
	}
	if err := d.Validate(); err != nil {
		return err
	}
	if !filepath.IsLocal(d.Build) {
		return fmt.Errorf("%w: %q", ErrBuildNotLocal, d.Build)
	}
	if !filepath.IsLocal(d.Service) {
		return fmt.Errorf("%w: %q", ErrServiceNotLocal, d.Service)
	}
	return nil
}

// DeployWithControl places a release beside the running process and records
// that process as both the control and kept fleet.
func (l *Local) DeployWithControl(ctx context.Context, p principal.Principal, d targetseam.Deployment) (targetseam.Placement, error) {
	if err := l.validateDeployment(p, d); err != nil {
		return targetseam.Placement{}, err
	}
	oldBuild, oldPID, running, err := l.read(d.Service)
	if err != nil {
		return targetseam.Placement{}, err
	}
	if !running {
		return l.Deploy(ctx, p, d)
	}
	old := oldBuild + " " + strconv.Itoa(oldPID)
	for _, file := range []string{ControlFile(l.dir, d.Service), KeptFile(l.dir, d.Service)} {
		if err := os.WriteFile(file, []byte(old), 0o644); err != nil {
			return targetseam.Placement{}, fmt.Errorf("localtarget: recording the control for service %q: %w", d.Service, err)
		}
	}
	return l.startMain(d, targetseam.ReplacementDrained)
}

// Reconfigure refreshes the kept process when a rollback can use it; without
// a kept process it restarts the running build.
func (l *Local) Reconfigure(ctx context.Context, p principal.Principal, r targetseam.Reconfiguration) (targetseam.Placement, error) {
	if err := targetseam.CheckPrincipal(p); err != nil {
		return targetseam.Placement{}, err
	}
	if err := r.Validate(); err != nil {
		return targetseam.Placement{}, err
	}
	deployment := targetseam.Deployment{Service: r.Service, Build: r.Build, Credential: r.Credential,
		Configuration: r.Configuration, WayInAddress: r.WayInAddress}
	if err := deployment.Validate(); err != nil {
		return targetseam.Placement{}, err
	}
	kept := KeptFile(l.dir, r.Service)
	if _, err := os.Stat(kept); errors.Is(err, os.ErrNotExist) {
		return l.Deploy(ctx, p, deployment)
	} else if err != nil {
		return targetseam.Placement{}, fmt.Errorf("localtarget: checking the kept process for service %q: %w", r.Service, err)
	}
	if _, err := l.drain(ctx, r.Service); err != nil {
		return targetseam.Placement{}, err
	}
	if err := l.StopKept(ctx, p, r.Service); err != nil {
		return targetseam.Placement{}, err
	}
	if err := l.start(deployment, kept); err != nil {
		return targetseam.Placement{}, err
	}
	content, err := os.ReadFile(kept)
	if err != nil {
		return targetseam.Placement{}, err
	}
	if err := os.WriteFile(RunningFile(l.dir, r.Service), content, 0o644); err != nil {
		return targetseam.Placement{}, fmt.Errorf("localtarget: recording the reconfigured service %q: %w", r.Service, err)
	}
	return targetseam.Placement{Replacement: targetseam.ReplacementDrained}, nil
}

// ShiftTraffic writes the build and share both processes read. Existing control
// and kept processes are reused, including after a full shift.
func (l *Local) ShiftTraffic(ctx context.Context, p principal.Principal, s targetseam.Shift) error {
	if err := l.signalError(); err != nil {
		return err
	}
	if err := targetseam.CheckPrincipal(p); err != nil {
		return err
	}
	if err := s.Validate(); err != nil {
		return err
	}
	if !filepath.IsLocal(s.Build) {
		return fmt.Errorf("%w: %q", ErrBuildNotLocal, s.Build)
	}
	if !filepath.IsLocal(s.Service) {
		return fmt.Errorf("%w: %q", ErrServiceNotLocal, s.Service)
	}
	control, found, err := l.sideBuild(ControlFile(l.dir, s.Service))
	if err != nil {
		return err
	}
	if !found {
		control, found, err = l.sideBuild(KeptFile(l.dir, s.Service))
		if err != nil {
			return err
		}
	}
	if !found && s.Share < 1 {
		return ErrNoPreviousBuild
	}
	traffic := fmt.Sprintf("%s %.17g\n", s.Build, s.Share)
	if found && control != s.Build {
		traffic += fmt.Sprintf("%s %.17g\n", control, 1-s.Share)
	}
	if err := os.WriteFile(TrafficFile(l.dir, s.Service), []byte(traffic), 0o644); err != nil {
		return fmt.Errorf("localtarget: writing traffic for service %q: %w", s.Service, err)
	}
	return nil
}

// StopControl ends the cold control copy while leaving the kept copy serving
// the rest of traffic.
func (l *Local) StopControl(ctx context.Context, p principal.Principal, service string) error {
	if err := targetseam.CheckPrincipal(p); err != nil {
		return err
	}
	if service == "" {
		return fmt.Errorf("%w: it names no service", targetseam.ErrIncomplete)
	}
	control := ControlFile(l.dir, service)
	kept := KeptFile(l.dir, service)
	controlPID, controlFound, err := l.sidePID(control)
	if err != nil || !controlFound {
		return err
	}
	keptPID, keptFound, err := l.sidePID(kept)
	if err != nil {
		return err
	}
	if keptFound && keptPID == controlPID {
		return os.Remove(control)
	}
	return l.stopFile(ctx, control, service)
}

// StopKept ends the kept copy after the last window that could return to it
// closes.
func (l *Local) StopKept(ctx context.Context, p principal.Principal, service string) error {
	if err := targetseam.CheckPrincipal(p); err != nil {
		return err
	}
	kept := KeptFile(l.dir, service)
	keptPID, found, err := l.sidePID(kept)
	if err != nil || !found {
		return err
	}
	if err := l.stopFile(ctx, kept, service); err != nil {
		return err
	}
	control := ControlFile(l.dir, service)
	controlPID, controlFound, err := l.sidePID(control)
	if err != nil {
		return err
	}
	if controlFound && controlPID == keptPID {
		if err := os.Remove(control); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// SetInstanceCount answers a count of one, which is the release capacity this
// platform provides, and refuses every other count.
func (l *Local) SetInstanceCount(_ context.Context, p principal.Principal, c targetseam.InstanceCount) error {
	if err := targetseam.CheckPrincipal(p); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Count == 1 {
		return nil
	}
	return fmt.Errorf("%w: service %q asked for %d", ErrOneInstance, c.Service, c.Count)
}

func (l *Local) startMain(d targetseam.Deployment, replacement targetseam.Replacement) (targetseam.Placement, error) {
	if err := l.start(d, RunningFile(l.dir, d.Service)); err != nil {
		return targetseam.Placement{}, err
	}
	return targetseam.Placement{Replacement: replacement}, nil
}

func (l *Local) start(d targetseam.Deployment, file string) error {
	cmd := exec.Command(filepath.Join(l.dir, d.Build))
	output, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("localtarget: connecting build %s output: %w", d.Build, err)
	}
	deploy := deployID(d.Configuration)
	cmd.Env = append(os.Environ(), ExchangeEnv+"="+ExchangeFile(l.dir, d.Build), TrafficEnv+"="+TrafficFile(l.dir, d.Service),
		BuildEnv+"="+d.Build,
		DeployEnv+"="+deploy, TargetEnv+"="+l.dir)
	if d.WayInAddress != "" {
		cmd.Env = append(cmd.Env, wayin.StoreEnv+"="+d.WayInAddress,
			wayin.ListenEnv+"="+WayInSocket(l.dir, d.Service))
	}
	for n, name := range d.Configuration.Names {
		cmd.Env = append(cmd.Env, name+"="+d.Configuration.Values[n])
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("localtarget: starting side-by-side build %s for service %q: %w", d.Build, d.Service, err)
	}
	l.startSignalReader(output, SignalFile(l.dir, d.Build), deploy)
	go func() { _ = cmd.Wait() }()
	if err := os.WriteFile(file, []byte(d.Build+" "+strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		return fmt.Errorf("localtarget: recording the side-by-side build for service %q: %w", d.Service, err)
	}
	return nil
}

func (l *Local) sideBuild(file string) (string, bool, error) {
	content, found, err := l.sideContent(file)
	if err != nil || !found {
		return "", found, err
	}
	fields := strings.Fields(content)
	if len(fields) != 2 {
		return "", false, fmt.Errorf("localtarget: the side-by-side process file %q is malformed", file)
	}
	return fields[0], true, nil
}

func (l *Local) sidePID(file string) (int, bool, error) {
	content, found, err := l.sideContent(file)
	if err != nil || !found {
		return 0, found, err
	}
	fields := strings.Fields(content)
	if len(fields) != 2 {
		return 0, false, fmt.Errorf("localtarget: the side-by-side process file %q is malformed", file)
	}
	pid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false, fmt.Errorf("localtarget: the side-by-side process file %q: %w", file, err)
	}
	return pid, true, nil
}

func (l *Local) sideContent(file string) (string, bool, error) {
	content, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("localtarget: reading the side-by-side process file %q: %w", file, err)
	}
	return strings.TrimSpace(string(content)), true, nil
}

func (l *Local) stopFile(ctx context.Context, file, service string) error {
	pid, found, err := l.sidePID(file)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !gone(err) {
		return fmt.Errorf("localtarget: stopping the side-by-side build of service %q: %w", service, err)
	}
	for syscall.Kill(pid, syscall.Signal(0)) == nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(drainPoll):
		}
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("localtarget: clearing the side-by-side build of service %q: %w", service, err)
	}
	return nil
}

package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/window"
)

// passInterval is what every last check this pass writes promises the next
// one within — the interval a reader with no authored value holds the
// record against, ../../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md's
// "the detector supplies its own interval the way it supplies its own
// recorded head, the owner installing it once and authoring no interval
// after."
const passInterval = 5 * time.Minute

// callerPrincipal is who the drift detector calls the target seam as: a
// component, like every other caller seam 5 puts a principal on, deciding
// nothing on it — populated, self-asserted, enforced by nothing.
var callerPrincipal = principal.OfComponent("driftdetector")

// pass is the first comparison of every service on every production target it
// runs on: the target's build and, where it answers, its digest, against the
// release the factory recorded.
func pass(ctx context.Context, s stores, out io.Writer, credential secretref.Ref,
	targetAt func(dir string) targetseam.Target) error {
	services, err := service.All(ctx, s.factory)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		fmt.Fprintln(out, "The factory has no services; there is nothing to check")
		return nil
	}

	writer := driftdetector.NewWriter(s.own)
	checkedAny := false
	for _, svc := range services {
		// Production is one record per project, and a service names its
		// project, so it is read here rather than once for the whole pass —
		// the arrangement that reads right whether every service is in one
		// project or several.
		production, found, err := environment.Production(ctx, s.factory, svc.ProjectID)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		checkedAny = true
		// The detector reads the targets the service runs on and no other for
		// that service: the service record's own field, and every target of the
		// environment where it names none, which is what an unwritten field
		// means. A target of the environment the service does not run on runs
		// somebody else's software and is that service's mismatch of nothing.
		addresses := runsOn(production, svc)
		// The command assembles the rollout exemption's inputs; the package
		// decides it, in driftdetector.Excused.
		windows, err := openWindows(ctx, s.factory, svc.ID, production.ID)
		if err != nil {
			return err
		}
		for _, address := range addresses {
			recordedReleaseID, recordedBuildID, err := recordedFor(ctx, s.factory, svc.ID, production.ID, address)
			if err != nil {
				return err
			}
			var recordedDigest string
			if recordedBuildID != "" {
				if b, err := build.Get(ctx, s.factory, recordedBuildID); err == nil {
					recordedDigest = b.ArtifactDigest
				}
			}
			p := driftdetector.Pass{
				ServiceID:         svc.ID,
				Target:            address,
				RecordedReleaseID: recordedReleaseID,
				RecordedBuildID:   recordedBuildID,
				RecordedDigest:    recordedDigest,
				Interval:          passInterval,
			}
			running, err := targetAt(address).ReadRunning(ctx, callerPrincipal, svc.Name, credential)
			if err != nil {
				// Failing to reach a target is not a mismatch: a network blip would
				// otherwise hold every production deploy, which is why the last check
				// exists at all.
				p.Why = err.Error()
			} else {
				p.Reached = true
				p.RunningBuild = running.Build
				p.RunningDigest = running.ArtifactDigest
				// The deployer's last check is kept per persistent target and not
				// per environment, so its subject is address and not production.ID —
				// the exemption stops standing on this one target's own advance,
				// whatever the deployer's pass over the rest of the environment did.
				var deployer *lastcheck.LastCheck
				if check, found, err := lastcheck.Get(ctx, s.factory, lastcheck.ComponentDeployer, address); err == nil && found {
					deployer = &check
				}
				p.Excused = running.Build != "" &&
					driftdetector.Excused(windows, address, running.Build, deployer, time.Now())
			}

			written, err := writer.Record(ctx, p)
			if err != nil {
				return err
			}
			report(out, svc.Name, p, written)
		}
	}
	if !checkedAny {
		fmt.Fprintln(out, "The factory has no production environment record; there is nothing to check")
	}
	return nil
}

// runsOn is which of a production environment's targets one service runs on: the
// service record's own field, and every target of the environment where the
// record names none, which is what an unwritten field means.
func runsOn(production environment.Environment, svc service.Service) []string {
	if len(svc.Targets) == 0 {
		return production.Addresses()
	}
	return svc.Targets
}

// recordedFor is what the factory recorded for one target: [driftdetector.RecordedRelease]'s
// own decision, over the target's own row of every one of the service's
// deploy records into this environment — the release it names where the
// target is marked complete, the previous one where it is not, and nothing
// where the complete record is a removal's.
//
// It is one read per target and not one for the whole service, because the
// comparison is per target: read over the whole set instead, every target of
// a rollout in progress is compared against the release below, so the target
// the rollout has already completed on disagrees, and that mismatch holds
// the service's production deploys and pages until a human clears it. This
// command assembles the sequence; [driftdetector.RecordedRelease] decides it.
func recordedFor(ctx context.Context, pool *pgxpool.Pool, serviceID, environmentID, address string) (releaseID, buildID string, err error) {
	deploys, err := deploy.ForEnvironment(ctx, pool, environmentID)
	if err != nil {
		return "", "", err
	}
	records := make([]driftdetector.RecordedDeploy, 0, len(deploys))
	for _, d := range deploys {
		if d.ServiceID != serviceID {
			continue
		}
		targets, err := deploy.Targets(ctx, pool, d.ID)
		if err != nil {
			return "", "", err
		}
		for _, t := range targets {
			if t.NotRunHere || t.Address != address {
				continue
			}
			records = append(records, driftdetector.RecordedDeploy{
				ReleaseID: d.ReleaseID,
				BuildID:   d.BuildID,
				Number:    int(d.Number),
				Complete:  t.Completion == deploy.CompletionComplete,
				Removal:   d.ReleaseID == "" && d.BuildID == "",
			})
		}
	}
	releaseID, buildID = driftdetector.RecordedRelease(records)
	return releaseID, buildID, nil
}

// openWindows assembles every open analysis window of one service as
// [driftdetector.Excused] reads it: the window's own clock and cap, the
// build it watches, and its deploy's own targets with their completion,
// control build, and kept build. [window.Window.BuildID] is the build under
// watch on every window, whether it names a release or, for a window the
// search opened, none — so no further read of the release is needed for it,
// and [deploy.Target.ControlBuildID] is likewise already the build a
// target's control runs. [rollbackTargetBuildID] is the fallback where a
// target ran no control: the release a rollback of the window's own release
// would return to, read the way [healthmonitor.HealthMonitor.TargetBelow]
// does. The decision over these inputs is [driftdetector.Excused]'s.
func openWindows(ctx context.Context, pool *pgxpool.Pool, serviceID, environmentID string) ([]driftdetector.OpenWindow, error) {
	open, err := window.AllOpen(ctx, pool, serviceID)
	if err != nil {
		return nil, err
	}
	windows := make([]driftdetector.OpenWindow, 0, len(open))
	for _, w := range open {
		targets, err := deploy.Targets(ctx, pool, w.DeployID)
		if err != nil {
			return nil, err
		}

		// The fallback is single-valued for the whole window, not per target: it
		// is computed for the release under watch and not for a rollback of it,
		// the way the design states. A window over a search's build names no
		// release and falls back to nothing.
		var fallbackBuildID string
		if w.ReleaseID != "" {
			if rel, err := release.Get(ctx, pool, w.ReleaseID); err == nil {
				if buildID, found, err := rollbackTargetBuildID(ctx, pool, serviceID, environmentID, rel.Number); err == nil && found {
					fallbackBuildID = buildID
				}
			}
		}

		wt := make([]driftdetector.WindowTarget, 0, len(targets))
		for _, t := range targets {
			// A rollback needs instances there to return to: a target whose kept
			// fleet is nothing, or torn down, falls back to neither the control
			// nor the release below.
			kept := t.Fleets.Kept.Instances > 0 && t.Fleets.Kept.TornDownAt == ""
			var keptBuildID string
			switch {
			case !kept:
			case t.ControlBuildID != "":
				keptBuildID = t.ControlBuildID
			default:
				keptBuildID = fallbackBuildID
			}
			wt = append(wt, driftdetector.WindowTarget{
				Address:        t.Address,
				Complete:       t.Completion == deploy.CompletionComplete,
				ControlBuildID: t.ControlBuildID,
				KeptBuildID:    keptBuildID,
			})
		}
		var builds []string
		if w.BuildID != "" {
			builds = []string{w.BuildID}
		}
		windows = append(windows, driftdetector.OpenWindow{
			OpenedAt:   w.At,
			CapSeconds: w.CapSeconds,
			Builds:     builds,
			Targets:    wt,
		})
	}
	return windows, nil
}

// rollbackTargetBuildID is the build of the release a rollback of the
// release under watch (number, in serviceID's own sequence) would return to:
// the newest release below it whose window closed passed or timed out,
// descending past skipped, past any window still open, and past a release
// whose deploy stopped before its build took traffic. It duplicates
// [healthmonitor.HealthMonitor.TargetBelow]'s own query rather than
// importing that package, which driftdetector's dependency graph refuses —
// this is a caller assembling an input for [driftdetector.Excused], the way
// every other field on [driftdetector.WindowTarget] already is.
func rollbackTargetBuildID(ctx context.Context, pool *pgxpool.Pool, serviceID, environmentID string, number int64) (string, bool, error) {
	closed, err := window.ClosedPassedOrTimedOut(ctx, pool, serviceID)
	if err != nil {
		return "", false, err
	}
	var best release.Release
	found := false
	for _, win := range closed {
		if win.ReleaseID == "" {
			continue
		}
		r, err := release.Get(ctx, pool, win.ReleaseID)
		if err != nil {
			return "", false, err
		}
		if r.Number >= number || (found && r.Number <= best.Number) {
			continue
		}
		took, err := tookTraffic(ctx, pool, environmentID, r.ID)
		if err != nil {
			return "", false, err
		}
		if !took {
			continue
		}
		best, found = r, true
	}
	if !found {
		return "", false, nil
	}
	return best.BuildID, true, nil
}

// tookTraffic is whether any deploy of releaseID into environmentID got far
// enough for its build to serve, which is what tells a release whose deploy
// never landed apart from one that did. It duplicates
// [healthmonitor.HealthMonitor.tookTraffic] for the reason
// [rollbackTargetBuildID] states.
func tookTraffic(ctx context.Context, pool *pgxpool.Pool, environmentID, releaseID string) (bool, error) {
	deploys, err := deploy.ByRelease(ctx, pool, environmentID, releaseID)
	if err != nil {
		return false, err
	}
	for _, d := range deploys {
		targets, err := deploy.Targets(ctx, pool, d.ID)
		if err != nil {
			return false, err
		}
		for _, t := range targets {
			if t.Completion == deploy.CompletionComplete || t.Completion == deploy.CompletionRolledBack {
				return true, nil
			}
		}
	}
	return false, nil
}

func report(out io.Writer, serviceName string, p driftdetector.Pass, written driftdetector.Recorded) {
	switch {
	case !p.Reached:
		fmt.Fprintf(out, "%s on %s: the target could not be reached, which is no mismatch — %s\n",
			serviceName, p.Target, p.Why)
	case written.Raised != "":
		fmt.Fprintf(out, "%s on %s: MISMATCH %s — the target runs %q and the factory recorded %q\n",
			serviceName, p.Target, written.Raised, p.RunningBuild, p.RecordedBuildID)
		fmt.Fprintln(out, "  it holds that service's production deploys until a human clears it here, and the factory cannot")
	case written.Agreed != "":
		fmt.Fprintf(out, "%s on %s: agrees now, and mismatch %s still stands — a later agreement is recorded on it as evidence\n",
			serviceName, p.Target, written.Agreed)
	case p.Excused:
		fmt.Fprintf(out, "%s on %s: the target runs %q, which an open analysis window accounts for\n",
			serviceName, p.Target, p.RunningBuild)
	default:
		fmt.Fprintf(out, "%s on %s: agrees — build %q\n", serviceName, p.Target, p.RunningBuild)
	}
}

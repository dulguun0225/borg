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
// record against, 08-drift-detection.md's "the detector supplies its own
// interval the way it supplies its own recorded head, the owner installing
// it once and authoring no interval after."
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
		excused, err := excusedBuilds(ctx, s.factory, svc.ID)
		if err != nil {
			return err
		}
		for _, address := range addresses {
			recorded, err := recordedFor(ctx, s.factory, svc.ID, production.ID, address)
			if err != nil {
				return err
			}
			var recordedDigest string
			if recorded.BuildID != "" {
				if b, err := build.Get(ctx, s.factory, recorded.BuildID); err == nil {
					recordedDigest = b.ArtifactDigest
				}
			}
			p := driftdetector.Pass{
				ServiceID:         svc.ID,
				Target:            address,
				RecordedReleaseID: recorded.ReleaseID,
				RecordedBuildID:   recorded.BuildID,
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
				p.Excused = running.Build != "" && excused[address][running.Build] &&
					!deployerLastCheckStale(ctx, s.factory, address)
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

// recordedFor is what the factory recorded for one target: the release its
// service's production deploy record names where that target is marked
// complete, the previous one where it is not, and nothing where the complete
// record is a removal's.
//
// It is one read per target and not one for the whole service, because the
// comparison is per target. [deploy.Current] over that one address answers
// exactly the sentence above — the highest-numbered release marked complete
// there, and nothing where a removal complete there is newer. Read over the
// whole set instead, every target of a rollout in progress is compared against
// the release below, so the target the rollout has already completed on
// disagrees, and that mismatch holds the service's production deploys and pages
// until a human clears it.
func recordedFor(ctx context.Context, pool *pgxpool.Pool, serviceID, environmentID, address string) (deploy.Deploy, error) {
	current, running, err := deploy.Current(ctx, pool, serviceID, environmentID, []string{address})
	if err != nil || !running {
		return deploy.Deploy{}, err
	}
	return current, nil
}

// excusedBuilds is every build an open analysis window accounts for, per target:
// the build of the release under watch, the build of the control that window's
// deploy record names, and the build of the release a rollback of it would
// return to, which is the same release the control runs. A build running beside
// the current release is a mismatch only where no open window names it, or the
// independent driftdetector would page on every rollout it sees.
//
// It is per target because the exemption is: it is never granted on a target the
// deploy record marks complete, that target being meant to run the release under
// watch, so an old build on it is a mismatch whatever window is open. The
// exemption covers the targets the rollout has not reached yet and nothing else.
//
// It is bounded by the window's own cap as well: a window open past it excuses
// nothing, because the record that suppresses the check is written by the
// component whose failure the check exists to survive, and an exemption that
// outlives the thing it describes is one a stopped health monitor holds open
// forever.
func excusedBuilds(ctx context.Context, pool *pgxpool.Pool, serviceID string) (map[string]map[string]bool, error) {
	open, err := window.AllOpen(ctx, pool, serviceID)
	if err != nil {
		return nil, err
	}
	excused := map[string]map[string]bool{}
	for _, w := range open {
		past, err := w.PastCap(time.Now())
		if err != nil || past {
			continue
		}
		builds := map[string]bool{}
		if w.ReleaseID != "" {
			rel, err := release.Get(ctx, pool, w.ReleaseID)
			if err != nil {
				return nil, err
			}
			builds[rel.BuildID] = true
		} else if w.BuildID != "" {
			// A window the search opened names a build and no release, and that
			// build is running beside the current release for as long as the
			// window is open.
			builds[w.BuildID] = true
		}
		dep, err := deploy.Get(ctx, pool, w.DeployID)
		if err != nil {
			return nil, err
		}
		if dep.ControlReleaseID != "" {
			control, err := release.Get(ctx, pool, dep.ControlReleaseID)
			if err != nil {
				return nil, err
			}
			// One release covers two of the three: the control runs the build of
			// the release a rollback of this deploy would return to.
			builds[control.BuildID] = true
		}

		targets, err := deploy.Targets(ctx, pool, dep.ID)
		if err != nil {
			return nil, err
		}
		for _, t := range targets {
			if t.Completion == deploy.CompletionComplete {
				continue
			}
			if excused[t.Address] == nil {
				excused[t.Address] = map[string]bool{}
			}
			for build := range builds {
				excused[t.Address][build] = true
			}
		}
	}
	return excused, nil
}

// deployerLastCheckStale is the rollout exemption's second bound: a target
// the exemption covers whose deployer last check is past the interval it
// names, with a further pass owed, stops being exempt.
func deployerLastCheckStale(ctx context.Context, pool *pgxpool.Pool, target string) bool {
	check, found, err := lastcheck.Get(ctx, pool, lastcheck.ComponentDeployer, target)
	if err != nil || !found {
		// No last check recorded for this target yet is not staleness — a
		// fresh install has none, and excusing on the windows alone is the
		// behaviour before this bound existed.
		return false
	}
	stale, err := check.Stale(time.Now())
	if err != nil {
		return false
	}
	return stale
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

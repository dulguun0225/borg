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

// pass is the first comparison of every service on every production target:
// the target's build and, where it answers, its digest, against the release
// the factory recorded.
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
		addresses := make([]string, len(production.Targets))
		for n, t := range production.Targets {
			addresses[n] = t.Address
		}
		recorded, err := recordedFor(ctx, s.factory, svc.ID, production.ID, addresses)
		if err != nil {
			return err
		}
		var recordedDigest string
		if recorded.BuildID != "" {
			if b, err := build.Get(ctx, s.factory, recorded.BuildID); err == nil {
				recordedDigest = b.ArtifactDigest
			}
		}
		excused, err := excusedBuilds(ctx, s.factory, svc.ID)
		if err != nil {
			return err
		}
		for _, target := range production.Targets {
			p := driftdetector.Pass{
				ServiceID:         svc.ID,
				Target:            target.Address,
				RecordedReleaseID: recorded.ReleaseID,
				RecordedBuildID:   recorded.BuildID,
				RecordedDigest:    recordedDigest,
				Interval:          passInterval,
			}
			running, err := targetAt(target.Address).ReadRunning(ctx, callerPrincipal, svc.Name, credential)
			if err != nil {
				// Failing to reach a target is not a mismatch: a network blip would
				// otherwise hold every production deploy, which is why the last check
				// exists at all.
				p.Why = err.Error()
			} else {
				p.Reached = true
				p.RunningBuild = running.Build
				p.RunningDigest = running.ArtifactDigest
				p.Excused = running.Build != "" && excused[running.Build] &&
					!deployerLastCheckStale(ctx, s.factory, target.Address)
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

// recordedFor is what the factory recorded as the service's current release in
// production: the release and the build its production deploy record names, and
// nothing where no deploy of it has completed on every one of addresses.
//
// It reads the newest deploy complete on every address the environment
// names rather than a true per-target reading with the previous-release
// and removal fallbacks 08-drift-detection.md:6-10 states — deploy does not
// yet expose a per-target walk back through a service's deploys, so a
// rollout in progress on one target while another is already current reads
// here as "not yet current" everywhere rather than per target. That gap is
// open in the report this dispatch returns.
func recordedFor(ctx context.Context, pool *pgxpool.Pool, serviceID, environmentID string, addresses []string) (deploy.Deploy, error) {
	current, running, err := deploy.Current(ctx, pool, serviceID, environmentID, addresses)
	if err != nil || !running {
		return deploy.Deploy{}, err
	}
	return current, nil
}

// excusedBuilds is every build an open analysis window accounts for: the build of
// the release under watch, and — where a platform keeps one — the build of the
// control that window's deploy record names. A build running beside the current
// release is a mismatch only where no open window names it, or the independent
// driftdetector would page on every rollout it sees.
//
// On this platform the set is never the reason a pass agrees. One directory runs one
// process, so what a target reports is either the current release's build or a
// disagreement, and a release under watch is the current release. The read is here
// because the rule is the design's and not the platform's, and because a control
// running after its window closed is meant to be a mismatch like any other — which
// is how a failed teardown is caught.
func excusedBuilds(ctx context.Context, pool *pgxpool.Pool, serviceID string) (map[string]bool, error) {
	open, err := window.AllOpen(ctx, pool, serviceID)
	if err != nil {
		return nil, err
	}
	excused := map[string]bool{}
	for _, w := range open {
		rel, err := release.Get(ctx, pool, w.ReleaseID)
		if err != nil {
			return nil, err
		}
		excused[rel.BuildID] = true
		// The control the window's deploy record names would be added here. There are
		// no columns for one, because this platform starts none, as package deploy's
		// doc.go states.
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

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// Ops: what is running rather than what needs a human. Which release of each
// service is running is the deploy record's answer per target, so a rollout
// stalled on one target is visible before anything pages.

// Ops is every service on every environment this install holds.
func (v *views) Ops(ctx context.Context, _ principal.Principal) (screens.Ops, error) {
	services, err := service.All(ctx, v.p.d.pool)
	if err != nil {
		return screens.Ops{}, err
	}
	var board screens.Ops
	for _, svc := range services {
		summary, found, err := v.serviceSummary(ctx, svc)
		if err != nil {
			return screens.Ops{}, err
		}
		if !found {
			continue
		}
		board.Services = append(board.Services, summary)
	}
	return board, nil
}

// serviceSummary is one row of the Ops board: which release of the service
// production is running and a short reading of what it is watched for.
func (v *views) serviceSummary(ctx context.Context, svc service.Service) (screens.ServiceSummary, bool, error) {
	summary := screens.ServiceSummary{ServiceID: svc.ID, EnvironmentID: v.p.production.ID}
	live, running, err := deploy.Current(ctx, v.p.d.pool, svc.ID, v.p.production.ID,
		serviceAddresses(v.p.production, svc))
	if err != nil {
		return summary, false, err
	}
	if running && live.ReleaseID != "" {
		rel, err := release.Get(ctx, v.p.d.pool, live.ReleaseID)
		if err != nil {
			return summary, false, err
		}
		summary.CurrentRelease = rel.Number
	}
	newest, found, err := newestWindow(ctx, v.p.d.pool, svc.ID)
	if err != nil {
		return summary, false, err
	}
	switch {
	case unmeasuredFields(svc) != "":
		summary.Health = "unmeasured"
	case found && !newest.PassedAvailable:
		summary.Health = "watched with no way to reach passed"
	default:
		summary.Health = "measured"
	}
	return summary, true, nil
}

// ServiceOn is one service's own view on one environment: what is running per
// target, what it publishes, its health, its incidents, the rollouts in
// progress, every last check record of it whatever its age, and what it is
// actually being watched for.
func (v *views) ServiceOn(ctx context.Context, _ principal.Principal, serviceID, environmentID string) (screens.Service, error) {
	svc, err := service.Get(ctx, v.p.d.pool, serviceID)
	if err != nil {
		return screens.Service{}, fmt.Errorf("%w: %s", screens.ErrNotFound, serviceID)
	}
	view := screens.Service{
		ServiceID: svc.ID, EnvironmentID: environmentID,
		Unmeasured: unmeasuredFields(svc) != "",
	}

	env, err := environment.Get(ctx, v.p.d.pool, environmentID)
	if err != nil {
		return screens.Service{}, fmt.Errorf("%w: %s", screens.ErrNotFound, environmentID)
	}
	deploys, err := deploy.ForEnvironment(ctx, v.p.d.pool, environmentID)
	if err != nil {
		return screens.Service{}, err
	}
	var started []deploy.Deploy
	for _, one := range deploys {
		if one.ServiceID != svc.ID || one.Status != deploy.StatusStarted {
			continue
		}
		started = append(started, one)
		rollout, err := v.rolloutOf(ctx, one)
		if err != nil {
			return screens.Service{}, err
		}
		view.Rollouts = append(view.Rollouts, rollout)
	}
	if view.Targets, err = v.targetsOf(ctx, svc, env, started); err != nil {
		return screens.Service{}, err
	}

	published, err := contract.OfService(ctx, v.p.d.pool, svc.ID)
	if err != nil {
		return screens.Service{}, err
	}
	for _, one := range published {
		view.ContractsPublished = append(view.ContractsPublished, one.Name)
	}

	incidents, err := incident.ForService(ctx, v.p.d.pool, svc.ID)
	if err != nil {
		return screens.Service{}, err
	}
	for _, one := range incidents {
		if !one.Open() {
			continue
		}
		view.OpenIncidents = append(view.OpenIncidents,
			screens.Incident{ID: one.ID, Quantity: one.Quantity, OpenedAt: one.At})
	}

	checks, err := lastcheck.All(ctx, v.p.d.pool)
	if err != nil {
		return screens.Service{}, err
	}
	for _, one := range checks {
		if one.Subject != svc.ID && one.Subject != svc.Name {
			continue
		}
		view.LastChecks = append(view.LastChecks, screens.LastCheck{
			Component: one.Component, Checks: one.Subject, LastPass: one.CheckedAt,
			IntervalSeconds: int64(one.Interval / time.Second),
			FurtherPassOwed: one.FurtherPassOwed(),
		})
	}

	if view.Windows, view.EmissionVersion, err = v.watchedFor(ctx, svc); err != nil {
		return screens.Service{}, err
	}
	if view.DriftMismatch, err = v.mismatchOn(ctx, svc.ID); err != nil {
		return screens.Service{}, err
	}
	if view.Mitigation, err = v.mitigationOn(ctx, svc.ID, environmentID); err != nil {
		return screens.Service{}, err
	}
	return view, nil
}

// targetsOf is one row per target of the environment the service runs on:
// which release is running there now, the deploy that release came from, and
// how far a deploy still in progress has reached on that target. A row per
// completed deploy would be the history of the target and not what is running
// on it, and what the design asks this screen for is the second — a rollout
// stalled on one target is two targets running two releases.
//
// started is the deploys of this service still in progress on this
// environment, read once by the caller because it lists the rollouts from the
// same set.
func (v *views) targetsOf(ctx context.Context, svc service.Service, env environment.Environment,
	started []deploy.Deploy) ([]screens.TargetRelease, error) {
	targets := serviceTargets(env, svc)
	rows := make([]screens.TargetRelease, 0, len(targets))
	for _, target := range targets {
		row := screens.TargetRelease{TargetID: target.Address}
		running, found, err := deploy.CurrentOnTarget(ctx, v.p.d.pool, svc.ID, env.ID, target.Address)
		if err != nil {
			return nil, err
		}
		if found {
			rel, err := release.Get(ctx, v.p.d.pool, running.ReleaseID)
			if err != nil {
				return nil, err
			}
			row.ReleaseNumber, row.DeployID = rel.Number, running.ID
			if row.CompletedAt, err = completionOn(ctx, v.p.d.pool, running.ID, target.Address); err != nil {
				return nil, err
			}
		}
		if row.Deploying, err = v.deployingOn(ctx, started, target.Address); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// completionOn is when one deploy completed on one target, read off that
// deploy's own target row.
func completionOn(ctx context.Context, pool *pgxpool.Pool, deployID, address string) (string, error) {
	targets, err := deploy.Targets(ctx, pool, deployID)
	if err != nil {
		return "", err
	}
	for _, target := range targets {
		if target.Address == address {
			return target.CompleteAt, nil
		}
	}
	return "", nil
}

// deployingOn is how far the deploy in progress on one target has reached, and
// empty where none is. Where more than one is in progress it is the newest by
// number, which is the order the deployer wrote them in.
func (v *views) deployingOn(ctx context.Context, started []deploy.Deploy, address string) (string, error) {
	completion := ""
	number := int64(-1)
	for _, one := range started {
		targets, err := deploy.Targets(ctx, v.p.d.pool, one.ID)
		if err != nil {
			return "", err
		}
		for _, target := range targets {
			if target.Address != address || one.Number <= number {
				continue
			}
			completion, number = string(target.Completion), one.Number
		}
	}
	return completion, nil
}

// rolloutOf is one rollout in progress: the release it is rolling out and the
// control target it is measured against, where the strategy keeps one.
func (v *views) rolloutOf(ctx context.Context, one deploy.Deploy) (screens.Rollout, error) {
	rollout := screens.Rollout{}
	if one.ReleaseID != "" {
		rel, err := release.Get(ctx, v.p.d.pool, one.ReleaseID)
		if err != nil {
			return rollout, err
		}
		rollout.ReleaseNumber = rel.Number
	}
	targets, err := deploy.Targets(ctx, v.p.d.pool, one.ID)
	if err != nil {
		return rollout, err
	}
	for _, target := range targets {
		if target.ControlReleaseID != "" {
			rollout.ControlTargetID = target.Address
		}
	}
	return rollout, nil
}

// watchedFor is what the service is actually being watched for, per quantity:
// the size in force, the finest size its traffic reaches, the confidence and
// power that reading was taken at, whether passed is reachable at all, and the
// run length in force with the crossings it admits where nothing changed. The
// emission version its newest record carries comes back beside it.
//
// Whether passed is reachable is the newest window's own field: the health
// monitor decides it at the open, from the traffic and the power in force, and
// writes it onto the record — so a screen reads the reading rather than
// recomputing one over traffic the window was not opened on.
func (v *views) watchedFor(ctx context.Context, svc service.Service) ([]screens.WindowParameters, string, error) {
	parameters, err := v.p.policy.WindowParameters(ctx, svc.ID)
	if err != nil {
		return nil, "", err
	}
	newest, found, err := newestWindow(ctx, v.p.d.pool, svc.ID)
	if err != nil {
		return nil, "", err
	}
	rows := make([]screens.WindowParameters, 0, len(gatepolicy.Quantities))
	for _, quantity := range gatepolicy.Quantities {
		row := screens.WindowParameters{
			Quantity:   string(quantity),
			Size:       int64(parameters.Size[quantity].Number),
			Confidence: parameters.Confidence.Number,
			Power:      parameters.Power[quantity].Number,
		}
		if found {
			row.FinestSizeReached = int64(newest.FinestSizeReached[quantity])
			row.PassedReachable = newest.PassedAvailable
			row.AverageRunLength = newest.OwnHistoryRunLength
			// A reading with a run length admits one crossing per that many
			// intervals on a service where nothing changed, which is the whole
			// rate at which its releases can be rolled back for no reason.
			if newest.OwnHistoryRunLength > 0 {
				row.AdmittedCrossings = 1 / newest.OwnHistoryRunLength
			}
		}
		rows = append(rows, row)
	}
	emission := ""
	if found {
		emission = newest.EmissionVersionRelease
	}
	return rows, emission, nil
}

// mismatchOn is what the drift detector's own record over this service names,
// and empty where no drift detector is installed or it found none.
func (v *views) mismatchOn(ctx context.Context, serviceID string) (string, error) {
	if v.p.d.driftdetector == nil {
		return "", nil
	}
	found, err := driftdetector.Uncleared(ctx, v.p.d.driftdetector, serviceID)
	if err != nil {
		return "", err
	}
	if len(found) == 0 {
		return "", nil
	}
	return found[0].Why(), nil
}

// mitigationOn is the mitigation standing on a target of this service on this
// environment, shown for as long as it stands, and nil where none does.
func (v *views) mitigationOn(ctx context.Context, serviceID, environmentID string) (*screens.Mitigation, error) {
	standing, err := deploy.StandingMitigations(ctx, v.p.d.pool)
	if err != nil {
		return nil, err
	}
	for _, one := range standing {
		on, err := deploy.Get(ctx, v.p.d.pool, one.DeployID)
		if err != nil {
			return nil, err
		}
		if on.ServiceID != serviceID || on.EnvironmentID != environmentID {
			continue
		}
		hours := 0.0
		if began, err := record.ParseTime(one.BeganAt); err == nil {
			hours = time.Since(began).Hours()
		}
		return &screens.Mitigation{
			ID: one.ID, TargetID: one.Address,
			Operation: string(one.Operation), StandingHours: hours,
		}, nil
	}
	return nil, nil
}

// newestWindow is the service's newest window, whether open or closed: what the
// service is being watched for is read off the reading it was last opened
// against, and a service with no window has never been watched.
func newestWindow(ctx context.Context, pool *pgxpool.Pool, serviceID string) (window.Window, bool, error) {
	all, err := window.All(ctx, pool, serviceID)
	if err != nil || len(all) == 0 {
		return window.Window{}, false, err
	}
	return all[len(all)-1], true, nil
}

// unmeasuredFields is which of the four fields the deployer populates the
// service is missing, in words, and empty where it is missing none. The four
// are the same reading the production deploy row's own open event carries; the
// words are repeated rather than shared, package gate exporting none, and the
// four field names are what a search finds in both.
func unmeasuredFields(svc service.Service) string {
	var missing []string
	for _, field := range []struct {
		found bool
		what  string
	}{
		{svc.Reachability.TargetReached, "a target the deployer reaches"},
		{svc.Reachability.InstancesReplaceable, "instances the platform can replace"},
		{svc.Reachability.RollbackPathPresent, "a rollback path"},
		{svc.Reachability.EmissionReadable, "an emission the health monitor can read"},
	} {
		if !field.found {
			missing = append(missing, field.what)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return "the service is missing " + strings.Join(missing, ", ")
}

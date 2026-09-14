package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// The deployer's side of the health monitor: what reaching a deploy target
// takes, which is the deployer's work and not the health monitor's. It is
// [healthmonitor.Deployer], the arrangement the merge queue already has for
// everything it needs done to a repository.

// StartControl is already performed by the production deploy's target reach.
// The health monitor calls this after the deploy has recorded the control on
// each reached target; this local composition has no second target operation.
func (p *path) StartControl(context.Context, healthmonitor.Control) error { return nil }

// TearDownControl ends the local control process at the window exit.
func (p *path) TearDownControl(ctx context.Context, c healthmonitor.Control) error {
	target := p.d.targets.at(c.Target)
	if local, ok := target.(*localtarget.Local); ok {
		if err := local.StopControl(ctx, deployerPrincipal, c.ServiceName); err != nil {
			return err
		}
	} else if err := target.ShiftTraffic(ctx, deployerPrincipal, targetseam.Shift{
		Service: c.ServiceName, Build: c.BuildID, Share: 1, Credential: p.d.credential,
	}); err != nil {
		return err
	}
	return p.tearDownControlFleet(ctx, c)
}

// TearDownKept ends the local kept process after the last window that could
// return to it closes.
func (p *path) TearDownKept(ctx context.Context, k healthmonitor.Kept) error {
	target := p.d.targets.at(k.Target)
	if local, ok := target.(*localtarget.Local); ok {
		if err := local.StopKept(ctx, deployerPrincipal, k.ServiceName); err != nil {
			return err
		}
	}
	return p.tearDownKeptFleet(ctx, k)
}

func (p *path) tearDownControlFleet(ctx context.Context, c healthmonitor.Control) error {
	dep, target, err := p.fleetTarget(ctx, c.DeployID, c.Target)
	if err != nil || target.Fleets.Control.Instances == 0 || target.Fleets.Control.TornDownAt != "" {
		return err
	}
	hours, err := deploy.Hours(dep.At, record.Now(), target.Fleets.Control.Instances)
	if err != nil {
		return err
	}
	svc, err := p.serviceOf(ctx, c.ServiceID)
	if err != nil {
		return err
	}
	return p.deploys.TearDownControl(ctx, c.DeployID, c.Target, hours, instanceHourRate(svc))
}

func (p *path) tearDownKeptFleet(ctx context.Context, k healthmonitor.Kept) error {
	dep, target, err := p.fleetTarget(ctx, k.DeployID, k.Target)
	if err != nil || target.Fleets.Kept.Instances == 0 || target.Fleets.Kept.TornDownAt != "" {
		return err
	}
	hours, err := deploy.Hours(dep.At, record.Now(), target.Fleets.Kept.Instances)
	if err != nil {
		return err
	}
	svc, err := p.serviceOf(ctx, k.ServiceID)
	if err != nil {
		return err
	}
	return p.deploys.TearDownKept(ctx, k.DeployID, k.Target, hours, instanceHourRate(svc))
}

func (p *path) fleetTarget(ctx context.Context, deployID, address string) (deploy.Deploy, deploy.Target, error) {
	dep, err := deploy.Get(ctx, p.d.pool, deployID)
	if err != nil {
		return deploy.Deploy{}, deploy.Target{}, err
	}
	targets, err := deploy.Targets(ctx, p.d.pool, deployID)
	if err != nil {
		return deploy.Deploy{}, deploy.Target{}, err
	}
	for _, target := range targets {
		if target.Address == address {
			return dep, target, nil
		}
	}
	return deploy.Deploy{}, deploy.Target{}, fmt.Errorf("factory: deploy %s has no target %s", deployID, address)
}

// RollBack returns traffic to the kept fleet where the failed deploy left one;
// otherwise it restores the recorded build onto the targets.
func (p *path) RollBack(ctx context.Context, r healthmonitor.Rollback) error {
	svc, err := p.serviceOf(ctx, r.ServiceID)
	if err != nil {
		return err
	}
	addresses := serviceAddresses(p.production, svc)
	if len(addresses) == 0 {
		return fmt.Errorf("factory: %s runs on no target of production, and a rollback has no artifact to verify", svc.Name)
	}
	undone, err := p.undoneBy(ctx, r)
	if err != nil {
		return err
	}
	performance := deploy.Performance{
		Actor:              deployActor,
		Principal:          deployerPrincipal,
		ServiceID:          r.ServiceID,
		ServiceName:        r.ServiceName,
		EnvironmentID:      r.EnvironmentID,
		What:               deploy.OfRelease(r.ToReleaseID, r.ToBuildID),
		InstanceHourRate:   instanceHourRate(svc),
		IntoProduction:     true,
		StrategyPicked:     deploy.StrategyWithoutControl,
		Credential:         p.d.credential,
		WayInAddress:       p.d.wayInAddress,
		Reaches:            p.reaches(p.production, svc),
		EnvironmentTargets: environmentTargets(p.production),
		UndoneDeployIDs:    undone,
	}
	dep, returned, err := p.shiftBack(ctx, r, performance)
	if err != nil {
		return err
	}
	if !returned {
		made, err := build.Get(ctx, p.d.pool, r.ToBuildID)
		if err != nil {
			return err
		}
		dep, err = deploy.Restore(ctx, p.deploys, deploy.Restoration{
			Performance: performance,
			Undoing: deploy.Undoing{
				FailedReleaseID:   r.FailedReleaseID,
				SkippedReleaseIDs: r.SkippedReleaseIDs,
				Source:            r.Source,
			},
			RecordedDigest:      made.ArtifactDigest,
			Artifacts:           artifactsOf{dir: addresses[0]},
			ConfigurationSource: p,
		})
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(p.d.out, "Rollback %s complete: build %s of release %s is back on the target\n",
		dep.ID, r.ToBuildID, r.ToReleaseID)
	fmt.Fprintf(p.d.out, "  it failed release %s and skipped %d above it; source: %s\n",
		r.FailedReleaseID, len(r.SkippedReleaseIDs), r.Source)
	fmt.Fprintln(p.d.out, "  every production deploy of this service holds until the revert the crossing raised ships")

	// The rollback is reported and not asked for: it has already happened, so
	// mail and chat carry it and the page channel does not — the factory does
	// not page to inform. [notifier.KindRollbackPerformed] is the kind that says
	// so, and the notifier refuses a caller that claims anything else about it.
	if p.notifier == nil {
		return nil
	}
	_, err = p.notifier.Notify(ctx, notifier.Wait{
		Row:  dep.ID,
		Kind: notifier.KindRollbackPerformed,
		Waiting: fmt.Sprintf("%s was rolled back to release %s: the window over release %s crossed and the deployer put the earlier build back",
			r.ServiceName, r.ToReleaseID, r.FailedReleaseID),
		Holding:   people.OfDuty(takeOverIssues),
		ServiceID: r.ServiceID,
	})
	return err
}

func (p *path) shiftBack(ctx context.Context, r healthmonitor.Rollback, performance deploy.Performance) (deploy.Deploy, bool, error) {
	kept, err := deploy.ByRelease(ctx, p.d.pool, r.EnvironmentID, r.FailedReleaseID)
	if err != nil {
		return deploy.Deploy{}, false, err
	}
	undoing := deploy.Undoing{
		FailedReleaseID:   r.FailedReleaseID,
		SkippedReleaseIDs: r.SkippedReleaseIDs,
		Source:            r.Source,
	}
	for n := len(kept) - 1; n >= 0; n-- {
		dep, err := deploy.ShiftBack(ctx, p.deploys, deploy.Returning{
			Performance:         performance,
			Undoing:             undoing,
			KeptBy:              kept[n].ID,
			ConfigurationSource: p,
		})
		if err == nil {
			return dep, true, nil
		}
		if !errors.Is(err, deploy.ErrNothingKeptToReturnTo) {
			return dep, false, err
		}
	}
	return deploy.Deploy{}, false, nil
}

// Configuration resolves the authored value set whose content digest the
// returned-to deploy record names.
func (p *path) Configuration(ctx context.Context, serviceID, digest string) (targetseam.ValueSet, error) {
	versions, err := service.ValueSetVersions(ctx, p.d.pool, serviceID)
	if err != nil {
		return targetseam.ValueSet{}, err
	}
	for _, version := range versions {
		if version.Digest == digest {
			values, _, err := deploy.ResolveValueSet(version.Content, p.d.secrets)
			return values, err
		}
	}
	return targetseam.ValueSet{}, fmt.Errorf("factory: no value set has configuration digest %s", digest)
}

// deleteExpiredSnapshots is the deployer's own pass over the copies its deploys
// took: for every service this install knows, the records naming a copy older
// than the snapshot retention the service record authors have that copy deleted
// through the seam and the deletion written on the record. A service that
// authored no retention keeps its copies — a retention nobody authored is not a
// retention of no time at all — and an owner's call from Ops deletes one
// earlier, through the same [deploy.DeleteSnapshot] this pass performs.
//
// The copies are reached through the first production target the service runs
// on: the store is one per service per environment, so every target of the
// environment reaches the same store and the same copies.
func (p *path) deleteExpiredSnapshots(ctx context.Context) (bool, error) {
	moved := false
	for _, name := range p.d.serviceNames() {
		svc, found, err := service.ByName(ctx, p.d.pool, name)
		if err != nil {
			return moved, err
		}
		if !found || !svc.SnapshotRetentionSeconds.Present {
			continue
		}
		targets := serviceTargets(p.production, svc)
		if len(targets) == 0 {
			continue
		}
		deleted, err := deploy.DeleteExpiredSnapshots(ctx, p.deploys, deploy.Pass{
			Principal:   deployerPrincipal,
			ServiceID:   svc.ID,
			ServiceName: svc.Name,
			Retention:   time.Duration(svc.SnapshotRetentionSeconds.Number * float64(time.Second)),
			Target:      p.d.targets.at(targets[0].Address),
			Credential:  p.d.credential,
		})
		if err != nil {
			return moved, err
		}
		for _, id := range deleted {
			moved = true
			fmt.Fprintf(p.d.out, "The deployer deleted the snapshot deploy %s named, at the end of %s's retention\n",
				id, svc.Name)
		}
	}
	return moved, nil
}

// DeploySearch is refused: the search is not built. Package healthmonitor wires
// the builder and never calls the search, so nothing reaches this, and a
// deploy performed here would put a build that passed no gate in front of
// production traffic on a path nothing bounds.
func (p *path) DeploySearch(context.Context, healthmonitor.SearchDeploy) (string, error) {
	return "", errors.New("factory: the search that deploys a build nothing has watched is not built")
}

// EndSearchDeploy ends nothing, there being no search deploy to end:
// [path.DeploySearch] refuses every one, so no window over such a deploy is
// ever opened here. It answers rather than refusing, for the reason
// [path.TearDownControl] does — the health monitor asks at the close of a
// window it is about to write, and a refusal there would stop that close.
func (p *path) EndSearchDeploy(context.Context, healthmonitor.SearchDeployEnding) error { return nil }

// pageRollbackNotComplete is the fifth page condition, read on the same pass
// that reads the windows: a rollback the health monitor called for whose deploy
// record is still not complete on every target at the deployer's next last
// check for that environment. Production serves a release the factory has
// already failed and the mechanism that would remove it did not finish.
//
// The condition and the page events on it are package healthmonitor's, which is
// where the two records it is read from are already read; this reports what
// went out.
func (p *path) pageRollbackNotComplete(ctx context.Context, w healthmonitor.Watching) (bool, error) {
	paged, err := p.healthMonitor.PageRollbackNotComplete(ctx, w)
	if err != nil || paged == "" {
		return false, err
	}
	fmt.Fprintf(p.d.out, "Rollback %s is not complete on every target and the deployer has passed since, and the page went out: production runs a release the factory failed\n", paged)
	return true, nil
}

// undoneBy is every deploy this rollback undoes: the failed release's own
// deploys and those of every release it skipped. There is more than one per
// release where a release was deployed, held, and deployed again, and each is
// advanced to rolled back as the rollback completes on each target.
func (p *path) undoneBy(ctx context.Context, r healthmonitor.Rollback) ([]string, error) {
	var undone []string
	for _, releaseID := range append([]string{r.FailedReleaseID}, r.SkippedReleaseIDs...) {
		deploys, err := deploy.ByRelease(ctx, p.d.pool, r.EnvironmentID, releaseID)
		if err != nil {
			return nil, err
		}
		for _, one := range deploys {
			undone = append(undone, one.ID)
		}
	}
	return undone, nil
}

// artifactsOf is [deploy.Artifacts]: the digest of the artifact a build
// produced, read off the disk and computed fresh before a rollback puts that
// build back on a target — which is what the verification buys: a build whose
// bytes changed under the record is caught here rather than restored as a name.
//
// dir is one target's directory — this platform's address — and one is enough:
// buildInto compiles the binary once and copyFile puts the identical bytes in
// every further target a rollout reaches, so what one target holds is what
// every other one does.
type artifactsOf struct{ dir string }

// Digest reads the build's own binary at dir/<build id> and returns its
// sha256, in the "sha256:" form the build record's own digest carries.
func (a artifactsOf) Digest(_ context.Context, buildID string) (string, error) {
	content, err := os.ReadFile(filepath.Join(a.dir, buildID))
	if err != nil {
		return "", fmt.Errorf("factory: reading the artifact of build %s at %s: %w", buildID, a.dir, err)
	}
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
)

// The deployer's side of the health monitor: what reaching a deploy target
// takes, which is the deployer's work and not the health monitor's. It is
// [healthmonitor.Deployer], the arrangement the merge queue already has for
// everything it needs done to a repository.

// StartControl is refused on this platform. A control is a set of instances
// taking comparable traffic beside the release, and this platform moves a
// process rather than traffic: it serves no share and runs one instance, so
// there is nowhere for a second set to take traffic from.
//
// It is refused rather than returning as though it had acted, for the reason
// [localtarget] refuses a traffic shift: a control reported as started would be
// a window recorded as having compared two builds while one of them served no
// request. The score never asks for one here — the environment record declares
// that no target serves a share, so the row without a control is what is picked
// — and this is what says so if that ever changes.
func (p *path) StartControl(context.Context, healthmonitor.Control) error {
	return errors.New("factory: this platform serves no share, so no control can run beside a release")
}

// TearDownControl ends nothing, there being no control to end: nothing on this
// platform starts one. It answers rather than refusing, because the health
// monitor tears controls down at the passed and the timed-out exits on every
// window it closes, and a refusal there would stop a window from closing over a
// deploy that never had one.
func (p *path) TearDownControl(context.Context, healthmonitor.Control) error { return nil }

// TearDownKept ends nothing either, there being no kept fleet on this platform:
// [path.reaches] keeps no instances, because this platform moves a process
// rather than traffic and a rollback here is a redeploy of a binary still on
// disk. It answers rather than refusing, for the reason [path.TearDownControl]
// does: the health monitor asks at the close of the last window that could
// return to a release, and a refusal there would stop that close.
func (p *path) TearDownKept(context.Context, healthmonitor.Kept) error { return nil }

// RollBack is the slow rollback the health monitor called for: the build of the
// release it returns to put back on production's targets, the rollback's own
// deploy record written, and the deploys it undid advanced to rolled back.
//
// It is the deployer's because reaching a deploy target is, and the health
// monitor reaches none. The target's build is already in production's directory,
// put there by the deploy that shipped it and never removed — so restoring it is
// a deploy of a binary that is still on disk, and there is nothing to rebuild.
// What that costs is that a directory pruned between the deploy and the rollback
// would leave the rollback with nothing to put back, which nothing here prunes.
func (p *path) RollBack(ctx context.Context, r healthmonitor.Rollback) error {
	made, err := build.Get(ctx, p.d.pool, r.ToBuildID)
	if err != nil {
		return err
	}
	// The rollback reaches the targets the service runs on, which is the set the
	// deploy it undoes reached.
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
	dep, err := deploy.Restore(ctx, p.deploys, deploy.Restoration{
		Performance: deploy.Performance{
			Actor:           deployActor,
			Principal:       deployerPrincipal,
			ServiceID:       r.ServiceID,
			ServiceName:     r.ServiceName,
			EnvironmentID:   r.EnvironmentID,
			What:            deploy.OfRelease(r.ToReleaseID, r.ToBuildID),
			IntoProduction:  true,
			StrategyPicked:  deploy.StrategyWithoutControl,
			Credential:      p.d.credential,
			Reaches:         p.reaches(p.production, svc),
			UndoneDeployIDs: undone,
		},
		Undoing: deploy.Undoing{
			FailedReleaseID:   r.FailedReleaseID,
			SkippedReleaseIDs: r.SkippedReleaseIDs,
			Source:            r.Source,
		},
		RecordedDigest: made.ArtifactDigest,
		Artifacts:      artifactsOf{dir: addresses[0]},
	})
	if err != nil {
		return err
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

// DeploySearch is refused: the search is not built. Package healthmonitor wires
// the builder and never calls the search, so nothing reaches this, and a
// deploy performed here would put a build that passed no gate in front of
// production traffic on a path nothing bounds.
func (p *path) DeploySearch(context.Context, healthmonitor.SearchDeploy) (string, error) {
	return "", errors.New("factory: the search that deploys a build nothing has watched is not built")
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

package deploy

import (
	"context"
	"errors"
	"fmt"
)

// Artifacts is where the artifact a build produced is read from, so that its
// digest can be computed before a rollback puts that build back on a target.
// The caller implements it: the artifact host is outside the factory's recovery
// unit and this package reaches nothing.
type Artifacts interface {
	// Digest is the digest of the artifact the build produced, computed over the
	// content the host holds now and not read off a record.
	Digest(ctx context.Context, buildID string) (string, error)
}

// ErrDigestDiffers is returned by [Restore] where the artifact the build names
// no longer digests to what the build recorded. Redeploying by name alone
// restores a name and not the bytes it was verified under, so the deployer
// shifts no traffic, marks the rollback's record failed at that step, and the
// failed release keeps serving. It pages at that exit: production is running a
// release the factory has just failed, and nothing the factory has will improve
// it.
var ErrDigestDiffers = errors.New("deploy: the artifact no longer digests to what the build recorded")

// Restoration is the slow rollback: a deploy of the release being returned to,
// naming what it failed, what it skipped, and the source that called for it,
// with the digest verified before anything is put anywhere.
type Restoration struct {
	// Performance is the deploy this rollback is. Its What is the release being
	// returned to and that release's build, and its Configuration is the value
	// set that release ran under — a rollback restores the configuration version
	// the deploy record named for that release beside its code.
	Performance
	// Undoing is the release this rollback failed, the ones it skipped, and the
	// source that called for it.
	Undoing Undoing
	// RecordedDigest is the artifact digest the build record holds, which is what
	// the artifact host's content is verified against.
	RecordedDigest string
	// Artifacts is what the digest is computed through, and is required: a
	// rollback that verified nothing would restore a name and not the bytes.
	Artifacts Artifacts
	// UndoneDeployIDs are the deploys this rollback undoes — the failed
	// release's own and those of every release it skipped — whose targets are
	// advanced to rolled back as this rollback completes on each.
	UndoneDeployIDs []string
}

// Restore is the slow rollback: the build of the release being returned to put
// back on the targets and waited for. It verifies the artifact's digest first,
// writes the rollback's deploy record, performs the deploy the way any other is
// performed, and then advances the deploys it undoes to rolled back, target by
// target.
//
// Slow is the design's own word for it, and it is what a rollout that kept no
// control leaves: with a control the release a rollback returns to is still
// running at full capacity and the rollback is a traffic shift onto it. Here the
// build has to be started from cold, and what that costs is the time between the
// crossing and the restored build serving, during which production is running
// the failed release.
//
// The order is [Perform]'s and for the same reason. The rollback's own record is
// completed before anything is marked undone: a store that said a release was
// rolled back with nothing put back in its place would describe a service
// running nothing.
func Restore(ctx context.Context, w *Writer, r Restoration) (Deploy, error) {
	if r.What.ReleaseID == "" {
		return Deploy{}, fmt.Errorf("%w: a rollback returns to a numbered release", ErrUndoingIncomplete)
	}
	if r.Artifacts == nil || r.RecordedDigest == "" {
		return Deploy{}, fmt.Errorf("%w: the build's recorded digest is what it is verified against",
			ErrDigestDiffers)
	}

	token, digest, err := mintWayInToken()
	if err != nil {
		return Deploy{}, err
	}
	d, err := w.StartUndoing(ctx, r.Actor, r.beginning(digest), r.Undoing)
	if err != nil {
		return Deploy{}, err
	}

	// The digest is verified before anything is deployed, and the record exists
	// before the verification, so a rollback refused at this step is a record
	// standing for Ops rather than a refusal with nothing behind it.
	found, err := r.Artifacts.Digest(ctx, r.What.BuildID)
	if err != nil {
		return d, fail(ctx, w, r.Performance, d, StepArtifactDigest,
			fmt.Errorf("%w: reading the artifact of %s: %w", ErrDigestDiffers, r.What.BuildID, err))
	}
	if found != r.RecordedDigest {
		return d, fail(ctx, w, r.Performance, d, StepArtifactDigest,
			fmt.Errorf("%w: build %s holds %s, the record says %s",
				ErrDigestDiffers, r.What.BuildID, found, r.RecordedDigest))
	}

	d, err = perform(ctx, w, r.Performance, d, token)
	if err != nil {
		return d, err
	}

	// Every release this rollback undid, failed first. The failed release and
	// the skipped ones are the same write with different reasons, which is why
	// the two are kept apart on the record and treated alike here. Each target
	// is advanced as this rollback completed on it, a target the release never
	// reached included.
	for _, undone := range r.UndoneDeployIDs {
		if undone == d.ID {
			continue
		}
		if err := w.Undo(ctx, undone); err != nil {
			return d, err
		}
	}
	return d, nil
}

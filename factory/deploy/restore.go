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
}

// Restore is the slow rollback: the build of the release being returned to put
// back on the targets and waited for. It verifies the artifact's digest first,
// writes the rollback's deploy record, and performs the deploy the way any other
// is performed — which is what advances the deploys it undoes, one target at a
// time, as this rollback completes on each. Which deploys those are is
// [Performance.UndoneDeployIDs]: the failed release's own and those of every
// release the same rollback skipped.
//
// Slow is the design's own word for it, and it is what a rollout that kept no
// control leaves: with a control the release a rollback returns to is still
// running at full capacity and the rollback is a traffic shift onto it. Here the
// build has to be started from cold, and what that costs is the time between the
// crossing and the restored build serving, during which production is running
// the failed release.
//
// The order is [Perform]'s and for the same reason: on each target the build is
// put back and that target marked complete before the deploys it undoes are
// advanced on that target, so a store never says a release was rolled back on a
// target with nothing put back in its place.
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

	// Every release this rollback undoes is advanced inside the walk. The failed
	// release and the skipped ones are the same write with different reasons,
	// which is why the two are kept apart on the record and treated alike there.
	return perform(ctx, w, r.Performance, d, token)
}

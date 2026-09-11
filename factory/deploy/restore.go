package deploy

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/targetseam"
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

// ErrSchemaChangeAtARollback is returned by [Restore] for a [Restoration]
// carrying a schema change. A rollback applies none: the schema moves only
// forward, staying at the newest release's form however far traffic moves back,
// which is what the store's forward promise exists to make survivable. The
// refusal is here rather than left to the caller's discipline, [Restoration]
// embedding the whole of [Performance] and [Performance.SchemaChanges] being one
// of its fields.
var ErrSchemaChangeAtARollback = errors.New("deploy: a rollback applies no schema change — the schema moves only forward")

// ErrNothingKeptToReturnTo is returned by [ShiftBack] where a target of the
// rollback keeps no instances of the release it returns to, or has torn them
// down. The fast rollback shifts traffic onto instances that are already
// running, so a target with none is one [Restore] has to redeploy on.
var ErrNothingKeptToReturnTo = errors.New("deploy: the target keeps no instances of the release the rollback returns to")

// Returning is the fast rollback: the traffic of every target moved onto the
// instances of the release being returned to, which the deploy that replaced it
// kept running at the capacity that release had.
type Returning struct {
	// Performance is the deploy this rollback is. Its What is the release being
	// returned to and that release's build, and its SchemaChanges is empty on
	// every rollback.
	Performance
	// Undoing is the release this rollback failed, the ones it skipped, and the
	// source that called for it.
	Undoing Undoing
	// KeptBy is the deploy record that keeps the instances the traffic returns
	// to: the deploy that replaced the release being returned to. Its row per
	// target says how many are kept there and whether they have been torn down,
	// which is the only fact about the rollback path that exists before the
	// rollback does.
	KeptBy string
}

// ShiftBack is the fast rollback: a rollback is a deploy event and not a
// version event, so this shifts traffic onto the instances of the release it
// returns to — still running at full capacity, because the deploy that replaced
// that release kept them — and writes a deploy record, minting and retiring no
// number.
//
// It puts nothing on a target. The build is already running there, so there is
// no artifact to verify and nothing to start from cold, which is what makes this
// the fast way and [Restore] the slow one; and it applies no schema change, the
// schema moving only forward however far traffic moves back.
//
// It is a deploy in its own right, and mints a fresh way-in token the way every
// deploy does: the configuration digest is taken over the resolved set alone,
// before the token is minted and appended, so it digests the same as the
// deploy that placed and kept these instances where the configuration itself
// has not changed — but the way-in token digest the record carries is this
// rollback's own, never [r.KeptBy]'s. [Target.Reconfigure] is what hands the
// kept instances that fresh token, in place of the [Target.Deploy] a build
// still cold would need: a platform unable to do that without dropping a
// request refuses with [targetseam.ErrCannotReconfigure], and the rollback
// goes no further here — the caller falls back to [Restore], the slow way.
//
// Per target, in the environment's order, it hands the kept instances the
// fresh configuration, shifts all of the traffic onto the release returned to,
// and marks that target complete with what the reconfiguration reported,
// advancing the deploys it undoes on that target as it goes — so a rollback
// that stopped undoes nothing beyond the targets it reached. There is no hold
// between targets: what a bake volume bounds is exposure to a build nothing
// has watched, and this returns traffic to the build that was serving before.
//
// A target whose kept count is nothing, or whose kept fleet has been torn down,
// is [ErrNothingKeptToReturnTo]: there is nothing there to shift onto, and that
// target is [Restore]'s. A platform that serves no share cannot perform the
// shift at all and refuses at the seam, which is the same answer one target
// later.
func ShiftBack(ctx context.Context, w *Writer, r Returning) (Deploy, error) {
	if r.What.ReleaseID == "" {
		return Deploy{}, fmt.Errorf("%w: a rollback returns to a numbered release", ErrUndoingIncomplete)
	}
	if r.KeptBy == "" {
		return Deploy{}, fmt.Errorf("%w: the rollback names no deploy keeping them", ErrNothingKeptToReturnTo)
	}
	if len(r.SchemaChanges) > 0 {
		return Deploy{}, fmt.Errorf("%w: %d named on the rollback to %s",
			ErrSchemaChangeAtARollback, len(r.SchemaChanges), r.What.ReleaseID)
	}

	kept, err := Targets(ctx, w.Pool(), r.KeptBy)
	if err != nil {
		return Deploy{}, err
	}
	standing := make(map[string]bool, len(kept))
	for _, target := range kept {
		standing[target.Address] = target.Fleets.Kept.Instances > 0 && target.Fleets.Kept.TornDownAt == ""
	}
	for _, reach := range r.Reaches {
		if !standing[reach.Address] {
			return Deploy{}, fmt.Errorf("%w: %s of %s", ErrNothingKeptToReturnTo, reach.Address, r.KeptBy)
		}
	}

	p := r.Performance
	if err := p.check(); err != nil {
		return Deploy{}, err
	}

	// The configuration digest is taken before the token is minted and
	// appended, the same way [Perform] takes it: over the resolved set alone,
	// so it digests the same here as it did at the deploy this rollback
	// undoes where the configuration itself has not changed. The way-in token
	// is minted fresh, this being a deploy in its own right and not a call
	// that reuses [r.KeptBy]'s.
	configDigest := DigestConfiguration(p.Configuration)
	p, wayInDigest, err := p.mintingTheWayInToken()
	if err != nil {
		return Deploy{}, err
	}

	d, err := w.StartUndoing(ctx, p.Actor, p.beginning(configDigest, wayInDigest), r.Undoing)
	if err != nil {
		return Deploy{}, err
	}

	configuration := addingTheDeployID(p.Configuration, d.ID)
	for n, reach := range p.Reaches {
		if err := w.ReachTarget(ctx, d.ID, reach.Address); err != nil {
			return d, err
		}
		reconfigured, err := reach.Target.Reconfigure(ctx, p.Principal, targetseam.Reconfiguration{
			Service: p.ServiceName, Build: p.What.BuildID, Configuration: configuration,
			WayInAddress: p.WayInAddress, Credential: p.Credential,
		})
		if err != nil {
			return d, refused(ctx, w, p, d, n, reach, err)
		}
		if err := reach.Target.ShiftTraffic(ctx, p.Principal, targetseam.Shift{
			Service: p.ServiceName, Build: p.What.BuildID, Share: 1, Credential: p.Credential,
		}); err != nil {
			return d, refused(ctx, w, p, d, n, reach, err)
		}
		// What goes on the record is what the reconfiguration reported: the
		// instances the traffic now serves from are the ones already
		// running, handed the fresh configuration rather than replaced.
		if err := w.CompleteTarget(ctx, d.ID, reach.Address, reconfigured.Replacement); err != nil {
			return d, err
		}
		if p.IntoProduction && n == 0 {
			// A rollback runs no comparison: the release returned to takes all
			// of the traffic, which is the row without a control performed.
			if err := w.PerformedWithoutControl(ctx, d.ID); err != nil {
				return d, err
			}
			d.StrategyPerformed = StrategyWithoutControl
		}
		if err := undoTarget(ctx, w, p, d, reach.Address); err != nil {
			return d, err
		}
	}

	if err := w.Complete(ctx, d.ID); err != nil {
		return d, err
	}
	d.Status = StatusComplete
	return d, nil
}

// Restoration is the slow rollback: a deploy of the release being returned to,
// naming what it failed, what it skipped, and the source that called for it,
// with the digest verified before anything is put anywhere.
type Restoration struct {
	// Performance is the deploy this rollback is. Its What is the release being
	// returned to and that release's build, and its Configuration is the value
	// set that release ran under — a rollback restores the configuration version
	// the deploy record named for that release beside its code. Its SchemaChanges
	// is empty on every rollback, and [ErrSchemaChangeAtARollback] is what a
	// non-empty one is answered with.
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
// back on the targets and waited for. It applies no schema change, refusing a
// restoration that names one; it verifies the artifact's digest next,
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
	if len(r.SchemaChanges) > 0 {
		return Deploy{}, fmt.Errorf("%w: %d named on the rollback to %s",
			ErrSchemaChangeAtARollback, len(r.SchemaChanges), r.What.ReleaseID)
	}

	// The configuration digest is taken before the token is minted and
	// appended, the same way [Perform] takes it: over the resolved set alone,
	// so it digests the same here as it did at the deploy this rollback
	// undoes where the configuration itself has not changed.
	configDigest := DigestConfiguration(r.Performance.Configuration)
	p, wayInDigest, err := r.Performance.mintingTheWayInToken()
	if err != nil {
		return Deploy{}, err
	}
	if err := p.check(); err != nil {
		return Deploy{}, err
	}
	d, err := w.StartUndoing(ctx, p.Actor, p.beginning(configDigest, wayInDigest), r.Undoing)
	if err != nil {
		return Deploy{}, err
	}

	// The digest is verified before anything is deployed, and the record exists
	// before the verification, so a rollback refused at this step is a record
	// standing for Ops rather than a refusal with nothing behind it.
	found, err := r.Artifacts.Digest(ctx, r.What.BuildID)
	if err != nil {
		return d, fail(ctx, w, p, d, StepArtifactDigest,
			fmt.Errorf("%w: reading the artifact of %s: %w", ErrDigestDiffers, r.What.BuildID, err))
	}
	if found != r.RecordedDigest {
		return d, fail(ctx, w, p, d, StepArtifactDigest,
			fmt.Errorf("%w: build %s holds %s, the record says %s",
				ErrDigestDiffers, r.What.BuildID, found, r.RecordedDigest))
	}

	// Every release this rollback undoes is advanced inside the walk. The failed
	// release and the skipped ones are the same write with different reasons,
	// which is why the two are kept apart on the record and treated alike there.
	return perform(ctx, w, p, d)
}

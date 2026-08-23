package deploy

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// Restore is the slow rollback: the target's build put back on the target and
// waited for. It writes the rollback's deploy record started, puts the build of the
// release being returned to on the target through the seam, advances that record to
// complete, and then advances the deploy of the condemned release and of every
// release the rollback swept to rolled back.
//
// Slow is the design's own word for it and it is the only rollback this substrate
// has. The fast path shifts traffic onto the control of the window immediately above
// the target, which is already running that build — started, warm, and the instances
// the comparison was being made against. A target that runs a release as a local
// process keeps no control, so there is nothing to shift traffic onto and the build
// has to be started from cold. What that costs is the time between the crossing and
// the restored build serving, during which production is running the condemned
// release.
//
// The order is the one [WithoutControl] keeps and for the same reason. The
// rollback's own record is written first and completed before anything is
// marked undone: a store that said a release was rolled back with nothing put
// back in its place would describe a service running nothing. A target error
// leaves the rollback's record started and nothing marked undone, which is the
// disagreement the independent checker reads targets to raise.
func Restore(ctx context.Context, w *Writer, target targetseam.Target, actor record.Actor,
	serviceID, serviceName, environmentID string, what What, undoing Undoing,
	credential secretref.Ref) (Deploy, error) {
	if what.ReleaseID == "" {
		return Deploy{}, fmt.Errorf("%w: a rollback returns to a numbered release", ErrUndoingIncomplete)
	}

	d, err := w.StartUndoing(ctx, actor, serviceID, environmentID, what, undoing)
	if err != nil {
		return Deploy{}, err
	}

	if err := target.Deploy(ctx, targetseam.Deployment{
		Service:    serviceName,
		Build:      what.BuildID,
		Credential: credential,
	}); err != nil {
		return d, fmt.Errorf("deploy: putting build %s back on the target for %s: %w", what.BuildID, serviceName, err)
	}

	if err := w.Complete(ctx, d.ID); err != nil {
		return d, err
	}
	d.Status = StatusComplete

	// Every release this rollback undid, condemned first. The condemned release and
	// the swept ones are the same write with different reasons, which is why the two
	// are kept apart on the record and treated alike here.
	for _, releaseID := range append([]string{undoing.CondemnedReleaseID}, undoing.SweptReleaseIDs...) {
		undone, err := ByRelease(ctx, w.pool, environmentID, releaseID)
		if err != nil {
			return d, err
		}
		for _, u := range undone {
			if u.ID == d.ID || u.Status == StatusRolledBack {
				continue
			}
			if err := w.Undo(ctx, u.ID); err != nil {
				return d, err
			}
		}
	}
	return d, nil
}

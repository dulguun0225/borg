package deploy

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// Straight is the straight rollout: it writes the deploy record started, puts
// the build on the target through the seam, and advances the record to complete.
// The credential crosses as a [secretref.Ref] — a name and never a value — and
// the target names both a service id, which the record stores, and a service
// name, which the target acts on.
//
// What crosses the seam is the build and never the release: a release is the name
// a build has on master, and the target runs a binary rather than a name.
//
// On a target error the record stays started and the error returns with the
// started record: whether the target ran anything is not knowable from here,
// and the reconciler is what reads the target and raises the disagreement.
// The same holds when the target took the build and Complete then fails —
// the record says started about a build that is running until something
// completes or reconciles it.
func Straight(ctx context.Context, w *Writer, target targetseam.Target, actor record.Actor,
	serviceID, serviceName, environmentID string, what What, credential secretref.Ref) (Deploy, error) {
	d, err := w.Start(ctx, actor, serviceID, environmentID, what)
	if err != nil {
		return Deploy{}, err
	}

	if err := target.Deploy(ctx, targetseam.Deployment{
		Service:    serviceName,
		Build:      what.BuildID,
		Credential: credential,
	}); err != nil {
		return d, fmt.Errorf("deploy: putting build %s on the target for %s: %w", what.BuildID, serviceName, err)
	}

	if err := w.Complete(ctx, d.ID); err != nil {
		return d, err
	}
	d.Status = StatusComplete
	return d, nil
}

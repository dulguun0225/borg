package deploy

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// Straight is the straight rollout: it writes the deploy record started, puts
// the release on the target through the seam, and advances the record to
// complete. The credential crosses as a [secretref.Ref] — a name and never a
// value — and the target names both a service id, which the record stores,
// and a service name, which the target acts on.
//
// On a target error the record stays started and the error returns with the
// started record: whether the target ran anything is not knowable from here,
// and the reconciler that reads the target and raises the disagreement is M4.
// The same holds when the target took the release and Complete then fails —
// the record says started about a release that is running until something
// completes or reconciles it.
func Straight(ctx context.Context, w *Writer, target targetseam.Target, actor record.Actor,
	serviceID, serviceName, environment, releaseID string, credential secretref.Ref) (Deploy, error) {
	d, err := w.Start(ctx, actor, serviceID, environment, releaseID)
	if err != nil {
		return Deploy{}, err
	}

	if err := target.Deploy(ctx, targetseam.Deployment{
		Service:    serviceName,
		Release:    releaseID,
		Credential: credential,
	}); err != nil {
		return d, fmt.Errorf("deploy: putting %s on the target for %s: %w", releaseID, serviceName, err)
	}

	if err := w.Complete(ctx, d.ID); err != nil {
		return d, err
	}
	d.Status = StatusComplete
	return d, nil
}

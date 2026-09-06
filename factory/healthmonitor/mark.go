package healthmonitor

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/window"
)

// RevertOfRollback is the intent whose revert a rollback is waiting for, and the
// items decomposed from it that have not ended, and false where the revert has
// shipped or the rollback raised none. It is the walk a mark makes and the walk
// the hold makes, and it is one function because the two would otherwise be the
// same non-obvious walk written twice and able to disagree.
//
// The rollback's own deploy record names the release it failed and not the
// intent it raised: the intent is on the incident the health monitor raised at
// the same crossing, so the link between the two is the failed release.
//
// It is exported and takes the pool for the reason [Shipped] is. Two callers ask
// it: the production deploy row, computing the hold a rollback leaves, and the
// command-line interface at the mark, where a named human at Ops marks a
// rollback as not caused by the release — that mark ends the revert item, which
// is [item.Dispatch.Drop] with Ops as the caller over each id this returns, and
// lifts the hold, which is the row's own read of [window.Marked] returning no
// hold once the mark stands.
func RevertOfRollback(ctx context.Context, pool *pgxpool.Pool, environmentID string,
	rollback deploy.Deploy) (string, []string, bool, error) {
	if rollback.Undoing.FailedReleaseID == "" {
		return "", nil, false, nil
	}
	open, found, err := incident.Open(ctx, pool, rollback.ServiceID, rollback.Undoing.FailedReleaseID)
	if err != nil || !found || open.IntentID == "" {
		return "", nil, false, err
	}
	shipped, err := Shipped(ctx, pool, environmentID, open.IntentID)
	if err != nil || shipped {
		return "", nil, false, err
	}

	items, err := item.ForIntent(ctx, pool, open.IntentID)
	if err != nil {
		return "", nil, false, err
	}
	var standing []string
	for _, it := range items {
		switch it.Stage {
		case item.StageMerged, item.StageDropped, item.StageSuperseded:
			continue
		}
		standing = append(standing, it.ID)
	}
	return open.IntentID, standing, true, nil
}

// MarkStands is whether a named human at Ops has marked this rollback as not
// caused by the release. It is the read that lifts the hold a rollback leaves:
// there is no defect on master for the hold to keep off production, so the next
// release from master carries the change, opens a window of its own, and is
// measured again.
//
// It is here rather than left as [window.Marked] at the call site so that the
// hold and the mark's own command read one predicate. Nothing about the mark is
// decided here: the mark is read by everything that learns from outcomes and by
// nothing that acts.
func MarkStands(ctx context.Context, pool *pgxpool.Pool, rollbackDeployID string) (bool, error) {
	_, marked, err := window.Marked(ctx, pool, rollbackDeployID)
	return marked, err
}

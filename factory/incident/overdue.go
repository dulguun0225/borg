package incident

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// Overdue is one incident-raised item still being worked past its service's
// incident-item bound, as [OverdueItems] found it.
type Overdue struct {
	IncidentID string
	ItemID     string
	ServiceID  string
}

// stillBeingWorked is whether an item's stage is ordinary work in progress
// rather than one of the three that end it for good or the merge that ships
// it: an item already merged, dropped, escalated or superseded is not being
// worked any longer, whatever the incident that raised its intent says.
func stillBeingWorked(stage item.Stage) bool {
	switch stage {
	case item.StageMerged, item.StageDropped, item.StageEscalated, item.StageSuperseded:
		return false
	default:
		return true
	}
}

// OverdueItems is every incident-raised item still being worked past its
// service's incident-item bound, at now: an open incident that raised an
// intent, an item decomposed from that intent still being worked, raised
// longer ago than [service.IncidentItemBoundSecondsInForce] reads for the
// incident's service. It is the reader nothing called until this pass
// existed, and it is what a caller pages against, one uncleared page per item
// the way the other clocks page.
//
// It reads every incident rather than one service's, the way [All] does and
// for the same reason: the service each names is read from the incident
// itself, so asking per service would first mean being told which to ask
// about. The bound is read once per service and kept for the rest of the
// pass.
func OverdueItems(ctx context.Context, pool *pgxpool.Pool, now time.Time) ([]Overdue, error) {
	all, err := All(ctx, pool)
	if err != nil {
		return nil, err
	}

	bounds := map[string]time.Duration{}
	var overdue []Overdue
	for _, i := range all {
		if i.IntentID == "" {
			// Nothing was raised from this crossing — it is inside a window still
			// open, where what follows is a rollback and not an item.
			continue
		}
		bound, read := bounds[i.ServiceID]
		if !read {
			svc, err := service.Get(ctx, pool, i.ServiceID)
			if err != nil {
				return nil, err
			}
			seconds := service.IncidentItemBoundSecondsInForce(svc.IncidentItemBoundSeconds)
			bound = time.Duration(seconds * float64(time.Second))
			bounds[i.ServiceID] = bound
		}
		over, err := i.OverBound(now, bound)
		if err != nil {
			return nil, err
		}
		if !over {
			continue
		}
		items, err := item.ForIntent(ctx, pool, i.IntentID)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if stillBeingWorked(it.Stage) {
				overdue = append(overdue, Overdue{IncidentID: i.ID, ItemID: it.ID, ServiceID: i.ServiceID})
			}
		}
	}
	return overdue, nil
}

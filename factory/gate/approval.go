package gate

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// ApprovalTimes is when each item's decision at one row closed as an approval,
// by item id. It is here rather than in the package that asks because the shape
// of the opening payload is this package's: a caller that unmarshalled it itself
// would be a second place naming the same JSON fields, and the two could come to
// disagree — the arrangement package score already has for the fields it reads
// back off an open event. The verdict itself is read off the [decisionlog.Row]'s
// own column and not off the closing payload, which is where every other verdict
// in the design is decided too.
//
// An item with more than one approval at the row keeps the latest, an item may be
// rejected and approved again, and the queue's order is about the approval in
// force.
//
// An opening payload it cannot read is skipped rather than returned as an error,
// the way every other reader of this log treats one: a payload is unconstrained
// bytes by decisionlog's contract, so a row in a shape this package does not know
// is not a decision at this gate row.
//
// It reads the whole log through token and principal — the reader every caller
// of the log carries and the actor its own read event names — which is what the
// merge queue's order costs: the design makes that order the item's priority
// and then the time of the approval in the log, so the time is not a field of
// any record and the log is where it is. What that costs grows with the log,
// and what would remove it is an index on the log this package does not own.
func ApprovalTimes(ctx context.Context, pool *pgxpool.Pool, token lease.Token, principal record.Actor, row Row) (map[string]string, error) {
	if _, err := Actions(row); err != nil {
		return nil, err
	}
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, principal)
	if err != nil {
		return nil, err
	}

	// The item each open event of this gate row decided over, by opening id, so
	// a close event can be attributed without a second pass.
	itemOf := make(map[string]string)
	for _, r := range rows {
		if r.Shape != decisionlog.ShapeDecision || r.Part != decisionlog.PartOpen {
			continue
		}
		var opening OpeningPayload
		if err := json.Unmarshal([]byte(r.Payload), &opening); err != nil {
			// A payload is unconstrained bytes by decisionlog's contract — that
			// package neither parses one nor constrains its format — so a row this
			// package cannot read is a row some other component wrote in a shape it
			// does not know. It is not a decision at this gate row and it is not an
			// error either, which is what package score already does with one.
			continue
		}
		if Row(opening.Gate) == row && opening.ItemID != "" {
			itemOf[r.ID] = opening.ItemID
		}
	}

	approved := make(map[string]string)
	for _, r := range rows {
		if r.Shape != decisionlog.ShapeDecision || r.Part != decisionlog.PartClose {
			continue
		}
		itemID, ours := itemOf[r.Closes]
		if !ours {
			continue
		}
		if Verdict(r.Verdict) == VerdictApprove {
			approved[itemID] = r.At
		}
	}
	return approved, nil
}

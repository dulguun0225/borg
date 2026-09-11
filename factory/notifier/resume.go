package notifier

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/people"
)

// Resume is this component's restart: the delivery record it overwrites per
// waiting row. A row still waiting is delivered again and one that stopped
// waiting is not, so a stop between a row opening and its delivery landing is
// answered by the next start rather than by a human noticing nothing arrived.
//
// What it reads is its own records: one delivery per row, and the log's own
// rows saying which of them are still open. A row a closing, an abandonment
// or a wait's closing ended is skipped, and its delivery record stays as the
// account of what was sent. A row of a kind with no log opening at all — a
// drift mismatch, the drift detector's own stale check, the harm mark's cap
// row — is read as still waiting or not off its own subject instead, through
// [Notifier.stillWaitingBySubject]; driftPool is the drift detector's own
// store that reads by, or nil on an install with no detector, the way
// [Notifier.PageDeferred] takes it. The wait each row is delivered again as is
// rebuilt from the delivery record itself — the kind, what it is waiting
// for, whose it is and whether it is worse — which is what lets a kind that
// pages never, and carries no page event to rebuild from, be delivered again
// too: the delivery record is the account of what was sent whether or not it
// ever qualified for a page.
//
// It returns the rows it delivered again.
func (n *Notifier) Resume(ctx context.Context, driftPool *pgxpool.Pool) ([]string, error) {
	waiting, err := n.stillWaiting(ctx)
	if err != nil {
		return nil, err
	}
	stored, err := allDeliveryRows(ctx, n.pool)
	if err != nil {
		return nil, err
	}
	var again []string
	for _, row := range stored {
		stillOpen := waiting[row.RowID]
		if !stillOpen {
			if stillOpen, err = n.stillWaitingBySubject(ctx, driftPool, row); err != nil {
				return again, err
			}
		}
		if !stillOpen {
			continue
		}
		w, err := row.wait()
		if err != nil {
			return again, err
		}
		for _, a := range row.Attempts {
			if _, err := n.deliver(ctx, Delivery{
				Channel: a.Channel, To: a.RecipientKey, Wait: w, Event: reachedEventFor(a.Channel),
			}); err != nil {
				return again, err
			}
		}
		again = append(again, row.RowID)
	}
	return again, nil
}

// stillWaitingBySubject is "still waiting" read off the subject a kind with
// no log opening waits on, rather than off a decision-log opening with no
// close: a row of one of these kinds never opens in the log, so
// [Notifier.stillWaiting] never finds it, and without this a stop between the
// row opening and its delivery landing would never be redelivered.
//
// A drift mismatch — [KindDriftMismatch] or [KindWindowCapUnevaluated] — is
// still waiting where the drift detector's own store still holds it
// uncleared; [KindDriftDetectorStale]'s one row is still waiting where the
// detector's own last check is still stale; and [KindHarmMarkedReport]'s cap
// row — told apart from an ordinary marked report's own row by
// [capRowPrefix] — is still waiting where the cap is still exceeded for the
// interval that row's own name is for. Each asks through the same seam
// driftpass.go and harmmark.go already read that subject by. A driftPool of
// nil, or a row of any other kind, answers false: nothing here to ask.
func (n *Notifier) stillWaitingBySubject(ctx context.Context, driftPool *pgxpool.Pool, row deliveryRowStored) (bool, error) {
	switch row.WaitKind {
	case KindDriftMismatch, KindWindowCapUnevaluated:
		if driftPool == nil {
			return false, nil
		}
		all, err := driftdetector.All(ctx, driftPool)
		if err != nil {
			return false, err
		}
		for _, m := range all {
			if m.ID == row.RowID {
				return !m.Cleared(), nil
			}
		}
		return false, nil
	case KindDriftDetectorStale:
		if driftPool == nil || row.RowID != driftDetectorStaleRow {
			return false, nil
		}
		checks, err := driftdetector.LastChecks(ctx, driftPool, "")
		if err != nil {
			return false, err
		}
		now := time.Now()
		for _, c := range checks {
			missed, err := c.Stale(now)
			if err != nil {
				return false, err
			}
			if missed {
				return true, nil
			}
		}
		return false, nil
	case KindHarmMarkedReport:
		if !strings.HasPrefix(row.RowID, capRowPrefix) {
			return false, nil
		}
		now := time.Now()
		over, _, intervalSeconds, err := n.overHarmMarkCap(ctx, Wait{ServiceID: row.ServiceID}, now)
		if err != nil || !over {
			return false, err
		}
		return row.RowID == capRow(row.ServiceID, intervalBucket(now, intervalSeconds)), nil
	default:
		return false, nil
	}
}

// reachedEventFor is the page event a redelivery on channel writes: the
// reached event on the page channel, and none on mail or chat, which never
// write one.
func reachedEventFor(channel Channel) Event {
	if channel == ChannelPage {
		return EventReached
	}
	return Event("")
}

// stillWaiting is every row of the log that opened and has not ended: a
// decision with no closing and no abandonment, and a wait with no closing.
func (n *Notifier) stillWaiting(ctx context.Context) (map[string]bool, error) {
	rows, err := n.reader.Read(ctx, componentPrincipal)
	if err != nil {
		return nil, err
	}
	ended := map[string]bool{}
	open := map[string]bool{}
	for _, row := range rows {
		switch row.Part {
		case decisionlog.PartOpen:
			open[row.ID] = true
		case decisionlog.PartClose, decisionlog.PartAbandonment:
			ended[row.Closes] = true
		}
	}
	waiting := map[string]bool{}
	for id := range open {
		if !ended[id] {
			waiting[id] = true
		}
	}
	return waiting, nil
}

// holdingFrom reads back what [people.Holding.String] wrote: a duty by number
// or an obligation by name.
func holdingFrom(stored string) (people.Holding, error) {
	for _, duty := range people.Duties {
		if people.OfDuty(duty).String() == stored {
			return people.OfDuty(duty), nil
		}
	}
	for _, obligation := range people.Obligations {
		if people.OfObligation(obligation).String() == stored {
			return people.OfObligation(obligation), nil
		}
	}
	return people.Holding{}, fmt.Errorf("notifier: %q names no duty and no obligation", stored)
}

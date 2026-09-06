package notifier

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/people"
)

// Resume is this component's restart: the delivery record it overwrites per
// waiting row. A row still waiting is delivered again and one that stopped
// waiting is not, so a stop between a row opening and its delivery landing is
// answered by the next start rather than by a human noticing nothing arrived.
//
// What it reads is its own records: one delivery per row and channel, and the
// log's own rows saying which of them are still open. A row a closing, an
// abandonment or a wait's closing ended is skipped, and its delivery record
// stays as the account of what was sent.
//
// It returns the rows it delivered again.
func (n *Notifier) Resume(ctx context.Context) ([]string, error) {
	waiting, err := n.stillWaiting(ctx)
	if err != nil {
		return nil, err
	}
	deliveries, err := n.deliveriesFor(ctx)
	if err != nil {
		return nil, err
	}
	var again []string
	for _, d := range deliveries {
		if !waiting[d.RowID] {
			continue
		}
		wait, found, err := n.waitOf(ctx, d.RowID)
		if err != nil {
			return again, err
		}
		if !found {
			// A row this component never delivered a page about carries no
			// event to rebuild the wait from, so what it was waiting for is
			// not reconstructible and delivering again would say nothing. The
			// delivery record stands and the row stays waiting.
			continue
		}
		if _, err := n.deliver(ctx, Delivery{
			Channel: d.Channel, To: d.RecipientKey, Wait: wait, Event: EventReached,
		}); err != nil {
			return again, err
		}
		again = append(again, d.RowID)
	}
	return again, nil
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

// deliveriesFor is every delivery record, one per row and channel.
func (n *Notifier) deliveriesFor(ctx context.Context) ([]DeliveryRecord, error) {
	rows, err := n.pool.Query(ctx, `select id, actor_kind, actor_key, actor_key_basis, at,
		row_id, channel, recipient_key, transport_accepted from `+DeliveryTable+` order by at`)
	if err != nil {
		return nil, fmt.Errorf("notifier: reading the delivery records: %w", err)
	}
	defer rows.Close()
	var found []DeliveryRecord
	for rows.Next() {
		var d DeliveryRecord
		var kind, basis, channel string
		if err := rows.Scan(&d.ID, &kind, &d.Actor.Key, &basis, &d.At,
			&d.RowID, &channel, &d.RecipientKey, &d.TransportAccepted); err != nil {
			return nil, fmt.Errorf("notifier: reading a delivery record: %w", err)
		}
		d.Channel = Channel(channel)
		found = append(found, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notifier: reading the delivery records: %w", err)
	}
	return found, nil
}

// waitOf rebuilds one row's wait from the page events this component wrote
// about it: the kind, what it is waiting for, and whose wait it is are on
// every one of them. False where the row has no page event, which is a wait of
// a kind that pages never.
func (n *Notifier) waitOf(ctx context.Context, row string) (Wait, bool, error) {
	events, err := n.EventsFor(ctx, row)
	if err != nil {
		return Wait{}, false, err
	}
	for _, e := range events {
		if e.WaitKind == "" {
			continue
		}
		wait := Wait{Row: row, Kind: Kind(e.WaitKind), Waiting: e.Waiting}
		if e.Holding != "" {
			holding, err := holdingFrom(e.Holding)
			if err != nil {
				return Wait{}, false, err
			}
			wait.Holding = holding
		}
		if Kinds[wait.Kind] == PagesAlways {
			wait.Worse = true
		}
		return wait, true, nil
	}
	return Wait{}, false, nil
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

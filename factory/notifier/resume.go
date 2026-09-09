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
// What it reads is its own records: one delivery per row, and the log's own
// rows saying which of them are still open. A row a closing, an abandonment
// or a wait's closing ended is skipped, and its delivery record stays as the
// account of what was sent. The wait each row is delivered again as is
// rebuilt from the delivery record itself — the kind, what it is waiting
// for, whose it is and whether it is worse — which is what lets a kind that
// pages never, and carries no page event to rebuild from, be delivered again
// too: the delivery record is the account of what was sent whether or not it
// ever qualified for a page.
//
// It returns the rows it delivered again.
func (n *Notifier) Resume(ctx context.Context) ([]string, error) {
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
		if !waiting[row.RowID] {
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

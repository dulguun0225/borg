package gate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
)

// The third kind of row a decision may carry between its open event and the row
// that ends it.

// acknowledgementPayload is what the acknowledgement row says: the open event
// the human has. Every payload in this package goes through the marshaller.
type acknowledgementPayload struct {
	Acknowledged string `json:"acknowledged"`
}

// Acknowledge appends the acknowledgement: a holder of the row's duty saying at
// Work that they have the row. It decides nothing, ends no wait, and excludes
// nobody — the row stays pending, stays in Work for every holder, and the first
// verdict still decides. What it changes is what the other holders see.
//
// An open event admits one acknowledgement per human and any number of humans,
// and a second from the same human is refused as a second close is, by the
// store's own constraint.
//
// Where the row also pages, one act at Work writes both: this call appends the
// row and then calls the notifier for the page's acknowledged event, the way the
// component that ends a wait calls it.
func (g *Gate) Acknowledge(ctx context.Context, opened Opened, human record.Actor) (decisionlog.Row, error) {
	if human.Kind != record.KindHuman {
		return decisionlog.Row{}, fmt.Errorf("%w: actor kind %q",
			decisionlog.ErrAcknowledgementNotHuman, human.Kind)
	}
	payload, err := json.Marshal(acknowledgementPayload{Acknowledged: opened.Row.ID})
	if err != nil {
		return decisionlog.Row{}, fmt.Errorf("gate: marshalling the acknowledgement of %s: %w",
			opened.Row.ID, err)
	}
	row, err := g.log.AppendDecisionAcknowledgement(ctx, decisionlog.Entry{
		Actor:         human,
		Payload:       string(payload),
		FormatVersion: decisionFormatVersion,
		Closes:        opened.Row.ID,
	})
	if err != nil {
		return decisionlog.Row{}, err
	}
	if opened.Pages() {
		if err := g.notifier.Acknowledged(ctx, opened.Row.ID, human); err != nil {
			return row, fmt.Errorf("gate: reporting that %s was acknowledged: %w", opened.Row.ID, err)
		}
	}
	return row, nil
}

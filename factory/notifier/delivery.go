package notifier

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// DeliveryTable is the one table this package owns: one row per delivery
// the notifier attempts, overwritten at each attempt — "what a delivery on
// any of the three writes instead is a delivery record."
const DeliveryTable = "notifier_delivery"

// DeliveryIDPrefix is what [record.NewID] is called with for a row.
const DeliveryIDPrefix = "ndl"

// FormatVersionDelivery is written into format_version on every insert.
const FormatVersionDelivery = "notifier_delivery/1"

// DeliveryDDL is this package's schema. [record.Columns] and
// [record.Constraints] are composed rather than restated. The unique
// constraint is what the upsert conflicts on: one row per waiting row and
// channel, overwritten at every attempt rather than kept as a history.
var DeliveryDDL = []string{
	`create table if not exists ` + DeliveryTable + ` (
	` + record.Columns + `,
	row_id text not null,
	channel text not null,
	recipient_key text not null,
	transport_accepted boolean not null,
	` + record.Constraints + `,
	constraint actor_is_the_notifier check (actor_kind = 'component'),
	constraint row_id_present check (row_id <> ''),
	constraint channel_known check (channel in ('mail', 'chat', 'page')),
	constraint one_row_per_wait_and_channel unique (row_id, channel)
)`,
}

// DeliveryRecord is one row of [DeliveryTable] as it is stored.
type DeliveryRecord struct {
	ID                string
	Actor             record.Actor
	At                string
	RowID             string
	Channel           Channel
	RecipientKey      string
	TransportAccepted bool
}

// recordDelivery upserts the delivery record for one attempt: the row, the
// channel, the recipient key resolved from People, whether the transport
// accepted the send, and when. It is written for every channel — including
// a refused send — and not only the page, which is the one channel that
// also appends a log row.
func (n *Notifier) recordDelivery(ctx context.Context, d Delivery, accepted bool) error {
	rec := DeliveryRecord{
		ID: record.NewID(DeliveryIDPrefix), Actor: Actor, At: record.Now(),
		RowID: d.Wait.Row, Channel: d.Channel, RecipientKey: d.To, TransportAccepted: accepted,
	}
	tx, err := n.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("notifier: beginning the delivery record for %s on %s: %w", d.Wait.Row, d.Channel, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, n.token); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into `+DeliveryTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, row_id, channel, recipient_key, transport_accepted)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		on conflict (row_id, channel) do update set
			at = excluded.at, recipient_key = excluded.recipient_key, transport_accepted = excluded.transport_accepted`,
		rec.ID, FormatVersionDelivery, string(rec.Actor.Kind), rec.Actor.Key, string(rec.Actor.Basis), rec.At,
		rec.RowID, string(rec.Channel), rec.RecipientKey, rec.TransportAccepted,
	)
	if err != nil {
		return fmt.Errorf("notifier: recording the delivery of %s on %s: %w", d.Wait.Row, d.Channel, err)
	}
	return tx.Commit(ctx)
}

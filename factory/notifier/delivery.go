package notifier

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

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
// constraint is what the upsert conflicts on: one row per waiting row,
// channel and recipient, overwritten at every attempt rather than kept as a
// history. The recipient is in the key because a duty held by two humans is
// two deliveries of one row on one channel, and a row keyed without them
// would record only the last attempt — which is what makes "a row no
// delivery was ever accepted for is a fault of the channel and against
// nobody" indistinguishable from one holder of two having been reached.
//
// wait_kind and service_id are the wait's own two fields the record keeps, so
// that a count of what this channel delivered for one service and one kind of
// wait is a query here rather than a walk of the log. The harm mark's cap is
// the one reader.
var DeliveryDDL = []string{
	`create table if not exists ` + DeliveryTable + ` (
	` + record.Columns + `,
	row_id text not null,
	channel text not null,
	recipient_key text not null,
	transport_accepted boolean not null,
	wait_kind text not null,
	service_id text not null,
	` + record.Constraints + `,
	constraint actor_is_the_notifier check (actor_kind = 'component'),
	constraint row_id_present check (row_id <> ''),
	constraint channel_known check (channel in ('mail', 'chat', 'page')),
	constraint one_row_per_wait_channel_and_holder unique (row_id, channel, recipient_key)
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
	// WaitKind and ServiceID are the wait's own two, kept here so that what
	// this channel delivered for one service and one kind is a count and not a
	// walk of the log.
	WaitKind  Kind
	ServiceID string
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
		WaitKind: d.Wait.Kind, ServiceID: d.Wait.ServiceID,
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
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, row_id, channel, recipient_key,
		 transport_accepted, wait_kind, service_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		on conflict (row_id, channel, recipient_key) do update set
			at = excluded.at, transport_accepted = excluded.transport_accepted,
			wait_kind = excluded.wait_kind, service_id = excluded.service_id`,
		rec.ID, FormatVersionDelivery, string(rec.Actor.Kind), rec.Actor.Key, string(rec.Actor.Basis), rec.At,
		rec.RowID, string(rec.Channel), rec.RecipientKey, rec.TransportAccepted,
		string(rec.WaitKind), rec.ServiceID,
	)
	if err != nil {
		return fmt.Errorf("notifier: recording the delivery of %s on %s: %w", d.Wait.Row, d.Channel, err)
	}
	return tx.Commit(ctx)
}

// PagedRowsSince is how many distinct waiting rows of one kind this component
// delivered a page about for one service since a stored time. It counts rows
// and not attempts, because what the harm mark's cap counts is intents paged
// and a row redelivered is one intent still.
//
// It is a read of this package's own table and takes the pool, the way every
// record package's reads do.
func PagedRowsSince(ctx context.Context, pool *pgxpool.Pool, serviceID string, kind Kind, since string) (int, error) {
	var count int
	err := pool.QueryRow(ctx, `select count(distinct row_id) from `+DeliveryTable+`
		where channel = $1 and wait_kind = $2 and service_id = $3 and at >= $4`,
		string(ChannelPage), string(kind), serviceID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("notifier: counting what %s paged for %s: %w", kind, serviceID, err)
	}
	return count, nil
}

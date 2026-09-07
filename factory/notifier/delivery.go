package notifier

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/record"
)

// DeliveryTable is the one table this package owns: one row per delivery
// the notifier attempts, overwritten at each attempt — "what a delivery on
// any of the three writes instead is a delivery record."
const DeliveryTable = "notifier_delivery"

// DeliveryIDPrefix is what [record.NewID] is called with for a row.
const DeliveryIDPrefix = "ndl"

// FormatVersionDelivery is written into format_version on every insert.
const FormatVersionDelivery = "notifier_delivery/3"

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
// wait_kind, service_id, waiting, holding and worse are the wait's own fields
// the record keeps. The first two make a count of what this channel delivered
// for one service and one kind of wait a query here rather than a walk of the
// log, which the harm mark's cap reads. The other three are what a page a
// service's paging hours held back is delivered from when those hours come
// round: mail and chat went out, and the log holds no page event to rebuild
// the wait from, so it is rebuilt from here.
//
// first_accepted_at is the one field the overwrite does not touch once it is
// set: transport_accepted says only whether the latest attempt was accepted,
// and [_The page channel_](../../end-goal/how-the-factory-works/11-screens/05-the-page-channel-and-what-reached-a-human.md)
// splits the human's load at the first delivery a transport accepted, which
// a row overwritten per attempt could not answer. Empty until the transport
// first accepts, and left exactly as it is on every attempt after — an
// attempt refused after that does not clear it, since the split it answers
// is when the channel first succeeded and not whether it still is.
var DeliveryDDL = []string{
	`create table if not exists ` + DeliveryTable + ` (
	` + record.Columns + `,
	row_id text not null,
	channel text not null,
	recipient_key text not null,
	transport_accepted boolean not null,
	wait_kind text not null,
	service_id text not null,
	waiting text not null default '',
	holding text not null default '',
	worse boolean not null default false,
	first_accepted_at text not null default '',
	` + record.Constraints + `,
	constraint actor_is_the_notifier check (actor_kind = 'component'),
	constraint row_id_present check (row_id <> ''),
	constraint channel_known check (channel in ('mail', 'chat', 'page')),
	constraint one_row_per_wait_channel_and_holder unique (row_id, channel, recipient_key),
	constraint first_accepted_at_is_time_layout check (first_accepted_at = '' or first_accepted_at ~ '` + record.TimePattern + `')
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
	// walk of the log. Waiting, Holding and Worse are the rest of the wait,
	// kept so that a page a service's hours held back can go out when those
	// hours come round.
	WaitKind  Kind
	ServiceID string
	Waiting   string
	Holding   string
	Worse     bool
	// FirstAcceptedAt is when the transport first accepted a delivery of this
	// row, channel and recipient — empty where none ever has. It is set once
	// and never overwritten, unlike every other field here.
	FirstAcceptedAt string
}

// recordDelivery upserts the delivery record for one attempt: the row, the
// channel, the recipient key resolved from People, whether the transport
// accepted the send, and when. It is written for every channel — including
// a refused send — and not only the page, which is the one channel that
// also appends a log row.
func (n *Notifier) recordDelivery(ctx context.Context, d Delivery, accepted bool) error {
	holding := ""
	if d.Wait.Holding != (people.Holding{}) {
		holding = d.Wait.Holding.String()
	}
	at := record.Now()
	firstAcceptedAt := ""
	if accepted {
		firstAcceptedAt = at
	}
	rec := DeliveryRecord{
		ID: record.NewID(DeliveryIDPrefix), Actor: Actor, At: at,
		RowID: d.Wait.Row, Channel: d.Channel, RecipientKey: d.To, TransportAccepted: accepted,
		WaitKind: d.Wait.Kind, ServiceID: d.Wait.ServiceID,
		Waiting: d.Wait.Waiting, Holding: holding, Worse: d.Wait.Worse,
		FirstAcceptedAt: firstAcceptedAt,
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
		 transport_accepted, wait_kind, service_id, waiting, holding, worse, first_accepted_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		on conflict (row_id, channel, recipient_key) do update set
			at = excluded.at, transport_accepted = excluded.transport_accepted,
			wait_kind = excluded.wait_kind, service_id = excluded.service_id,
			waiting = excluded.waiting, holding = excluded.holding, worse = excluded.worse,
			first_accepted_at = case
				when `+DeliveryTable+`.first_accepted_at <> '' then `+DeliveryTable+`.first_accepted_at
				when excluded.transport_accepted then excluded.at
				else ''
			end`,
		rec.ID, FormatVersionDelivery, string(rec.Actor.Kind), rec.Actor.Key, string(rec.Actor.Basis), rec.At,
		rec.RowID, string(rec.Channel), rec.RecipientKey, rec.TransportAccepted,
		string(rec.WaitKind), rec.ServiceID, rec.Waiting, rec.Holding, rec.Worse, rec.FirstAcceptedAt,
	)
	if err != nil {
		return fmt.Errorf("notifier: recording the delivery of %s on %s: %w", d.Wait.Row, d.Channel, err)
	}
	return tx.Commit(ctx)
}

// deliveredRows is every row this component has already delivered something
// about, on any channel. It is what tells a row nothing has gone out about at
// all from one whose mail and chat went out with the page channel held to its
// service's paging hours: the second writes no page event, so the page events
// alone cannot separate the two.
func (n *Notifier) deliveredRows(ctx context.Context) (map[string]bool, error) {
	rows, err := n.pool.Query(ctx, `select distinct row_id from `+DeliveryTable)
	if err != nil {
		return nil, fmt.Errorf("notifier: reading which rows have been delivered: %w", err)
	}
	defer rows.Close()
	delivered := map[string]bool{}
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			return nil, fmt.Errorf("notifier: reading a delivered row: %w", err)
		}
		delivered[row] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notifier: reading which rows have been delivered: %w", err)
	}
	return delivered, nil
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

// DeliveriesOf is every delivery record of one row, on every channel and to
// every recipient, in the order the rows were written — what the decision
// view needs to say what reached whom about it.
//
// It is a read of this package's own table and takes the pool, the way every
// record package's reads do.
func DeliveriesOf(ctx context.Context, pool *pgxpool.Pool, rowID string) ([]DeliveryRecord, error) {
	rows, err := pool.Query(ctx, `select id, actor_kind, actor_key, actor_key_basis, at, row_id, channel,
		recipient_key, transport_accepted, wait_kind, service_id, waiting, holding, worse, first_accepted_at
		from `+DeliveryTable+` where row_id = $1 order by at`, rowID)
	if err != nil {
		return nil, fmt.Errorf("notifier: reading the deliveries of %s: %w", rowID, err)
	}
	defer rows.Close()
	var found []DeliveryRecord
	for rows.Next() {
		var d DeliveryRecord
		var kind, basis, channel, waitKind string
		if err := rows.Scan(&d.ID, &kind, &d.Actor.Key, &basis, &d.At, &d.RowID, &channel,
			&d.RecipientKey, &d.TransportAccepted, &waitKind, &d.ServiceID, &d.Waiting, &d.Holding,
			&d.Worse, &d.FirstAcceptedAt); err != nil {
			return nil, fmt.Errorf("notifier: reading a delivery of %s: %w", rowID, err)
		}
		d.Actor.Kind = record.Kind(kind)
		d.Actor.Basis = record.Basis(basis)
		d.Channel = Channel(channel)
		d.WaitKind = Kind(waitKind)
		found = append(found, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notifier: reading the deliveries of %s: %w", rowID, err)
	}
	return found, nil
}

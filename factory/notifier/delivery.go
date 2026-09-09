package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/record"
)

// DeliveryTable is the table an earlier form of this package kept one row in
// per waiting row, channel and recipient. Its DDL stays declared and applied
// — a removal is not a change this store's forward promise can declare yet,
// [postgres.Changes] says so — but nothing here writes it any longer:
// [DeliveryRowTable] is what a delivery on any of the three writes instead,
// one row per waiting row rather than one per channel and recipient of it.
const DeliveryTable = "notifier_delivery"

// DeliveryIDPrefix is what [record.NewID] was called with for a row of
// [DeliveryTable], kept beside it for the same reason.
const DeliveryIDPrefix = "ndl"

// FormatVersionDelivery is what an insert into [DeliveryTable] carried.
const FormatVersionDelivery = "notifier_delivery/3"

// DeliveryDDL is this package's schema: [DeliveryTable], unused and kept for
// the reason named above, and [DeliveryRowTable] beside it, the table this
// package now writes. [record.Columns] and [record.Constraints] are composed
// rather than restated.
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
	`create table if not exists ` + DeliveryRowTable + ` (
	` + record.Columns + `,
	row_id text not null,
	wait_kind text not null,
	service_id text not null,
	waiting text not null default '',
	holding text not null default '',
	person_key text not null default '',
	worse boolean not null default false,
	page_at text not null default '',
	attempts text not null default '[]',
	` + record.Constraints + `,
	constraint delivery_row_actor_is_the_notifier check (actor_kind = 'component'),
	constraint delivery_row_id_present check (row_id <> ''),
	constraint delivery_row_page_at_is_time_layout check (page_at = '' or page_at ~ '` + record.TimePattern + `'),
	constraint one_row_per_waiting_row unique (row_id)
)`,
}

// DeliveryRowTable is the one table this package writes: one row per waiting
// row, overwritten at each attempt on any channel — "so what exists is one
// record per waiting row rather than one per delivery." The row, the
// recipient a channel resolved from People, whether the transport accepted
// the send and when are kept inside it, in [DeliveryAttempt], one entry per
// channel and recipient the row has been attempted on; wait_kind, service_id,
// waiting, holding, person_key and worse are the wait's own fields, kept so a
// page a service's paging hours held back, or a restart, can rebuild the wait
// without reading page events at all. page_at is hours.go's own: the next
// hour a wait of the second kind held back may page, computed once at the
// deferral.
const DeliveryRowTable = "notifier_delivery_row"

// DeliveryRowIDPrefix is what [record.NewID] is called with for a row of
// [DeliveryRowTable].
const DeliveryRowIDPrefix = "ndlr"

// FormatVersionDeliveryRow is written into format_version on every insert
// into [DeliveryRowTable].
const FormatVersionDeliveryRow = "notifier_delivery_row/1"

// DeliveryAttempt is one channel and recipient's attempt, kept inside the one
// record [DeliveryRowTable] holds per waiting row. It is overwritten in place
// on a later attempt at the same channel and recipient, except for
// FirstAcceptedAt, which [_The page channel_](../../end-goal/how-the-factory-works/11-screens/05-the-page-channel-and-what-reached-a-human.md)
// splits the human's load at the first delivery a transport accepted: empty
// until the transport first accepts a send of this channel and recipient,
// and left exactly as it is on every attempt after.
type DeliveryAttempt struct {
	Channel           Channel `json:"channel"`
	RecipientKey      string  `json:"recipient_key"`
	TransportAccepted bool    `json:"transport_accepted"`
	At                string  `json:"at"`
	FirstAcceptedAt   string  `json:"first_accepted_at,omitempty"`
}

// DeliveryRecord is one channel and recipient's delivery of one waiting row,
// as [DeliveriesOf] and [PagedRowsSince] read it back: the row-level fields
// [DeliveryRowTable] keeps once per row, beside the one [DeliveryAttempt] this
// record names. It is not a row of its own in the store; [deliveryRowStored]
// is that, and this is what reading one flattens into.
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

// deliveryRowStored is one row of [DeliveryRowTable] as it is stored: the
// wait's own fields once, and every channel and recipient's attempt inside
// Attempts.
type deliveryRowStored struct {
	ID        string
	Actor     record.Actor
	At        string
	RowID     string
	WaitKind  Kind
	ServiceID string
	Waiting   string
	Holding   string
	Person    string
	Worse     bool
	PageAt    string
	Attempts  []DeliveryAttempt
}

// flatten is one [DeliveryRecord] per attempt r holds, in the order the
// attempts were last written — what [DeliveriesOf] and the resume and hours
// passes read a stored row back as.
func (r deliveryRowStored) flatten() []DeliveryRecord {
	found := make([]DeliveryRecord, 0, len(r.Attempts))
	for _, a := range r.Attempts {
		found = append(found, DeliveryRecord{
			ID: r.ID, Actor: r.Actor, At: a.At, RowID: r.RowID,
			Channel: a.Channel, RecipientKey: a.RecipientKey, TransportAccepted: a.TransportAccepted,
			WaitKind: r.WaitKind, ServiceID: r.ServiceID, Waiting: r.Waiting, Holding: r.Holding, Worse: r.Worse,
			FirstAcceptedAt: a.FirstAcceptedAt,
		})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].At < found[j].At })
	return found
}

// wait rebuilds this row's [Wait] from what the record itself keeps, with no
// read of the page events: what a still-waiting row is delivered again from
// at [Notifier.Resume], and what a page held to a service's hours is
// delivered from at [Notifier.PageDeferred].
func (r deliveryRowStored) wait() (Wait, error) {
	w := Wait{
		Row: r.RowID, Kind: r.WaitKind, Waiting: r.Waiting, ServiceID: r.ServiceID,
		Person: r.Person, Worse: r.Worse, PageAt: r.PageAt,
	}
	if r.Holding != "" {
		holding, err := holdingFrom(r.Holding)
		if err != nil {
			return Wait{}, err
		}
		w.Holding = holding
	}
	return w, nil
}

// hasChannel reports whether any attempt names channel.
func (r deliveryRowStored) hasChannel(channel Channel) bool {
	for _, a := range r.Attempts {
		if a.Channel == channel {
			return true
		}
	}
	return false
}

// recordDelivery upserts the delivery record for one attempt: the row's own
// fields, and the attempt on this channel and recipient inside it — merged
// with whatever the row already held for every other channel and recipient,
// so the row stays one per waiting row rather than one per attempt.
func (n *Notifier) recordDelivery(ctx context.Context, d Delivery, accepted bool) error {
	holding := ""
	if d.Wait.Holding != (people.Holding{}) {
		holding = d.Wait.Holding.String()
	}
	at := record.Now()

	tx, err := n.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("notifier: beginning the delivery record for %s on %s: %w", d.Wait.Row, d.Channel, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, n.token); err != nil {
		return err
	}

	var existing string
	err = tx.QueryRow(ctx, `select attempts from `+DeliveryRowTable+` where row_id = $1`, d.Wait.Row).Scan(&existing)
	var attempts []DeliveryAttempt
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No record for this row yet.
	case err != nil:
		return fmt.Errorf("notifier: reading the delivery already recorded for %s: %w", d.Wait.Row, err)
	default:
		if err := json.Unmarshal([]byte(existing), &attempts); err != nil {
			return fmt.Errorf("notifier: reading the delivery attempts already recorded for %s: %w", d.Wait.Row, err)
		}
	}

	firstAcceptedAt, matched := "", false
	for i := range attempts {
		if attempts[i].Channel != d.Channel || attempts[i].RecipientKey != d.To {
			continue
		}
		firstAcceptedAt = attempts[i].FirstAcceptedAt
		if firstAcceptedAt == "" && accepted {
			firstAcceptedAt = at
		}
		attempts[i] = DeliveryAttempt{
			Channel: d.Channel, RecipientKey: d.To, TransportAccepted: accepted, At: at, FirstAcceptedAt: firstAcceptedAt,
		}
		matched = true
		break
	}
	if !matched {
		if accepted {
			firstAcceptedAt = at
		}
		attempts = append(attempts, DeliveryAttempt{
			Channel: d.Channel, RecipientKey: d.To, TransportAccepted: accepted, At: at, FirstAcceptedAt: firstAcceptedAt,
		})
	}

	encoded, err := json.Marshal(attempts)
	if err != nil {
		return fmt.Errorf("notifier: marshalling the delivery attempts for %s: %w", d.Wait.Row, err)
	}

	_, err = tx.Exec(ctx, `insert into `+DeliveryRowTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, row_id,
		 wait_kind, service_id, waiting, holding, person_key, worse, page_at, attempts)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		on conflict (row_id) do update set
			at = excluded.at, wait_kind = excluded.wait_kind, service_id = excluded.service_id,
			waiting = excluded.waiting, holding = excluded.holding, person_key = excluded.person_key,
			worse = excluded.worse, page_at = excluded.page_at, attempts = excluded.attempts`,
		record.NewID(DeliveryRowIDPrefix), FormatVersionDeliveryRow, string(Actor.Kind), Actor.Key, string(Actor.Basis),
		at, d.Wait.Row, string(d.Wait.Kind), d.Wait.ServiceID, d.Wait.Waiting, holding, d.Wait.Person, d.Wait.Worse,
		d.Wait.PageAt, string(encoded),
	)
	if err != nil {
		return fmt.Errorf("notifier: recording the delivery of %s on %s: %w", d.Wait.Row, d.Channel, err)
	}
	return tx.Commit(ctx)
}

// selectDeliveryRow is the column list every read of [DeliveryRowTable]
// shares.
const selectDeliveryRow = `select id, actor_kind, actor_key, actor_key_basis, at, row_id,
	wait_kind, service_id, waiting, holding, person_key, worse, page_at, attempts from ` + DeliveryRowTable

// scanDeliveryRow reads one row of [DeliveryRowTable] back.
func scanDeliveryRow(row pgx.Row) (deliveryRowStored, error) {
	var r deliveryRowStored
	var kind, basis, waitKind, attempts string
	if err := row.Scan(&r.ID, &kind, &r.Actor.Key, &basis, &r.At, &r.RowID,
		&waitKind, &r.ServiceID, &r.Waiting, &r.Holding, &r.Person, &r.Worse, &r.PageAt, &attempts); err != nil {
		return deliveryRowStored{}, err
	}
	r.Actor.Kind, r.Actor.Basis, r.WaitKind = record.Kind(kind), record.Basis(basis), Kind(waitKind)
	if err := json.Unmarshal([]byte(attempts), &r.Attempts); err != nil {
		return deliveryRowStored{}, fmt.Errorf("notifier: reading the delivery attempts of %s: %w", r.RowID, err)
	}
	return r, nil
}

// rowOf is the stored delivery record of one waiting row, and false where
// nothing has been delivered about it.
func rowOf(ctx context.Context, pool *pgxpool.Pool, rowID string) (deliveryRowStored, bool, error) {
	r, err := scanDeliveryRow(pool.QueryRow(ctx, selectDeliveryRow+` where row_id = $1`, rowID))
	if errors.Is(err, pgx.ErrNoRows) {
		return deliveryRowStored{}, false, nil
	} else if err != nil {
		return deliveryRowStored{}, false, fmt.Errorf("notifier: reading the delivery record of %s: %w", rowID, err)
	}
	return r, true, nil
}

// allDeliveryRows is every stored delivery record.
func allDeliveryRows(ctx context.Context, pool *pgxpool.Pool) ([]deliveryRowStored, error) {
	rows, err := pool.Query(ctx, selectDeliveryRow)
	if err != nil {
		return nil, fmt.Errorf("notifier: reading the delivery records: %w", err)
	}
	defer rows.Close()
	var found []deliveryRowStored
	for rows.Next() {
		r, err := scanDeliveryRow(rows)
		if err != nil {
			return nil, fmt.Errorf("notifier: reading a delivery record: %w", err)
		}
		found = append(found, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notifier: reading the delivery records: %w", err)
	}
	return found, nil
}

// deliveredRows is every row this component has already delivered something
// about, on any channel. It is what tells a row nothing has gone out about at
// all from one whose mail and chat went out with the page channel held to its
// service's paging hours: the second writes no page event, so the page events
// alone cannot separate the two.
func (n *Notifier) deliveredRows(ctx context.Context) (map[string]bool, error) {
	rows, err := n.pool.Query(ctx, `select row_id from `+DeliveryRowTable)
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
// and a row redelivered is one intent still. A row's own page attempt, and
// not the row's last write across every channel, is what is read against
// since: a mail or chat attempt after the page can otherwise move a row's
// last-write time past a page long delivered.
//
// It is a read of this package's own table and takes the pool, the way every
// record package's reads do.
func PagedRowsSince(ctx context.Context, pool *pgxpool.Pool, serviceID string, kind Kind, since string) (int, error) {
	rows, err := pool.Query(ctx, `select attempts from `+DeliveryRowTable+`
		where wait_kind = $1 and service_id = $2`, string(kind), serviceID)
	if err != nil {
		return 0, fmt.Errorf("notifier: counting what %s paged for %s: %w", kind, serviceID, err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return 0, fmt.Errorf("notifier: counting what %s paged for %s: %w", kind, serviceID, err)
		}
		var attempts []DeliveryAttempt
		if err := json.Unmarshal([]byte(raw), &attempts); err != nil {
			return 0, fmt.Errorf("notifier: reading a delivery's attempts for %s: %w", serviceID, err)
		}
		for _, a := range attempts {
			if a.Channel == ChannelPage && a.At >= since {
				count++
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("notifier: counting what %s paged for %s: %w", kind, serviceID, err)
	}
	return count, nil
}

// DeliveriesOf is every delivery record of one row, on every channel and to
// every recipient, in the order they were written — what the decision
// view needs to say what reached whom about it.
//
// It is a read of this package's own table and takes the pool, the way every
// record package's reads do.
func DeliveriesOf(ctx context.Context, pool *pgxpool.Pool, rowID string) ([]DeliveryRecord, error) {
	stored, found, err := rowOf(ctx, pool, rowID)
	if err != nil {
		return nil, fmt.Errorf("notifier: reading the deliveries of %s: %w", rowID, err)
	}
	if !found {
		return nil, nil
	}
	return stored.flatten(), nil
}

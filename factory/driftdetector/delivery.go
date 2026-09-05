package driftdetector

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrAddressEmpty is returned by [Writer.SetAddress] for an empty
	// address.
	ErrAddressEmpty = errors.New("driftdetector: an address is required")
	// ErrNoAddress is returned by [Address] where installing the detector
	// has not written one yet.
	ErrNoAddress = errors.New("driftdetector: no address is set; installing the detector includes writing one")
)

// SetAddress writes the one address the detector delivers its own page to,
// mail or chat — installing it, the owner's and nobody's duty. Writing it
// again replaces it.
func (w *Writer) SetAddress(ctx context.Context, address string) error {
	if address == "" {
		return ErrAddressEmpty
	}
	_, err := w.pool.Exec(ctx, `insert into `+AddressTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, singleton, address)
		values ($1, $2, $3, $4, $5, $6, true, $7)
		on conflict (singleton) do update set at = excluded.at, address = excluded.address`,
		record.NewID(AddressIDPrefix), FormatVersionAddress, string(Actor.Kind), Actor.Key, string(Actor.Basis), record.Now(), address)
	if err != nil {
		return fmt.Errorf("driftdetector: setting the address: %w", err)
	}
	return nil
}

// Address is the address installing the detector wrote, and [ErrNoAddress]
// where none has been.
func Address(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var address string
	err := pool.QueryRow(ctx, `select address from `+AddressTable+` where singleton`).Scan(&address)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoAddress
	} else if err != nil {
		return "", fmt.Errorf("driftdetector: reading the address: %w", err)
	}
	return address, nil
}

// OwnDelivery is one delivery the detector made to its own address, naming
// what it found. It is what the notifier reads back at the factory's next
// start to append the page event the log missed while the process was down.
type OwnDelivery struct {
	ID    string
	Actor record.Actor
	At    string
	Why   string
}

// Deliver records one delivery the detector made to its own address: the
// third comparison finding the notifier's last check stale, or every
// factory last check stale at once. This package does not send mail or
// chat itself — cmd/driftdetector's own process does, the way the
// factory's notifier composes its own — this is the record of having done
// so.
func (w *Writer) Deliver(ctx context.Context, why string) (OwnDelivery, error) {
	if why == "" {
		return OwnDelivery{}, errors.New("driftdetector: a delivery names what it found")
	}
	d := OwnDelivery{ID: record.NewID(DeliveryIDPrefix), Actor: Actor, At: record.Now(), Why: why}
	_, err := w.pool.Exec(ctx, `insert into `+DeliveryTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, why)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		d.ID, FormatVersionDelivery, string(d.Actor.Kind), d.Actor.Key, string(d.Actor.Basis), d.At, d.Why)
	if err != nil {
		return OwnDelivery{}, fmt.Errorf("driftdetector: recording a delivery: %w", err)
	}
	return d, nil
}

// OwnDeliveries is every delivery the detector made to its own address,
// oldest first. It is what the notifier reads at the factory's next start,
// appending the page event for whichever of these it has not appended one
// for yet.
func OwnDeliveries(ctx context.Context, pool *pgxpool.Pool) ([]OwnDelivery, error) {
	rows, err := pool.Query(ctx, `select id, actor_kind, actor_key, actor_key_basis, at, why
		from `+DeliveryTable+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("driftdetector: reading its own deliveries: %w", err)
	}
	defer rows.Close()

	var read []OwnDelivery
	for rows.Next() {
		var d OwnDelivery
		var kind, basis string
		if err := rows.Scan(&d.ID, &kind, &d.Actor.Key, &basis, &d.At, &d.Why); err != nil {
			return nil, fmt.Errorf("driftdetector: reading one of its own deliveries: %w", err)
		}
		d.Actor.Kind = record.Kind(kind)
		d.Actor.Basis = record.Basis(basis)
		read = append(read, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("driftdetector: reading its own deliveries: %w", err)
	}
	return read, nil
}

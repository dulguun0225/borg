package consumercontract

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

// Every read here takes a [Querier] and not a writer, because reading a consumer
// contract is not a reason to be handed the thing that writes one. That is the
// arrangement every record package in the factory has, with one addition: a
// derivation written again at an upgrade reads the newest derivation of the same
// item from inside its own transaction, so the same reads have to work against a
// transaction as well as against the pool.

// Querier is what a read here is performed against: a pool, or the transaction a
// write is running inside. It is two methods and not an abstraction over the
// store, and it is the arrangement package contract already has.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var (
	_ Querier = (*pgxpool.Pool)(nil)
	_ Querier = (pgx.Tx)(nil)
)

const selectPredicate = `select id, actor_kind, actor_key, actor_key_basis, at, item_id, service_id, artifact_id,
	address, producer_service, producer_service_id, interface_name, element, kind, argument
	from ` + Table

func scan(row pgx.Row) (Predicate, error) {
	var p Predicate
	var kind, basis, predicateKind string
	err := row.Scan(&p.ID, &kind, &p.Actor.Key, &basis, &p.At, &p.ItemID, &p.ServiceID, &p.ArtifactID,
		&p.Address, &p.ProducerService, &p.ProducerServiceID, &p.Interface, &p.Element, &predicateKind, &p.Argument)
	if err != nil {
		return Predicate{}, err
	}
	p.Actor.Kind = record.Kind(kind)
	p.Actor.Basis = record.Basis(basis)
	p.Kind = gatepolicy.PredicateKind(predicateKind)
	return p, nil
}

const selectDerivation = `select id, actor_kind, actor_key, actor_key_basis, at, item_id, service_id, artifact_id,
	extractor, extractor_version, toolchain, factory_version, unfollowed, cause, reported
	from ` + DerivationTable

func scanDerivation(row pgx.Row) (Derivation, error) {
	var d Derivation
	var kind, basis, unfollowed, cause string
	err := row.Scan(&d.ID, &kind, &d.Actor.Key, &basis, &d.At, &d.ItemID, &d.ServiceID, &d.ArtifactID,
		&d.Extractor.Name, &d.Extractor.Version, &d.Extractor.Toolchain, &d.Extractor.FactoryVersion,
		&unfollowed, &cause, &d.Reported)
	if err != nil {
		return Derivation{}, err
	}
	d.Actor.Kind = record.Kind(kind)
	d.Actor.Basis = record.Basis(basis)
	d.Unfollowed = splitLines(unfollowed)
	d.Cause = Cause(cause)
	return d, nil
}

// Get is one predicate by id.
func Get(ctx context.Context, q Querier, id string) (Predicate, error) {
	p, err := scan(q.QueryRow(ctx, selectPredicate+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Predicate{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Predicate{}, fmt.Errorf("consumercontract: reading %s: %w", id, err)
	}
	return p, nil
}

// ForArtifact is every predicate one consumer contract version introduced.
func ForArtifact(ctx context.Context, q Querier, artifactID string) ([]Predicate, error) {
	if artifactID == "" {
		return nil, nil
	}
	return list(ctx, q, selectPredicate+`
		where artifact_id = $1 order by producer_service, interface_name, element, kind`, artifactID)
}

// ForItems is every predicate the newest consumer contract version of each of
// these items introduced. It is the read the in-force query is built on: which
// items are in the range is a question about releases and windows, which this
// package does not read, so the caller resolves the range and hands over the
// items.
//
// The newest version of each item and not every version of it, because a stage
// attempted twice authors two consumer contract versions and the later one is
// what the item declares — the same rule the artifact store's version chain
// already sets, applied here because a predicate has no field saying it was
// superseded.
func ForItems(ctx context.Context, q Querier, itemIDs []string) ([]Predicate, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	return list(ctx, q, `select d.id, d.actor_kind, d.actor_key, d.actor_key_basis, d.at, d.item_id, d.service_id,
		d.artifact_id, d.address, d.producer_service, d.producer_service_id, d.interface_name, d.element,
		d.kind, d.argument
		from `+Table+` d
		where d.item_id = any($1)
		and d.artifact_id = (select newest.artifact_id from `+Table+` newest
			where newest.item_id = d.item_id order by newest.at desc, newest.artifact_id desc limit 1)
		order by d.service_id, d.producer_service, d.interface_name, d.element, d.kind`, itemIDs)
}

// NamingElement is every predicate among these that names one element of one
// producer's interface. It is the filter the deprecation list and the enforcement
// check are both a use of, kept here rather than written twice by two callers.
func NamingElement(predicates []Predicate, producerServiceID, interfaceName, element string) []Predicate {
	var naming []Predicate
	for _, p := range predicates {
		if p.ProducerServiceID == producerServiceID && p.Interface == interfaceName && p.Element == element {
			naming = append(naming, p)
		}
	}
	return naming
}

// AgainstInterface is every predicate among these that names one producer's
// interface, whichever element.
func AgainstInterface(predicates []Predicate, producerServiceID, interfaceName string) []Predicate {
	var naming []Predicate
	for _, p := range predicates {
		if p.ProducerServiceID == producerServiceID && p.Interface == interfaceName {
			naming = append(naming, p)
		}
	}
	return naming
}

// AgainstProducer is every predicate ever derived that names one producer,
// whichever consumer and whichever release. It is deliberately unfiltered: the
// range that decides which of them bind is a question about releases and windows,
// which this package does not read, so a caller that needs the range asks for it and
// a caller that needs a reading about the service does not.
//
// Its reader of the second kind is the risk score's context factor, which counts
// the services declaring they consume what this one publishes. That factor is a
// reading about the service rather than enforcement's question about one candidate,
// so it filters by there being a release naming the item and not by the in-force
// range.
func AgainstProducer(ctx context.Context, q Querier, producerServiceID string) ([]Predicate, error) {
	if producerServiceID == "" {
		return nil, nil
	}
	return list(ctx, q, selectPredicate+`
		where producer_service_id = $1
		order by service_id, interface_name, element, kind`, producerServiceID)
}

// ItemsOf is the distinct items among these predicates, which is what a reader
// naming the consumers behind a list walks to.
func ItemsOf(predicates []Predicate) []string {
	var items []string
	for _, p := range predicates {
		if !slices.Contains(items, p.ItemID) {
			items = append(items, p.ItemID)
		}
	}
	return items
}

func list(ctx context.Context, q Querier, statement string, args ...any) ([]Predicate, error) {
	rows, err := q.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("consumercontract: reading predicates: %w", err)
	}
	defer rows.Close()

	var read []Predicate
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("consumercontract: reading a predicate: %w", err)
		}
		read = append(read, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("consumercontract: reading predicates: %w", err)
	}
	return read, nil
}

// ConsumerServicesEver is every service that has ever declared a predicate
// against one producer, in the order they were first written, including the
// producer itself — a service declaring against its own store contract is its own
// past, and that consumer is exactly why a store promises forward.
//
// It is how the set of consumers to compute a range for is found without reading
// every service there is: what makes a service a candidate consumer is a
// consumer contract naming the producer, and that is a row of this table. A
// service on this list may have nothing in force — its consumer contracts may
// all be outside the range — which is what the range is for.
func ConsumerServicesEver(ctx context.Context, q Querier, producerServiceID string) ([]string, error) {
	if producerServiceID == "" {
		return nil, nil
	}
	rows, err := q.Query(ctx, `select service_id, min(at) as first from `+Table+`
		where producer_service_id = $1 group by service_id order by first, service_id`,
		producerServiceID)
	if err != nil {
		return nil, fmt.Errorf("consumercontract: reading who has declared against %s: %w", producerServiceID, err)
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var id, first string
		if err := rows.Scan(&id, &first); err != nil {
			return nil, fmt.Errorf("consumercontract: reading a service declaring against %s: %w", producerServiceID, err)
		}
		services = append(services, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("consumercontract: reading who has declared against %s: %w", producerServiceID, err)
	}
	return services, nil
}

// DerivationFor is the derivation of one consumer contract version, and false
// where the version has none. Every version written by this package has one, so
// false is a version written before the derivation record existed or one written
// around this package.
func DerivationFor(ctx context.Context, q Querier, artifactID string) (Derivation, bool, error) {
	if artifactID == "" {
		return Derivation{}, false, nil
	}
	d, err := scanDerivation(q.QueryRow(ctx, selectDerivation+` where artifact_id = $1`, artifactID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Derivation{}, false, nil
	} else if err != nil {
		return Derivation{}, false, fmt.Errorf("consumercontract: reading the derivation of %s: %w", artifactID, err)
	}
	return d, true, nil
}

// NewestDerivation is the newest derivation of one item's consumer contract, and
// false where the item has none. It is what [DeriveAgain] reads before it writes,
// and what a reader asking whether a consumer's record is partial or could not
// derive reads: a release's contract in force is its derivation by the newest
// extractor.
func NewestDerivation(ctx context.Context, q Querier, itemID string) (Derivation, bool, error) {
	if itemID == "" {
		return Derivation{}, false, nil
	}
	d, err := scanDerivation(q.QueryRow(ctx, selectDerivation+`
		where item_id = $1 order by at desc, id desc limit 1`, itemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Derivation{}, false, nil
	} else if err != nil {
		return Derivation{}, false, fmt.Errorf("consumercontract: reading the newest derivation of %s: %w", itemID, err)
	}
	return d, true, nil
}

// StandingCouldNotDerive is every consumer whose newest derivation could not
// derive at all, whatever the producer and whatever the service — a record and
// not an empty list, because "no consumer reads this" and "no consumer's read
// was visible" call for opposite responses.
//
// It is the whole install because nothing bounds what an unreadable consumer
// consumes: what reads it is the score's context factor, which cannot say which
// services consume what a candidate publishes while any consumer is unreadable,
// so the factor is resolved rather than valued and a human decides whatever the
// formula returns. A later derivation by a newer extractor supersedes an earlier
// one, so it is the newest per item that stands.
func StandingCouldNotDerive(ctx context.Context, q Querier) ([]Derivation, error) {
	rows, err := q.Query(ctx, `select d.id, d.actor_kind, d.actor_key, d.actor_key_basis, d.at, d.item_id,
		d.service_id, d.artifact_id, d.extractor, d.extractor_version, d.toolchain, d.factory_version,
		d.unfollowed, d.cause, d.reported
		from `+DerivationTable+` d
		where d.cause <> ''
		and d.at = (select max(newest.at) from `+DerivationTable+` newest where newest.item_id = d.item_id)
		order by d.at, d.id`)
	if err != nil {
		return nil, fmt.Errorf("consumercontract: reading the consumers nobody could read: %w", err)
	}
	defer rows.Close()

	var read []Derivation
	for rows.Next() {
		d, err := scanDerivation(rows)
		if err != nil {
			return nil, fmt.Errorf("consumercontract: reading a derivation: %w", err)
		}
		read = append(read, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("consumercontract: reading the consumers nobody could read: %w", err)
	}
	return read, nil
}

// DerivationsForItems is the newest derivation of each of these items, in the
// order the items were given nothing about — it is the read beside [ForItems],
// and it answers which of the consumers in force are partial and which could not
// be derived at all.
func DerivationsForItems(ctx context.Context, q Querier, itemIDs []string) ([]Derivation, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `select d.id, d.actor_kind, d.actor_key, d.actor_key_basis, d.at, d.item_id,
		d.service_id, d.artifact_id, d.extractor, d.extractor_version, d.toolchain, d.factory_version,
		d.unfollowed, d.cause, d.reported
		from `+DerivationTable+` d
		where d.item_id = any($1)
		and d.at = (select max(newest.at) from `+DerivationTable+` newest where newest.item_id = d.item_id)
		order by d.service_id, d.item_id`, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("consumercontract: reading the derivations of %d items: %w", len(itemIDs), err)
	}
	defer rows.Close()

	var read []Derivation
	for rows.Next() {
		d, err := scanDerivation(rows)
		if err != nil {
			return nil, fmt.Errorf("consumercontract: reading a derivation: %w", err)
		}
		read = append(read, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("consumercontract: reading the derivations of %d items: %w", len(itemIDs), err)
	}
	return read, nil
}

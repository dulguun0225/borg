package declaration

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

// Every read here takes the pool and not a writer, because reading a declaration
// is not a reason to be handed the thing that writes one. That is the arrangement
// every record package in the factory has.

const selectPredicate = `select id, actor_kind, actor_name, at, item_id, service_id, artifact_id,
	producer_service, producer_service_id, interface_name, element, kind, argument
	from ` + Table

func scan(row pgx.Row) (Predicate, error) {
	var p Predicate
	var kind, predicateKind string
	err := row.Scan(&p.ID, &kind, &p.Actor.Name, &p.At, &p.ItemID, &p.ServiceID, &p.ArtifactID,
		&p.ProducerService, &p.ProducerServiceID, &p.Interface, &p.Element, &predicateKind, &p.Argument)
	if err != nil {
		return Predicate{}, err
	}
	p.Actor.Kind = record.Kind(kind)
	p.Kind = gatepolicy.PredicateKind(predicateKind)
	return p, nil
}

// Get is one predicate by id.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Predicate, error) {
	p, err := scan(pool.QueryRow(ctx, selectPredicate+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Predicate{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Predicate{}, fmt.Errorf("declaration: reading %s: %w", id, err)
	}
	return p, nil
}

// ForArtifact is every predicate one declaration version introduced.
func ForArtifact(ctx context.Context, pool *pgxpool.Pool, artifactID string) ([]Predicate, error) {
	if artifactID == "" {
		return nil, nil
	}
	return list(ctx, pool, selectPredicate+`
		where artifact_id = $1 order by producer_service, interface_name, element, kind`, artifactID)
}

// ForItems is every predicate the newest declaration version of each of these
// items introduced. It is the read the in-force query is built on: which items are
// in the range is a question about releases and windows, which this package does
// not read, so the caller resolves the range and hands over the items.
//
// The newest version of each item and not every version of it, because a stage
// attempted twice authors two declaration versions and the later one is what the
// item declares — the same rule the artifact store's version chain already sets,
// applied here because a predicate has no field saying it was superseded.
func ForItems(ctx context.Context, pool *pgxpool.Pool, itemIDs []string) ([]Predicate, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	return list(ctx, pool, `select d.id, d.actor_kind, d.actor_name, d.at, d.item_id, d.service_id,
		d.artifact_id, d.producer_service, d.producer_service_id, d.interface_name, d.element,
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
func AgainstProducer(ctx context.Context, pool *pgxpool.Pool, producerServiceID string) ([]Predicate, error) {
	if producerServiceID == "" {
		return nil, nil
	}
	return list(ctx, pool, selectPredicate+`
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

func list(ctx context.Context, pool *pgxpool.Pool, statement string, args ...any) ([]Predicate, error) {
	rows, err := pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("declaration: reading predicates: %w", err)
	}
	defer rows.Close()

	var read []Predicate
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("declaration: reading a predicate: %w", err)
		}
		read = append(read, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("declaration: reading predicates: %w", err)
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
// declaration naming the producer, and that is a row of this table. A service on
// this list may have nothing in force — its declarations may all be outside the
// range — which is what the range is for.
func ConsumerServicesEver(ctx context.Context, pool *pgxpool.Pool, producerServiceID string) ([]string, error) {
	if producerServiceID == "" {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `select service_id, min(at) as first from `+Table+`
		where producer_service_id = $1 group by service_id order by first, service_id`,
		producerServiceID)
	if err != nil {
		return nil, fmt.Errorf("declaration: reading who has declared against %s: %w", producerServiceID, err)
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var id, first string
		if err := rows.Scan(&id, &first); err != nil {
			return nil, fmt.Errorf("declaration: reading a service declaring against %s: %w", producerServiceID, err)
		}
		services = append(services, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("declaration: reading who has declared against %s: %w", producerServiceID, err)
	}
	return services, nil
}

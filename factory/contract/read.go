package contract

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Every read here takes a [Querier] and not a [Publisher], because reading a
// contract is not a reason to be handed the thing that writes one. That is the
// arrangement every record package in the factory has, with one addition: the
// publish runs inside the mint's transaction and reads the version below the one
// it is about, so the same reads have to work against a transaction as well as
// against the pool. [Querier] is the whole of that addition.

// Querier is what a read here is performed against: a pool, or the transaction a
// publish is running inside. It is two methods and not an abstraction over the
// store — a publish has to see the version below it, and inside a transaction the
// pool would not.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var (
	_ Querier = (*pgxpool.Pool)(nil)
	_ Querier = (pgx.Tx)(nil)
)

const selectContract = `select id, actor_kind, actor_key, actor_key_basis, at, service_id, name, kind from ` + Table

func scanContract(row pgx.Row) (Contract, error) {
	var c Contract
	var kind, basis, contractKind string
	if err := row.Scan(&c.ID, &kind, &c.Actor.Key, &basis, &c.At, &c.ServiceID, &c.Name, &contractKind); err != nil {
		return Contract{}, err
	}
	c.Actor.Kind = record.Kind(kind)
	c.Actor.Basis = record.Basis(basis)
	c.Kind = Kind(contractKind)
	return c, nil
}

// Get is one contract by id.
func Get(ctx context.Context, q Querier, id string) (Contract, error) {
	c, err := scanContract(q.QueryRow(ctx, selectContract+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Contract{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Contract{}, fmt.Errorf("contract: reading %s: %w", id, err)
	}
	return c, nil
}

// ByName is the contract of one service by the interface's own name in its
// build, and false where the service publishes none by that name. The pair is
// unique in the store, so at most one row can answer.
//
// It is what a consumer contract is resolved through: a consumer's build names
// the producer's service and the interface, and a contract exists only from the
// merge that first published it — so a consumer contract against an interface no
// release has published yet resolves to nothing here, which is absence and not an
// error.
func ByName(ctx context.Context, q Querier, serviceID, name string) (Contract, bool, error) {
	if serviceID == "" || name == "" {
		return Contract{}, false, nil
	}
	c, err := scanContract(q.QueryRow(ctx, selectContract+` where service_id = $1 and name = $2`, serviceID, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return Contract{}, false, nil
	} else if err != nil {
		return Contract{}, false, fmt.Errorf("contract: reading %s of %s: %w", name, serviceID, err)
	}
	return c, true, nil
}

// OfService is every contract one service publishes, in the order they were
// first published.
func OfService(ctx context.Context, q Querier, serviceID string) ([]Contract, error) {
	return listContracts(ctx, q, selectContract+` where service_id = $1 order by at, id`, serviceID)
}

// All is every contract in the factory, in the order they were first published.
// Its readers are the command-line interface's own printer and the deprecation detector,
// which has to walk every marked element there is.
func All(ctx context.Context, q Querier) ([]Contract, error) {
	return listContracts(ctx, q, selectContract+` order by at, id`)
}

func listContracts(ctx context.Context, q Querier, statement string, args ...any) ([]Contract, error) {
	rows, err := q.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("contract: reading contracts: %w", err)
	}
	defer rows.Close()

	var read []Contract
	for rows.Next() {
		c, err := scanContract(rows)
		if err != nil {
			return nil, fmt.Errorf("contract: reading a contract: %w", err)
		}
		read = append(read, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("contract: reading contracts: %w", err)
	}
	return read, nil
}

const selectVersion = `select id, actor_kind, actor_key, actor_key_basis, at, contract_id, service_id,
	release_id, release_number, item_id, major, minor, patch
	from ` + VersionTable

func scanVersion(row pgx.Row) (Version, error) {
	var v Version
	var kind, basis string
	err := row.Scan(&v.ID, &kind, &v.Actor.Key, &basis, &v.At, &v.ContractID, &v.ServiceID,
		&v.ReleaseID, &v.ReleaseNumber, &v.ItemID, &v.Semver.Major, &v.Semver.Minor, &v.Semver.Patch)
	if err != nil {
		return Version{}, err
	}
	v.Actor.Kind = record.Kind(kind)
	v.Actor.Basis = record.Basis(basis)
	return v, nil
}

// VersionAt is the version of one contract in force at a release of that number:
// the newest version minted at or below it. Most releases publish no new version,
// so the version a release publishes is usually one minted below it, and that is
// what this answers.
//
// False is a contract with no version at or below the number, which is a release
// older than the contract's first.
func VersionAt(ctx context.Context, q Querier, contractID string, releaseNumber int64) (Version, bool, error) {
	v, err := scanVersion(q.QueryRow(ctx, selectVersion+`
		where contract_id = $1 and release_number <= $2
		order by release_number desc, major desc, minor desc, patch desc limit 1`,
		contractID, releaseNumber))
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, false, nil
	} else if err != nil {
		return Version{}, false, fmt.Errorf("contract: reading the version of %s at release %d: %w",
			contractID, releaseNumber, err)
	}
	return v, true, nil
}

// NewestVersion is the newest version of one contract, and false where it has
// none. It is one of the design's two baselines: a consumer contract
// is checked against the version its producer's newest release publishes,
// because that is what the consumer will meet.
func NewestVersion(ctx context.Context, q Querier, contractID string) (Version, bool, error) {
	v, err := scanVersion(q.QueryRow(ctx, selectVersion+`
		where contract_id = $1 order by release_number desc, major desc, minor desc, patch desc limit 1`,
		contractID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, false, nil
	} else if err != nil {
		return Version{}, false, fmt.Errorf("contract: reading the newest version of %s: %w", contractID, err)
	}
	return v, true, nil
}

// VersionsOf is every version of one contract, lowest release first.
func VersionsOf(ctx context.Context, q Querier, contractID string) ([]Version, error) {
	return listVersions(ctx, q, selectVersion+`
		where contract_id = $1 order by release_number, major, minor, patch`, contractID)
}

// VersionsForRelease is every contract version one release publishes, which is
// the inbound edge "the release names the contract versions it publishes" is. Most
// releases publish none.
func VersionsForRelease(ctx context.Context, q Querier, releaseID string) ([]Version, error) {
	if releaseID == "" {
		return nil, nil
	}
	return listVersions(ctx, q, selectVersion+` where release_id = $1 order by contract_id`, releaseID)
}

func listVersions(ctx context.Context, q Querier, statement string, args ...any) ([]Version, error) {
	rows, err := q.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("contract: reading versions: %w", err)
	}
	defer rows.Close()

	var read []Version
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("contract: reading a version: %w", err)
		}
		read = append(read, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("contract: reading versions: %w", err)
	}
	return read, nil
}

// ElementsOf is the elements of one version, ordered by name — which is the order
// a [Form] is in, so a form read back out of the store equals the one that was
// derived.
func ElementsOf(ctx context.Context, q Querier, versionID string) ([]Element, error) {
	rows, err := q.Query(ctx, `select name, kind, element_position, declared_type, required, populated, deprecated,
		accepted_domain, range_low, range_high, not_null, unique_rule from `+ElementTable+`
		where contract_version_id = $1 order by name`, versionID)
	if err != nil {
		return nil, fmt.Errorf("contract: reading the elements of %s: %w", versionID, err)
	}
	defer rows.Close()

	var read []Element
	for rows.Next() {
		var e Element
		var kind, position, domain string
		var low, high *float64
		if err := rows.Scan(&e.Name, &kind, &position, &e.Type, &e.Required, &e.Populated, &e.Marked,
			&domain, &low, &high, &e.NotNull, &e.Unique); err != nil {
			return nil, fmt.Errorf("contract: reading an element of %s: %w", versionID, err)
		}
		e.Kind, e.Position, e.Domain = ElementKind(kind), Position(position), DomainNames(domain)
		if low != nil && high != nil {
			e.Range = &Range{Low: *low, High: *high}
		}
		read = append(read, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("contract: reading the elements of %s: %w", versionID, err)
	}
	return read, nil
}

// FormOf is one version's whole form: the contract's name and kind with that
// version's elements. It takes the contract rather than reading it, because every
// caller here has one already and reading it again would be a second query for a
// name it holds.
func FormOf(ctx context.Context, q Querier, c Contract, versionID string) (Form, error) {
	elements, err := ElementsOf(ctx, q, versionID)
	if err != nil {
		return Form{}, err
	}
	return Form{Name: c.Name, Kind: c.Kind, Elements: elements}, nil
}

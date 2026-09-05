package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
)

var (
	// ErrNameEmpty is returned by [Writer.Create] for a service with no name.
	ErrNameEmpty = errors.New("service: the name is empty")
	// ErrRepositoryEmpty is returned by [Writer.Create] for a service with no
	// repository.
	ErrRepositoryEmpty = errors.New("service: the repository is empty")
	// ErrProjectEmpty is returned by [Writer.Create] for a service in no
	// project. The project is part of the identity decomposition writes, taken
	// from the project intake wrote on the intent, and no later write moves it.
	ErrProjectEmpty = errors.New("service: the project is empty")
	// ErrNotFound is returned where no service has the id.
	ErrNotFound = errors.New("service: no service has that id")
)

// Service is one service as it is stored: the identity decomposition wrote, the
// fields an owner authors at Factory, and the four the deployer populates.
type Service struct {
	ID         string
	Actor      record.Actor
	At         string
	Name       string
	Repository string
	// ProjectID is the project this service is in. It is identity and never
	// moves, so nothing here writes it after [Writer.Create].
	ProjectID string

	// Provisioned is what the owner recorded once the repository and the store
	// exist, with the shape the repository host gave the credentials.
	Provisioned Provisioned
	// RetiredAt is when an owner retired the service, and empty while it stands.
	RetiredAt string
	// Targets is which of the environment's targets this service runs on, in the
	// order a rollout reaches them, and empty where the owner named none — which
	// every reader of targets reads as every target of the environment.
	Targets []string

	// Parameters is the gate-policy values an owner authored here.
	Parameters Parameters

	// The authored values that are not gate policy's, each absent where the
	// owner authored none.
	MutantCap                gatepolicy.Authored
	FailureRecordKeyCap      gatepolicy.Authored
	UnreliableBound          gatepolicy.Authored
	IncidentItemBoundSeconds gatepolicy.Authored
	SnapshotRetentionSeconds gatepolicy.Authored
	Objective                Objective
	PagingHours              PagingHours
	ProductLicence           string

	// Reachability is the deployer's four fields and when it wrote them.
	Reachability Reachability
}

// Retired is whether an owner has retired the service. Every reader that skips a
// retired service asks this rather than comparing the timestamp itself.
func (s Service) Retired() bool { return s.RetiredAt != "" }

// Writer is the identity's writer, held by decomposition.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Create writes a service's identity: its name, its repository, and the project
// it is in. A name already taken is refused by the store's unique constraint,
// and the error carries that refusal rather than this package pre-checking — a
// pre-check and an insert are two statements a concurrent decomposition can
// interleave.
//
// Every other column is left as it is created here: no parameter, nothing
// provisioned, no target named, and the deployer's four false. That is the seam
// doc.go states — decomposition writes identity and never a parameter.
func (w *Writer) Create(ctx context.Context, actor record.Actor, name, repository, projectID string) (Service, error) {
	if err := actor.Validate(); err != nil {
		return Service{}, err
	}
	if name == "" {
		return Service{}, ErrNameEmpty
	}
	if repository == "" {
		return Service{}, ErrRepositoryEmpty
	}
	if projectID == "" {
		return Service{}, ErrProjectEmpty
	}

	s := Service{
		ID:         record.NewID(IDPrefix),
		Actor:      actor,
		At:         record.Now(),
		Name:       name,
		Repository: repository,
		ProjectID:  projectID,
		Parameters: Parameters{
			WindowSize:  map[gatepolicy.Quantity]gatepolicy.Authored{},
			WindowPower: map[gatepolicy.Quantity]gatepolicy.Authored{},
		},
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Service{}, fmt.Errorf("service: beginning the creation of %q: %w", name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Service{}, err
	}

	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, name, repository, project_id,
		provisioned_at, repository_credential_shape, repository_credential_branch, repository_credential_master,
		retired_at, targets,
		window_confidence, window_cap_seconds, window_limit, exposure_bound,
		mutant_cap, failure_record_key_cap, unreliable_bound, incident_item_bound_seconds,
		snapshot_retention_seconds, objective, objective_period_seconds,
		paging_hours_start, paging_hours_end, paging_hours_zone, product_licence,
		target_reached, instances_replaceable, rollback_path_present, emission_readable, deployer_wrote_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9,
		'', '', '', '',
		'', '',
		null, null, null, null,
		null, null, null, null,
		null, null, null,
		'', '', '', '',
		false, false, false, false, '')`,
		s.ID, FormatVersion, string(s.Actor.Kind), s.Actor.Key, string(s.Actor.Basis), s.At,
		s.Name, s.Repository, s.ProjectID,
	)
	if err != nil {
		return Service{}, fmt.Errorf("service: creating %q: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Service{}, fmt.Errorf("service: committing the creation of %q: %w", name, err)
	}
	return s, nil
}

const selectService = `select id, actor_kind, actor_key, actor_key_basis, at, name, repository, project_id,
	provisioned_at, repository_credential_shape, repository_credential_branch, repository_credential_master,
	retired_at, targets,
	window_confidence, window_cap_seconds, window_limit, exposure_bound,
	mutant_cap, failure_record_key_cap, unreliable_bound, incident_item_bound_seconds,
	snapshot_retention_seconds, objective, objective_period_seconds,
	paging_hours_start, paging_hours_end, paging_hours_zone, product_licence,
	target_reached, instances_replaceable, rollback_path_present, emission_readable, deployer_wrote_at
	from ` + Table

// Get is one service by id, with everything on the record: the identity, every
// authored field, the per-quantity sizes and powers, and the deployer's four. It
// takes the pool and not a [Writer], because reading a service is not a reason to
// be handed the thing that creates them.
//
// The seed and the value set are not here: each authoring of those is a version
// of its own, read by [SeedInForce] and [ValueSetInForce].
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Service, error) {
	s, err := scan(pool.QueryRow(ctx, selectService+` where id = $1`, id), id)
	if err != nil {
		return Service{}, err
	}
	return withQuantities(ctx, pool, s)
}

// ByName is the service of that name, and false where no service has it. The
// name is unique in the store, so at most one row can answer.
//
// This is what decomposition calls before it creates: a service the work changes
// may not exist yet, and decomposition writes a service's identity once, so the
// second item on that service reaches the record the first one wrote. An absent
// service is false and not an error, because absence is the case the caller acts
// on.
//
// What the pair costs: the look-up and the create are two statements, so two
// decompositions of one new service name can both find nothing, and what refuses
// the second create is the store's unique constraint rather than this function.
func ByName(ctx context.Context, pool *pgxpool.Pool, name string) (Service, bool, error) {
	s, err := scan(pool.QueryRow(ctx, selectService+` where name = $1`, name), name)
	if errors.Is(err, ErrNotFound) {
		return Service{}, false, nil
	} else if err != nil {
		return Service{}, false, err
	}
	s, err = withQuantities(ctx, pool, s)
	if err != nil {
		return Service{}, false, err
	}
	return s, true, nil
}

// All is every service, in the order they were created, retired ones included: a
// reader that skips a retired service asks [Service.Retired] rather than being
// given a shorter list it cannot tell from an install with fewer services.
//
// It takes the pool and not a [Writer], for the reason [Get] does — and the
// drift detector, which walks every service there is, holds no writer of
// anything in the factory at all.
func All(ctx context.Context, pool *pgxpool.Pool) ([]Service, error) {
	rows, err := pool.Query(ctx, selectService+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("service: reading the services: %w", err)
	}
	defer rows.Close()

	var read []Service
	for rows.Next() {
		s, err := scan(rows, "a service")
		if err != nil {
			return nil, err
		}
		read = append(read, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("service: reading the services: %w", err)
	}
	for n, s := range read {
		if read[n], err = withQuantities(ctx, pool, s); err != nil {
			return nil, err
		}
	}
	return read, nil
}

// scan reads one row of [Table], turning each null number into an unauthored
// value rather than a zero.
func scan(row pgx.Row, named string) (Service, error) {
	var s Service
	var kind, basis, targets string
	var shape, branch, master string
	var confidence, capSeconds, limit, exposure *float64
	var mutantCap, keyCap, unreliable, incidentBound, snapshotRetention *float64
	var objective, objectivePeriod *float64
	err := row.Scan(&s.ID, &kind, &s.Actor.Key, &basis, &s.At, &s.Name, &s.Repository, &s.ProjectID,
		&s.Provisioned.At, &shape, &branch, &master,
		&s.RetiredAt, &targets,
		&confidence, &capSeconds, &limit, &exposure,
		&mutantCap, &keyCap, &unreliable, &incidentBound,
		&snapshotRetention, &objective, &objectivePeriod,
		&s.PagingHours.Start, &s.PagingHours.End, &s.PagingHours.Zone, &s.ProductLicence,
		&s.Reachability.TargetReached, &s.Reachability.InstancesReplaceable,
		&s.Reachability.RollbackPathPresent, &s.Reachability.EmissionReadable, &s.Reachability.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return Service{}, fmt.Errorf("%w: %s", ErrNotFound, named)
	} else if err != nil {
		return Service{}, fmt.Errorf("service: reading %s: %w", named, err)
	}
	s.Actor.Kind = record.Kind(kind)
	s.Actor.Basis = record.Basis(basis)
	s.Provisioned.Shape = CredentialShape(shape)
	if s.Provisioned.BranchCredential, err = readRef(branch); err != nil {
		return Service{}, err
	}
	if s.Provisioned.MasterCredential, err = readRef(master); err != nil {
		return Service{}, err
	}
	s.Targets = splitTargets(targets)
	s.Parameters = Parameters{
		WindowSize:       map[gatepolicy.Quantity]gatepolicy.Authored{},
		WindowPower:      map[gatepolicy.Quantity]gatepolicy.Authored{},
		WindowConfidence: authored(confidence),
		WindowCapSeconds: authored(capSeconds),
		WindowLimit:      authored(limit),
		ExposureBound:    authored(exposure),
	}
	s.MutantCap = authored(mutantCap)
	s.FailureRecordKeyCap = authored(keyCap)
	s.UnreliableBound = authored(unreliable)
	s.IncidentItemBoundSeconds = authored(incidentBound)
	s.SnapshotRetentionSeconds = authored(snapshotRetention)
	s.Objective = Objective{Target: authored(objective), PeriodSeconds: authored(objectivePeriod)}
	return s, nil
}

// readRef turns a stored credential name back into a reference, an empty name
// being the reference that names nothing. A stored name the reference type
// refuses is an error and not a silently empty reference: what would follow is a
// clone or a fast-forward reaching for a credential nobody can name.
func readRef(name string) (secretref.Ref, error) {
	if name == "" {
		return secretref.Ref{}, nil
	}
	ref, err := secretref.New(name)
	if err != nil {
		return secretref.Ref{}, fmt.Errorf("service: reading a stored credential name: %w", err)
	}
	return ref, nil
}

func authored(column *float64) gatepolicy.Authored {
	if column == nil {
		return gatepolicy.Authored{}
	}
	return gatepolicy.Authored{Number: *column, Present: true}
}

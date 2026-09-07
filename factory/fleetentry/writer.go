package fleetentry

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// The seven classes of material a fleet entry may name, narrowed from what
// context assembly hands an agent: the intent's statement, reports, an
// owner's answer, the repository, run output, constraints, and an incident's
// copied failure records.
const (
	ClassIntentStatement = "intent_statement"
	ClassReports         = "reports"
	ClassOwnerAnswer     = "owner_answer"
	ClassRepository      = "repository"
	ClassRunOutput       = "run_output"
	ClassConstraints     = "constraints"
	ClassFailureRecords  = "failure_records"
)

// MaterialClasses is every class [New.MaterialClasses] may name. The writer
// refuses a class not in this list, and a class named twice.
var MaterialClasses = []string{
	ClassIntentStatement, ClassReports, ClassOwnerAnswer, ClassRepository,
	ClassRunOutput, ClassConstraints, ClassFailureRecords,
}

var (
	// ErrNotAnOwner is returned for a write by any actor but an owner at
	// Factory: the one writer of a fleet entry, no component in the pipeline
	// writing back to it.
	ErrNotAnOwner = errors.New("fleetentry: the fleet entry's one writer is an owner at Factory")
	// ErrModelVersionEmpty is returned for an entry naming no model version.
	ErrModelVersionEmpty = errors.New("fleetentry: an entry names a model version")
	// ErrRoleEmpty is returned for an entry naming no role.
	ErrRoleEmpty = errors.New("fleetentry: an entry names a role")
	// ErrCredentialNameEmpty is returned for an entry naming no credential.
	ErrCredentialNameEmpty = errors.New("fleetentry: an entry names a credential")
	// ErrProcessingLocationEmpty is returned for an entry naming no
	// processing location.
	ErrProcessingLocationEmpty = errors.New("fleetentry: an entry names a processing location")
	// ErrReadsAtOnceNotPositive is returned for an entry whose reads-at-once
	// is not greater than zero.
	ErrReadsAtOnceNotPositive = errors.New("fleetentry: how much the model reads at once must be positive")
	// ErrDispatchesBetweenEvaluationRunsNotPositive is returned for an entry
	// whose dispatches-between-evaluation-runs is not greater than zero.
	ErrDispatchesBetweenEvaluationRunsNotPositive = errors.New(
		"fleetentry: dispatches between evaluation-set runs must be positive")
	// ErrMaterialClassUnknown is returned for an entry naming a class not in
	// [MaterialClasses].
	ErrMaterialClassUnknown = errors.New("fleetentry: an entry names a material class this package does not define")
	// ErrMaterialClassDuplicate is returned for an entry naming the same
	// class twice.
	ErrMaterialClassDuplicate = errors.New("fleetentry: an entry names a material class twice")
	// ErrNotFound is returned where no entry has the id asked for.
	ErrNotFound = errors.New("fleetentry: no entry has that id")
	// ErrAlreadyWithdrawn is returned by [Writer.Withdraw] for an entry
	// already withdrawn.
	ErrAlreadyWithdrawn = errors.New("fleetentry: this entry is already withdrawn")
)

// Scope is the two halves of a fleet entry's scope: the item's area chain and
// its service's project. Every field may be empty; an entry with all three
// empty is scoped to the whole factory.
type Scope struct {
	ProjectID string
	ServiceID string
	AreaID    string
}

// Entry is one fleet entry as it is stored: a model at an effort in a role
// with a scope, the credential it runs on, the processing location that
// credential resolves to, the classes of material it may be handed, how much
// it reads at once, and how many dispatches pass between evaluation-set
// runs.
type Entry struct {
	ID                              string
	Actor                           record.Actor
	At                              string
	ModelVersion                    string
	Effort                          string
	Role                            string
	Scope                           Scope
	CredentialName                  string
	ProcessingLocation              string
	MaterialClasses                 []string
	ReadsAtOnce                     int64
	DispatchesBetweenEvaluationRuns int64
	WithdrawnAt                     string
}

// InForce reports whether the entry has not been withdrawn.
func (e Entry) InForce() bool { return e.WithdrawnAt == "" }

// New is the writer's input for [Writer.Write]: the nine fields the design
// gives a fleet entry.
type New struct {
	ModelVersion                    string
	Effort                          string
	Role                            string
	Scope                           Scope
	CredentialName                  string
	ProcessingLocation              string
	MaterialClasses                 []string
	ReadsAtOnce                     int64
	DispatchesBetweenEvaluationRuns int64
}

// Writer is the table's one writer: an owner at Factory. It wraps the pool
// and the lease token every write is fenced against.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Write writes one fleet entry, in its own transaction.
func (w *Writer) Write(ctx context.Context, actor record.Actor, n New) (Entry, error) {
	if err := validateActor(actor); err != nil {
		return Entry{}, err
	}
	if n.ModelVersion == "" {
		return Entry{}, ErrModelVersionEmpty
	}
	if n.Role == "" {
		return Entry{}, ErrRoleEmpty
	}
	if n.CredentialName == "" {
		return Entry{}, ErrCredentialNameEmpty
	}
	if n.ProcessingLocation == "" {
		return Entry{}, ErrProcessingLocationEmpty
	}
	if n.ReadsAtOnce <= 0 {
		return Entry{}, ErrReadsAtOnceNotPositive
	}
	if n.DispatchesBetweenEvaluationRuns <= 0 {
		return Entry{}, ErrDispatchesBetweenEvaluationRunsNotPositive
	}
	if err := validateMaterialClasses(n.MaterialClasses); err != nil {
		return Entry{}, err
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Entry{}, fmt.Errorf("fleetentry: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Entry{}, err
	}

	e := Entry{
		ID:                              record.NewID(IDPrefix),
		Actor:                           actor,
		At:                              record.Now(),
		ModelVersion:                    n.ModelVersion,
		Effort:                          n.Effort,
		Role:                            n.Role,
		Scope:                           n.Scope,
		CredentialName:                  n.CredentialName,
		ProcessingLocation:              n.ProcessingLocation,
		MaterialClasses:                 n.MaterialClasses,
		ReadsAtOnce:                     n.ReadsAtOnce,
		DispatchesBetweenEvaluationRuns: n.DispatchesBetweenEvaluationRuns,
	}

	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at,
		 model_version, effort, role, scope_project_id, scope_service_id, scope_area_id,
		 credential_name, processing_location, material_classes, reads_at_once,
		 dispatches_between_evaluation_runs, withdrawn_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, null)`,
		e.ID, FormatVersion, string(e.Actor.Kind), e.Actor.Key, string(e.Actor.Basis), e.At,
		e.ModelVersion, e.Effort, e.Role, e.Scope.ProjectID, e.Scope.ServiceID, e.Scope.AreaID,
		e.CredentialName, e.ProcessingLocation, joinLines(e.MaterialClasses), e.ReadsAtOnce,
		e.DispatchesBetweenEvaluationRuns,
	)
	if err != nil {
		return Entry{}, fmt.Errorf("fleetentry: writing an entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Entry{}, fmt.Errorf("fleetentry: committing: %w", err)
	}
	return e, nil
}

// Withdraw keeps the row and sets withdrawn_at, in its own transaction. An
// entry already withdrawn is refused with [ErrAlreadyWithdrawn].
func (w *Writer) Withdraw(ctx context.Context, actor record.Actor, id string) (Entry, error) {
	if err := validateActor(actor); err != nil {
		return Entry{}, err
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Entry{}, fmt.Errorf("fleetentry: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Entry{}, err
	}

	e, err := scanEntry(tx.QueryRow(ctx, selectEntries+` where id = $1 for update`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return Entry{}, fmt.Errorf("fleetentry: reading entry %s: %w", id, err)
	}
	if !e.InForce() {
		return Entry{}, fmt.Errorf("%w: %s", ErrAlreadyWithdrawn, id)
	}
	e.WithdrawnAt = record.Now()
	if _, err := tx.Exec(ctx, `update `+Table+` set withdrawn_at = $2 where id = $1`, id, e.WithdrawnAt); err != nil {
		return Entry{}, fmt.Errorf("fleetentry: withdrawing entry %s: %w", id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Entry{}, fmt.Errorf("fleetentry: committing: %w", err)
	}
	return e, nil
}

func validateActor(actor record.Actor) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if actor.Kind != record.KindHuman {
		return fmt.Errorf("%w: %s %q", ErrNotAnOwner, actor.Kind, actor.Key)
	}
	return nil
}

func validateMaterialClasses(classes []string) error {
	seen := make(map[string]bool, len(classes))
	for _, c := range classes {
		if !slices.Contains(MaterialClasses, c) {
			return fmt.Errorf("%w: %q", ErrMaterialClassUnknown, c)
		}
		if seen[c] {
			return fmt.Errorf("%w: %q", ErrMaterialClassDuplicate, c)
		}
		seen[c] = true
	}
	return nil
}

// The classes of material are stored one per line, the way agentrun's
// sources and skill version ids are: a class is one of the seven constants
// above, which holds no line ending, so the separator needs no escaping.

func joinLines(values []string) string { return strings.Join(values, "\n") }

func splitLines(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, "\n")
}

const selectEntries = `select id, actor_kind, actor_key, actor_key_basis, at,
	model_version, effort, role, scope_project_id, scope_service_id, scope_area_id,
	credential_name, processing_location, material_classes, reads_at_once,
	dispatches_between_evaluation_runs, withdrawn_at from ` + Table

// rowScanner is what both a pool's QueryRow and a Rows cursor satisfy, so
// scanEntry reads either.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(row rowScanner) (Entry, error) {
	var e Entry
	var kind, basis, materialClasses string
	var withdrawnAt *string
	if err := row.Scan(&e.ID, &kind, &e.Actor.Key, &basis, &e.At,
		&e.ModelVersion, &e.Effort, &e.Role, &e.Scope.ProjectID, &e.Scope.ServiceID, &e.Scope.AreaID,
		&e.CredentialName, &e.ProcessingLocation, &materialClasses, &e.ReadsAtOnce,
		&e.DispatchesBetweenEvaluationRuns, &withdrawnAt); err != nil {
		return Entry{}, err
	}
	e.Actor.Kind, e.Actor.Basis = record.Kind(kind), record.Basis(basis)
	e.MaterialClasses = splitLines(materialClasses)
	if withdrawnAt != nil {
		e.WithdrawnAt = *withdrawnAt
	}
	return e, nil
}

// Get reads one fleet entry by id, through the pool.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Entry, error) {
	e, err := scanEntry(pool.QueryRow(ctx, selectEntries+` where id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return Entry{}, fmt.Errorf("fleetentry: reading entry %s: %w", id, err)
	}
	return e, nil
}

// InForce is every entry not withdrawn, in insertion order.
func InForce(ctx context.Context, pool *pgxpool.Pool) ([]Entry, error) {
	rows, err := pool.Query(ctx, selectEntries+` where withdrawn_at is null order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("fleetentry: reading the entries in force: %w", err)
	}
	defer rows.Close()

	var read []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("fleetentry: reading an entry: %w", err)
		}
		read = append(read, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fleetentry: reading the entries in force: %w", err)
	}
	return read, nil
}

// InForceForRole is every entry not withdrawn naming role, in insertion
// order.
func InForceForRole(ctx context.Context, pool *pgxpool.Pool, role string) ([]Entry, error) {
	rows, err := pool.Query(ctx, selectEntries+` where withdrawn_at is null and role = $1 order by at, id`, role)
	if err != nil {
		return nil, fmt.Errorf("fleetentry: reading the entries in force for role %s: %w", role, err)
	}
	defer rows.Close()

	var read []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("fleetentry: reading an entry: %w", err)
		}
		read = append(read, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fleetentry: reading the entries in force for role %s: %w", role, err)
	}
	return read, nil
}

// CoveredRoles is which roles at least one entry in force names. The
// comparison against the full role list dispatch honours is the composition's,
// since the roles are dispatch's vocabulary and not this package's.
func CoveredRoles(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `select distinct role from `+Table+` where withdrawn_at is null`)
	if err != nil {
		return nil, fmt.Errorf("fleetentry: reading the roles in force: %w", err)
	}
	defer rows.Close()

	covered := map[string]bool{}
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, fmt.Errorf("fleetentry: reading a role: %w", err)
		}
		covered[role] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fleetentry: reading the roles in force: %w", err)
	}
	return covered, nil
}

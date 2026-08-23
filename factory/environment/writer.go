package environment

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
)

// Kind is what an environment is, fixed at creation. Two values are written at
// this milestone; doc.go says which the third is and what writes it.
type Kind string

const (
	// KindProduction is the environment every project has and nobody chooses,
	// because production exists everywhere. Every gate row of the default path
	// reads its threshold from this one.
	KindProduction Kind = "production"
	// KindCandidate is a candidate's own environment, created from master plus
	// that candidate and torn down when the item merges, is dropped, or is
	// superseded by a re-decomposition. It holds nothing an owner authored, being created
	// at the gate that decides its deploy, and its writer is [Candidates] and
	// never [Writer].
	KindCandidate Kind = "candidate"
)

// Kinds is every kind an environment record may have here. The CHECK in [DDL]
// lists the same ones, and TestDDLListsEveryKind fails if the two stop
// agreeing.
var Kinds = []Kind{KindProduction, KindCandidate}

// ProductionName is the name production's record carries. It is a constant
// rather than a caller's choice, production being the one environment an owner
// does not define.
const ProductionName = "production"

var (
	// ErrKindUnknown is returned for a kind outside [Kinds].
	ErrKindUnknown = errors.New("environment: the kind is not one this milestone writes")
	// ErrTargetsEmpty is returned by [Writer.Create] for an environment naming
	// no target. An environment with no address is one no deploy can reach.
	ErrTargetsEmpty = errors.New("environment: an environment names at least one target")
	// ErrNotFound is returned where no environment has the id or the name.
	ErrNotFound = errors.New("environment: no environment has that id")
	// ErrThresholdOutOfRange is returned by [SetGateThreshold] for a threshold
	// outside nothing to one, which is the scale the score's number is on.
	ErrThresholdOutOfRange = errors.New("environment: a gate threshold is between 0 and 1")
	// ErrGateRowEmpty is returned by [SetGateThreshold] for a threshold naming
	// no gate row.
	ErrGateRowEmpty = errors.New("environment: a threshold names the gate row it applies at")
	// ErrNotAnOwnersKind is returned by [Writer.Create] for the candidate kind
	// and by [SetGateThreshold] for a candidate's record. An owner writes the
	// persistent kinds and authors on them; a candidate's environment is the
	// deploy agent's and holds nothing an owner authored, being created at the
	// gate that decides its deploy and so unable to hold the threshold that
	// decided it.
	ErrNotAnOwnersKind = errors.New("environment: a candidate's environment is not an owner's to write or to author on")
)

// Environment is one environment as it is stored.
type Environment struct {
	ID    string
	Actor record.Actor
	At    string
	Kind  Kind
	Name  string
	// Targets are the addresses a deploy is performed against, one per line.
	Targets []string
	// Credential is the reference a deploy is performed with. It is a
	// reference and never a value, so nothing that renders this record
	// renders a secret.
	Credential secretref.Ref
	// ItemID is the item a candidate's environment belongs to, and is empty on
	// a persistent kind. It is the item and not the build, because the
	// environment persists across a rebuild.
	ItemID string
	// ComposedFrom is what the deploy agent put in place beside the candidate,
	// one dependency per entry, and is empty where it put nothing there.
	ComposedFrom []Composed
	// TornDownAt is when the environment was torn down, and is empty while it
	// is live. The row is kept rather than deleted, because the deploy records
	// naming it would otherwise point at nothing.
	TornDownAt string
}

// Live says whether the environment has not been torn down. A persistent kind
// is always live; nothing tears one down.
func (e Environment) Live() bool { return e.TornDownAt == "" }

// Writer is the persistent kinds' one writer: an owner at Factory.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Create writes a persistent environment of kind, with the targets a deploy
// into it is performed against and the credential it is performed with. The
// candidate kind is refused with [ErrNotAnOwnersKind]: the kind is the seam
// between this writer and [Candidates], and neither writes a record of the
// other's.
func (w *Writer) Create(ctx context.Context, actor record.Actor, kind Kind, name string, targets []string, credential secretref.Ref) (Environment, error) {
	if err := actor.Validate(); err != nil {
		return Environment{}, err
	}
	if !contains(Kinds, kind) {
		return Environment{}, fmt.Errorf("%w: %q", ErrKindUnknown, kind)
	}
	if kind == KindCandidate {
		return Environment{}, fmt.Errorf("%w: %q is the deploy agent's to create", ErrNotAnOwnersKind, name)
	}
	if len(targets) == 0 {
		return Environment{}, ErrTargetsEmpty
	}
	if credential.Name() == "" {
		return Environment{}, fmt.Errorf("environment: %s names no credential", name)
	}

	e := Environment{
		ID:         record.NewID(IDPrefix),
		Actor:      actor,
		At:         record.Now(),
		Kind:       kind,
		Name:       name,
		Targets:    targets,
		Credential: credential,
	}
	if err := insert(ctx, w.pool, e); err != nil {
		return Environment{}, err
	}
	return e, nil
}

// insert writes one environment row, whichever of the two writers composed it.
func insert(ctx context.Context, pool *pgxpool.Pool, e Environment) error {
	_, err := pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, kind, name, targets, credential, item_id, composed_from, torn_down_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		e.ID, string(e.Actor.Kind), e.Actor.Name, e.At,
		string(e.Kind), e.Name, joinTargets(e.Targets), e.Credential.Name(),
		e.ItemID, joinComposed(e.ComposedFrom), e.TornDownAt,
	)
	if err != nil {
		return fmt.Errorf("environment: creating %q: %w", e.Name, err)
	}
	return nil
}

// SetGateThreshold writes the threshold an owner authored for one gate row on
// one environment, inside tx. Its one caller is package policy, which calls it
// inside the transaction that appends the policy version, so the row and the
// version commit together or not at all.
//
// Re-authoring is one row: the insert conflicts on the environment and the gate
// row and updates the threshold, leaving the row's actor and time at the first
// authoring. What that costs is that who last moved a threshold is not on the
// row; what says it is the policy version the write appended.
func SetGateThreshold(ctx context.Context, tx pgx.Tx, actor record.Actor, environmentID, gateRow string, threshold float64) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if gateRow == "" {
		return ErrGateRowEmpty
	}
	if threshold < 0 || threshold > 1 {
		return fmt.Errorf("%w: %v", ErrThresholdOutOfRange, threshold)
	}
	// The kind is read inside the same transaction as the write, because what
	// makes a candidate's record unauthorable is the record and not the caller:
	// an owner naming one is refused here rather than left to a reader to notice
	// a threshold nothing will ever compare against.
	var kind string
	err := tx.QueryRow(ctx, `select kind from `+Table+` where id = $1`, environmentID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, environmentID)
	} else if err != nil {
		return fmt.Errorf("environment: reading the kind of %s: %w", environmentID, err)
	}
	if Kind(kind) == KindCandidate {
		return fmt.Errorf("%w: %s is a candidate's", ErrNotAnOwnersKind, environmentID)
	}
	_, err = tx.Exec(ctx, `insert into `+ThresholdTable+`
		(id, actor_kind, actor_name, at, environment_id, gate_row, threshold)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (environment_id, gate_row) do update set threshold = excluded.threshold`,
		record.NewID(ThresholdIDPrefix), string(actor.Kind), actor.Name, record.Now(),
		environmentID, gateRow, threshold,
	)
	if err != nil {
		return fmt.Errorf("environment: authoring the %s threshold on %s: %w", gateRow, environmentID, err)
	}
	return nil
}

// GateThreshold is the threshold an owner authored for one gate row on one
// environment, and absent where they authored none — where the value in force is
// what the score supplies.
func GateThreshold(ctx context.Context, pool *pgxpool.Pool, environmentID, gateRow string) (gatepolicy.Authored, error) {
	var threshold float64
	err := pool.QueryRow(ctx, `select threshold from `+ThresholdTable+`
		where environment_id = $1 and gate_row = $2`, environmentID, gateRow).Scan(&threshold)
	if errors.Is(err, pgx.ErrNoRows) {
		return gatepolicy.Authored{}, nil
	} else if err != nil {
		return gatepolicy.Authored{}, fmt.Errorf("environment: reading the %s threshold of %s: %w", gateRow, environmentID, err)
	}
	return gatepolicy.Authored{Number: threshold, Present: true}, nil
}

const selectEnvironment = `select id, actor_kind, actor_name, at, kind, name, targets, credential,
	item_id, composed_from, torn_down_at
	from ` + Table

// Get is one environment by id. It takes the pool and not a [Writer], because
// reading an environment is not a reason to be handed the thing that creates
// them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Environment, error) {
	return scan(pool.QueryRow(ctx, selectEnvironment+` where id = $1`, id), id)
}

// ByName is the environment of that name, and false where none has it.
func ByName(ctx context.Context, pool *pgxpool.Pool, name string) (Environment, bool, error) {
	e, err := scan(pool.QueryRow(ctx, selectEnvironment+` where name = $1`, name), name)
	if errors.Is(err, ErrNotFound) {
		return Environment{}, false, nil
	} else if err != nil {
		return Environment{}, false, err
	}
	return e, true, nil
}

// ForItem is the candidate environment of one item, and false where the item has
// none — which is every item until the candidate deploy gate approves. A
// torn-down one is still returned, the row being kept: a caller that wants a
// place to deploy into reads [Environment.Live] on what comes back.
func ForItem(ctx context.Context, pool *pgxpool.Pool, itemID string) (Environment, bool, error) {
	if itemID == "" {
		return Environment{}, false, nil
	}
	e, err := scan(pool.QueryRow(ctx, selectEnvironment+` where item_id = $1`, itemID), itemID)
	if errors.Is(err, ErrNotFound) {
		return Environment{}, false, nil
	} else if err != nil {
		return Environment{}, false, err
	}
	return e, true, nil
}

// CountLiveCandidates is how many candidate environments have not been torn
// down. It is what the substrate's own ceiling is compared against, and that
// ceiling is a number in the source of whatever composes the deploy agent
// rather than a parameter an owner authors — doc.go says why.
func CountLiveCandidates(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var count int
	err := pool.QueryRow(ctx, `select count(*) from `+Table+`
		where kind = $1 and torn_down_at = ''`, string(KindCandidate)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("environment: counting the live candidate environments: %w", err)
	}
	return count, nil
}

func scan(row pgx.Row, named string) (Environment, error) {
	var e Environment
	var kind, actorKind, targets, credential, composed string
	err := row.Scan(&e.ID, &actorKind, &e.Actor.Name, &e.At, &kind, &e.Name, &targets, &credential,
		&e.ItemID, &composed, &e.TornDownAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, fmt.Errorf("%w: %s", ErrNotFound, named)
	} else if err != nil {
		return Environment{}, fmt.Errorf("environment: reading %s: %w", named, err)
	}
	e.Actor.Kind = record.Kind(actorKind)
	e.Kind = Kind(kind)
	e.Targets = splitTargets(targets)
	ref, err := secretref.New(credential)
	if err != nil {
		return Environment{}, fmt.Errorf("environment: the credential stored on %s: %w", named, err)
	}
	e.Credential = ref
	e.ComposedFrom, err = splitComposed(composed)
	if err != nil {
		return Environment{}, fmt.Errorf("environment: what %s was composed from: %w", named, err)
	}
	return e, nil
}

func contains(kinds []Kind, kind Kind) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

package score

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// ErrNoVersion is returned by [Get] where no version has the id.
var ErrNoVersion = errors.New("score: no score version has that id")

// Version is one score version as it is stored: what the score published at
// the moment every decision naming it was decided.
type Version struct {
	ID             string
	Actor          record.Actor
	At             string
	FormulaVersion string
	Formula        string
	FactorSet      string
	// Rules is the published rules by which each supplied value moves, which is
	// what an owner disagreeing with a moved value argues with.
	Rules string
	// Supplied is every value the score supplies: the starting value of each
	// parameter and a row per subject an outcome has moved it for.
	Supplied SuppliedValues
	// Supersedes is the version this one replaced, and is empty on the first.
	Supersedes string
}

// Value is what this version supplies for one parameter on one subject. It is the
// read package policy makes: the value in force is what an owner authored where
// they authored one and what the version in force supplies otherwise, clamped by
// any safeguard.
//
// A zero version answers the starting values, which is the answer for a factory
// that has appended no version yet — so a factory with an empty table still runs
// on the numbers the formula was calibrated at rather than on nothing.
func (v Version) Value(p gatepolicy.Parameter, subject string) (Supplied, bool) {
	return v.Supplied.Value(p, subject)
}

// Writer appends score versions. There is no method that edits one: the table
// is append-only, which is what makes a decision naming a version a decision
// readable against what that version said.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer { return &Writer{pool: pool, token: token} }

// Ensure is the version in force: the newest stored version where it still says
// what this source publishes and what the outcomes in the store supply, and a
// freshly appended one naming it as its predecessor where it does not. So a change
// to the formula, the factor set, the rules, or any supplied value moves the
// version by the ordinary path, and starting the factory twice over an unchanged
// store appends nothing.
//
// This is where the score learns. The learning is a pass over records that already
// exist and never a write at a firing: an outcome arrives long after the decision
// it judges, so nothing at a gate could have computed it, and a version that moved
// mid-process would leave two decisions of one run naming different numbers. What
// that costs is that a run acts on what the store said when it started, and an
// outcome that arrives during a run is learned from by the next one.
//
// The whole of it runs under [AdvisoryLockKey], so two processes ensuring at once
// append one version and not two: the read of the newest and the append that
// supersedes it are one step, which is what nothing in the schema can enforce —
// two versions saying the same thing are legitimate where they are not adjacent.
func (w *Writer) Ensure(ctx context.Context, actor record.Actor) (Version, error) {
	if err := actor.Validate(); err != nil {
		return Version{}, err
	}

	supplied, err := Learn(ctx, w.pool, w.token)
	if err != nil {
		return Version{}, err
	}
	stored, err := json.Marshal(supplied)
	if err != nil {
		return Version{}, fmt.Errorf("score: encoding the supplied values: %w", err)
	}

	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Version{}, fmt.Errorf("score: beginning the version read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Version{}, err
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, AdvisoryLockKey()); err != nil {
		return Version{}, fmt.Errorf("score: taking the version lock: %w", err)
	}

	newest, storedNewest, err := scanVersion(tx.QueryRow(ctx, selectVersion+` order by at desc, id desc limit 1`), "the newest")
	if err != nil && !errors.Is(err, ErrNoVersion) {
		return Version{}, err
	}
	if err == nil && newest.FormulaVersion == FormulaVersion && newest.Formula == Formula &&
		newest.FactorSet == FactorSet() && newest.Rules == Rules && storedNewest == string(stored) {
		return newest, nil
	}

	v := Version{
		ID:             record.NewID(IDPrefix),
		Actor:          actor,
		At:             record.Now(),
		FormulaVersion: FormulaVersion,
		Formula:        Formula,
		FactorSet:      FactorSet(),
		Rules:          Rules,
		Supplied:       supplied,
		Supersedes:     newest.ID,
	}
	if _, err := tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, formula_version, formula, factor_set, rules, supplied, supersedes)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		v.ID, FormatVersion, string(v.Actor.Kind), v.Actor.Key, string(v.Actor.Basis), v.At,
		v.FormulaVersion, v.Formula, v.FactorSet, v.Rules, string(stored), v.Supersedes,
	); err != nil {
		return Version{}, fmt.Errorf("score: appending a version of %s: %w", FormulaVersion, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, fmt.Errorf("score: committing version %s: %w", v.ID, err)
	}
	return v, nil
}

const selectVersion = `select id, actor_kind, actor_key, actor_key_basis, at, formula_version, formula,
	factor_set, rules, supplied, supersedes
	from ` + Table

// Newest is the version in force, and false where none has been appended. The
// order is the timestamp with the id breaking a tie, the id ordering nothing on
// its own.
func Newest(ctx context.Context, pool *pgxpool.Pool) (Version, bool, error) {
	v, _, err := scanVersion(pool.QueryRow(ctx, selectVersion+` order by at desc, id desc limit 1`), "the newest")
	if errors.Is(err, ErrNoVersion) {
		return Version{}, false, nil
	} else if err != nil {
		return Version{}, false, err
	}
	return v, true, nil
}

// Get is one version by id, which is what a reader of a decision follows to
// what the score published when it was decided.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Version, error) {
	v, _, err := scanVersion(pool.QueryRow(ctx, selectVersion+` where id = $1`, id), id)
	return v, err
}

// All is every version, oldest first. It is what a reader following a supplied
// value's movement walks: each names the one it superseded, so the sequence is
// readable from either end, and what makes a movement readable beside it is every
// decision naming the version it was decided under.
func All(ctx context.Context, pool *pgxpool.Pool) ([]Version, error) {
	rows, err := pool.Query(ctx, selectVersion+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("score: reading every version: %w", err)
	}
	defer rows.Close()

	var read []Version
	for rows.Next() {
		v, _, err := scanVersion(rows, "a version")
		if err != nil {
			return nil, err
		}
		read = append(read, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("score: reading every version: %w", err)
	}
	return read, nil
}

// scanVersion reads one row and returns the supplied values both decoded and as
// they were stored. The stored text is what [Writer.Ensure] compares against, and
// comparing the decoded tables instead would make two tables that encode
// differently read as one.
func scanVersion(row pgx.Row, named string) (Version, string, error) {
	var v Version
	var kind, basis, supplied string
	err := row.Scan(&v.ID, &kind, &v.Actor.Key, &basis, &v.At, &v.FormulaVersion,
		&v.Formula, &v.FactorSet, &v.Rules, &supplied, &v.Supersedes)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, "", fmt.Errorf("%w: %s", ErrNoVersion, named)
	} else if err != nil {
		return Version{}, "", fmt.Errorf("score: reading %s version: %w", named, err)
	}
	v.Actor.Kind = record.Kind(kind)
	v.Actor.Basis = record.Basis(basis)
	if err := json.Unmarshal([]byte(supplied), &v.Supplied); err != nil {
		return Version{}, "", fmt.Errorf("score: reading the supplied values of %s: %w", v.ID, err)
	}
	return v, supplied, nil
}

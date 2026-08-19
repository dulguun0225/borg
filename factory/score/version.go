package score

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
	Supplied       string
	// Supersedes is the version this one replaced, and is empty on the first.
	Supersedes string
}

// Writer appends score versions. There is no method that edits one: the table
// is append-only, which is what makes a decision naming a version a decision
// readable against what that version said.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Ensure is the version in force: the newest stored version where it still says
// what this source publishes, and a freshly appended one naming it as its
// predecessor where it does not. So a change to the formula, the factor set, or
// a supplied value moves the version by the ordinary path, and starting the
// factory twice on unchanged source appends nothing.
//
// The whole of it runs under [AdvisoryLockKey], so two processes ensuring at once
// append one version and not two: the read of the newest and the append that
// supersedes it are one step, which is what nothing in the schema can enforce —
// two versions saying the same thing are legitimate where they are not adjacent.
func (w *Writer) Ensure(ctx context.Context, actor record.Actor) (Version, error) {
	if err := actor.Validate(); err != nil {
		return Version{}, err
	}

	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Version{}, fmt.Errorf("score: beginning the version read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, AdvisoryLockKey()); err != nil {
		return Version{}, fmt.Errorf("score: taking the version lock: %w", err)
	}

	newest, err := scanVersion(tx.QueryRow(ctx, selectVersion+` order by at desc, id desc limit 1`), "the newest")
	if err != nil && !errors.Is(err, ErrNoVersion) {
		return Version{}, err
	}
	if err == nil && newest.FormulaVersion == FormulaVersion && newest.Formula == Formula &&
		newest.FactorSet == FactorSet() && newest.Supplied == SuppliedText() {
		return newest, nil
	}

	v := Version{
		ID:             record.NewID(IDPrefix),
		Actor:          actor,
		At:             record.Now(),
		FormulaVersion: FormulaVersion,
		Formula:        Formula,
		FactorSet:      FactorSet(),
		Supplied:       SuppliedText(),
		Supersedes:     newest.ID,
	}
	if _, err := tx.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, formula_version, formula, factor_set, supplied, supersedes)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		v.ID, string(v.Actor.Kind), v.Actor.Name, v.At,
		v.FormulaVersion, v.Formula, v.FactorSet, v.Supplied, v.Supersedes,
	); err != nil {
		return Version{}, fmt.Errorf("score: appending a version of %s: %w", FormulaVersion, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, fmt.Errorf("score: committing version %s: %w", v.ID, err)
	}
	return v, nil
}

const selectVersion = `select id, actor_kind, actor_name, at, formula_version, formula,
	factor_set, supplied, supersedes
	from ` + Table

// Newest is the version in force, and false where none has been appended. The
// order is the timestamp with the id breaking a tie, the id ordering nothing on
// its own.
func Newest(ctx context.Context, pool *pgxpool.Pool) (Version, bool, error) {
	v, err := scanVersion(pool.QueryRow(ctx, selectVersion+` order by at desc, id desc limit 1`), "the newest")
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
	return scanVersion(pool.QueryRow(ctx, selectVersion+` where id = $1`, id), id)
}

func scanVersion(row pgx.Row, named string) (Version, error) {
	var v Version
	var kind string
	err := row.Scan(&v.ID, &kind, &v.Actor.Name, &v.At, &v.FormulaVersion,
		&v.Formula, &v.FactorSet, &v.Supplied, &v.Supersedes)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, fmt.Errorf("%w: %s", ErrNoVersion, named)
	} else if err != nil {
		return Version{}, fmt.Errorf("score: reading %s version: %w", named, err)
	}
	v.Actor.Kind = record.Kind(kind)
	return v, nil
}

package criterion

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrMutationRunMismatch is returned by [RecordMutation] for a run outside
	// the candidate environment. The deployer mutates the build at the
	// candidate run and numbers its runs from 1; the build's own process
	// mutates nothing.
	ErrMutationRunMismatch = errors.New("criterion: a mutation is read at a run on the candidate environment")
	// ErrMutationCountsMismatch is returned by [RecordMutation] for a reading
	// whose counts disagree with its could-not-derive: a derivation that could
	// not be made counts nothing, and one that was made tested at least one
	// mutant.
	ErrMutationCountsMismatch = errors.New("criterion: the mutants counted disagree with the could-not-derive")
)

// MutationReading is one mutation score as it is stored: the build and the run
// it was read on, and the reading itself. Its identity is the build and the
// run, so the id is the row's and never what anything points at.
type MutationReading struct {
	ID       string
	Actor    record.Actor
	At       string
	BuildID  string
	Run      int
	Mutation Mutation
}

// RecordMutation writes what mutating one run of one build produced, beside
// that run's criteria results. Its caller is the deployer, when the mutation
// finishes: what is written is what was read, and what is decided is decided at
// the Merge to master gate, where the mutation floor is.
//
// Nothing is updated: a second run over one build is a new run and so a new
// row, which is the arrangement [InsertResults] already has for the results the
// same run wrote.
func RecordMutation(ctx context.Context, pool *pgxpool.Pool, token lease.Token,
	actor record.Actor, run Run, m Mutation,
) (MutationReading, error) {
	if err := actor.Validate(); err != nil {
		return MutationReading{}, err
	}
	if run.BuildID == "" {
		return MutationReading{}, ErrBuildIDEmpty
	}
	if run.Place != PlaceCandidateEnvironment || run.Number < 1 {
		return MutationReading{}, fmt.Errorf("%w: place %q at run %d", ErrMutationRunMismatch, run.Place, run.Number)
	}
	if m.Derived() != (m.MutantsTested >= 1) || (!m.Derived() && m.MutantsDetected != 0) {
		return MutationReading{}, fmt.Errorf("%w: %d of %d tested, could not derive %q",
			ErrMutationCountsMismatch, m.MutantsDetected, m.MutantsTested, m.CouldNotDerive)
	}
	if m.MutantsDetected > m.MutantsTested {
		return MutationReading{}, fmt.Errorf("%w: %d detected of %d tested",
			ErrMutationCountsMismatch, m.MutantsDetected, m.MutantsTested)
	}

	reading := MutationReading{
		ID:       record.NewID(MutationIDPrefix),
		Actor:    actor,
		At:       record.Now(),
		BuildID:  run.BuildID,
		Run:      run.Number,
		Mutation: m,
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return MutationReading{}, fmt.Errorf("criterion: beginning the mutation of build %s: %w", run.BuildID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, token); err != nil {
		return MutationReading{}, err
	}
	if _, err := tx.Exec(ctx, `insert into `+MutationTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, build_id, run,
		toolchain, tool, coverage, mutants_tested, mutants_detected, could_not_derive)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		reading.ID, FormatVersionMutation, string(actor.Kind), actor.Key, string(actor.Basis), reading.At,
		reading.BuildID, reading.Run, m.Toolchain, m.Tool, m.Coverage,
		m.MutantsTested, m.MutantsDetected, m.CouldNotDerive,
	); err != nil {
		return MutationReading{}, fmt.Errorf("criterion: recording the mutation of build %s run %d: %w",
			run.BuildID, run.Number, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return MutationReading{}, fmt.Errorf("criterion: committing the mutation of build %s: %w", run.BuildID, err)
	}
	return reading, nil
}

const selectMutation = `select id, actor_kind, actor_key, actor_key_basis, at, build_id, run,
	toolchain, tool, coverage, mutants_tested, mutants_detected, could_not_derive
	from ` + MutationTable

// LatestMutation is what the Merge to master gate reads: the mutation of the
// build's highest run, and false where the build was never mutated — a build
// nothing mutated is not a reading of zero, and the gate resolves it the way it
// resolves an absent input.
func LatestMutation(ctx context.Context, pool *pgxpool.Pool, buildID string) (MutationReading, bool, error) {
	reading, err := scanMutation(pool.QueryRow(ctx,
		selectMutation+` where build_id = $1 order by run desc limit 1`, buildID))
	if errors.Is(err, pgx.ErrNoRows) {
		return MutationReading{}, false, nil
	} else if err != nil {
		return MutationReading{}, false, fmt.Errorf("criterion: reading the mutation of build %s: %w", buildID, err)
	}
	return reading, true, nil
}

// MutationsForBuild is every mutation of one build, run by run, in run order.
// A repeated run is a second reading and not a rewrite of the first, so what an
// earlier run read survives here.
func MutationsForBuild(ctx context.Context, pool *pgxpool.Pool, buildID string) ([]MutationReading, error) {
	rows, err := pool.Query(ctx, selectMutation+` where build_id = $1 order by run, at`, buildID)
	if err != nil {
		return nil, fmt.Errorf("criterion: reading the mutations of build %s: %w", buildID, err)
	}
	defer rows.Close()

	var read []MutationReading
	for rows.Next() {
		reading, err := scanMutation(rows)
		if err != nil {
			return nil, fmt.Errorf("criterion: reading a mutation of build %s: %w", buildID, err)
		}
		read = append(read, reading)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("criterion: reading the mutations of build %s: %w", buildID, err)
	}
	return read, nil
}

// scanMutation reads one row of [selectMutation] into a [MutationReading].
func scanMutation(row pgx.Row) (MutationReading, error) {
	var r MutationReading
	var kind, basis string
	if err := row.Scan(&r.ID, &kind, &r.Actor.Key, &basis, &r.At, &r.BuildID, &r.Run,
		&r.Mutation.Toolchain, &r.Mutation.Tool, &r.Mutation.Coverage,
		&r.Mutation.MutantsTested, &r.Mutation.MutantsDetected, &r.Mutation.CouldNotDerive); err != nil {
		return MutationReading{}, err
	}
	r.Actor.Kind = record.Kind(kind)
	r.Actor.Basis = record.Basis(basis)
	return r, nil
}

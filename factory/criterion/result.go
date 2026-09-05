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

// Outcome is what deciding one criterion produced. There are three and the
// third is not a kind of pass — and the third is never stored: what is written
// is what was observed, and undecided is computed at the read by [Undecided].
type Outcome string

const (
	// OutcomePassed is an encoding that passed.
	OutcomePassed Outcome = "passed"
	// OutcomeFailed is an encoding that failed.
	OutcomeFailed Outcome = "failed"
	// OutcomeUndecided is an encoding that produced a failure and a pass over
	// two runs of one build whose compositions match. What a repeated run
	// measures is the repetition, so the criterion is undecided for that build
	// rather than passed — recording it as a pass is what would make the merge
	// verdict rest on nothing, a passing criterion being the whole of what that
	// gate reads about the item's own behaviour. It is derived by [Undecided]
	// and refused by the writers, which store observations alone.
	OutcomeUndecided Outcome = "undecided"
)

// Outcomes is every outcome a criterion may have at a gate, the derived one
// included.
var Outcomes = []Outcome{OutcomePassed, OutcomeFailed, OutcomeUndecided}

// Observed is every outcome a run may record. The CHECK constraint in [DDL]
// lists the same two, and TestDDLListsEveryObservedOutcome fails if the two
// stop agreeing.
var Observed = []Outcome{OutcomePassed, OutcomeFailed}

// Place is which of the two places decided a criterion, declared by its
// encoding and carried onto every result the run wrote.
type Place string

const (
	// PlaceBuild is the build's own process: an encoding deciding a criterion
	// over the code alone, run by the build runner as it performs the build,
	// so the Implementation gate reads the result before any environment
	// exists.
	PlaceBuild Place = "build"
	// PlaceCandidateEnvironment is a run on the item's candidate environment,
	// which decides every encoding declaring it.
	PlaceCandidateEnvironment Place = "candidate_environment"
)

// Places is every place an encoding may declare. The CHECK constraint in [DDL]
// lists the same two.
var Places = []Place{PlaceBuild, PlaceCandidateEnvironment}

// Blocks says whether the outcome stops the item at the Merge to master gate.
// Undecided is read there the way a failure is, which is the whole reason the
// value exists. It takes whether the criterion is unreliable, because while a
// criterion is unreliable its failure rejects nothing, counts no attempt, and
// moves no prior, and Merge to master reads it as absent — its result is still
// recorded, which is why the exception is here and not in the writer.
func (o Outcome) Blocks(unreliable bool) bool {
	if unreliable {
		return false
	}
	return o != OutcomePassed
}

var (
	// ErrBuildIDEmpty is returned by the result writers for a result naming no
	// build. record's doc.go states what a link is checked for.
	ErrBuildIDEmpty = errors.New("criterion: the build id is empty")
	// ErrOutcomeUnknown is returned for an outcome outside [Outcomes].
	ErrOutcomeUnknown = errors.New("criterion: the outcome is none of passed, failed, undecided")
	// ErrOutcomeNotObserved is returned for a result recorded as undecided.
	// Undecided is a disagreement between two runs and no run observes one, so
	// it is derived by [Undecided] and never written.
	ErrOutcomeNotObserved = errors.New("criterion: undecided is derived at the read and never recorded")
	// ErrPlaceUnknown is returned for a run whose place is neither of [Places].
	ErrPlaceUnknown = errors.New("criterion: the place is neither the build nor the candidate environment")
	// ErrRunMismatch is returned for a run whose number disagrees with its
	// place: the build's own process is run 0, and the deployer numbers a
	// candidate environment's runs from 1.
	ErrRunMismatch = errors.New("criterion: the run number disagrees with the place")
	// ErrCompositionMismatch is returned for a run whose composition disagrees
	// with its place: a run on a candidate environment carries the composition
	// in force at the run, and the build's own process has no environment and
	// carries none.
	ErrCompositionMismatch = errors.New("criterion: the composition disagrees with the place")
)

// Run is one run of the encodings over one build: the build, the number the
// run has among that build's runs, where it happened, and the composition in
// force at it.
//
// The composition is copied per run and not per build, because that field is
// rewritten at a recomposition and what an earlier run ran against survives on
// the result or nowhere. Comparing two runs' copies is the whole of how
// [Undecided] tells a disagreement from two answers to two questions.
type Run struct {
	BuildID string
	// Number is the run's number among the build's runs, given by the deployer
	// in the order it performed them, and 0 for the build's own process.
	Number int
	Place  Place
	// Composition is what the environment was composed from at the run, and is
	// empty for the build's own process.
	Composition string
}

// Result is one criterion decided by one run of one build. Its identity is the
// build, the run, and the criterion, which is why this is a table of those
// three and not a record anything points at.
type Result struct {
	ID          string
	Actor       record.Actor
	At          string
	BuildID     string
	Run         int
	CriterionID string
	Outcome     Outcome
	Place       Place
	Composition string
}

// InsertResults writes what one run decided, one row per criterion, inside tx.
// It is what both writers of results share: the build runner calls it inside
// the transaction that writes the build, so what the build's own process
// decided commits with the build or not at all, and [RecordResults] wraps it
// for the deployer, which has no other write to join.
//
// Nothing is updated: a second run over one build is a new run and so new
// rows, and the disagreement [Undecided] computes is what would be gone if the
// second overwrote the first.
func InsertResults(ctx context.Context, tx pgx.Tx, actor record.Actor, run Run, results map[string]Outcome) error {
	if err := refuseRun(actor, run, results); err != nil {
		return err
	}
	for criterionID, outcome := range results {
		_, err := tx.Exec(ctx, `insert into `+ResultTable+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, build_id, run, criterion_id, outcome, place, composition)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			record.NewID(ResultIDPrefix), FormatVersionResult,
			string(actor.Kind), actor.Key, string(actor.Basis), record.Now(),
			run.BuildID, run.Number, criterionID, string(outcome), string(run.Place), run.Composition,
		)
		if err != nil {
			return fmt.Errorf("criterion: recording %s over build %s run %d: %w",
				criterionID, run.BuildID, run.Number, err)
		}
	}
	return nil
}

// RecordResults writes what a run on a candidate environment produced, in one
// transaction fenced once at the top, so a caller whose lease has lapsed
// writes none of the rows rather than some. Its caller is the deployer, when
// the run finishes: what is written is what was observed, and what is decided
// is decided at the gate.
func RecordResults(ctx context.Context, pool *pgxpool.Pool, token lease.Token, actor record.Actor, run Run, results map[string]Outcome) error {
	if err := refuseRun(actor, run, results); err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("criterion: beginning the recording over build %s: %w", run.BuildID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if err := InsertResults(ctx, tx, actor, run, results); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("criterion: committing the recording over build %s: %w", run.BuildID, err)
	}
	return nil
}

func refuseRun(actor record.Actor, run Run, results map[string]Outcome) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if run.BuildID == "" {
		return ErrBuildIDEmpty
	}
	switch run.Place {
	case PlaceBuild:
		if run.Number != 0 {
			return fmt.Errorf("%w: the build's own process is run 0, not %d", ErrRunMismatch, run.Number)
		}
		if run.Composition != "" {
			return fmt.Errorf("%w: the build's own process has no environment", ErrCompositionMismatch)
		}
	case PlaceCandidateEnvironment:
		if run.Number < 1 {
			return fmt.Errorf("%w: a run on a candidate environment is numbered from 1, not %d", ErrRunMismatch, run.Number)
		}
		if run.Composition == "" {
			return fmt.Errorf("%w: a run on a candidate environment carries the composition it ran against", ErrCompositionMismatch)
		}
	default:
		return fmt.Errorf("%w: %q", ErrPlaceUnknown, run.Place)
	}
	for criterionID, outcome := range results {
		if criterionID == "" {
			return fmt.Errorf("%w: a result of build %s", ErrCriterionIDEmpty, run.BuildID)
		}
		if outcome == OutcomeUndecided {
			return fmt.Errorf("%w: %s over build %s", ErrOutcomeNotObserved, criterionID, run.BuildID)
		}
		if !contains(Observed, outcome) {
			return fmt.Errorf("%w: %q", ErrOutcomeUnknown, outcome)
		}
	}
	return nil
}

const selectResult = `select id, actor_kind, actor_key, actor_key_basis, at, build_id, run, criterion_id, outcome, place, composition
	from ` + ResultTable

// ResultsForBuild is every result of one build, every run of it included, in
// run order. Every earlier run stands with the composition it was decided
// against, which is what [Undecided], the queue's three readings, and the
// criterion's outcome history each compare.
func ResultsForBuild(ctx context.Context, pool *pgxpool.Pool, buildID string) ([]Result, error) {
	return readResults(ctx, pool, buildID,
		selectResult+` where build_id = $1 order by run, at, criterion_id`, buildID)
}

// Latest is what a gate reads: per criterion, the row of that criterion's
// highest run. It is per criterion and not the highest run of the build,
// because a criterion the build's own process decided has one row at run 0 and
// no later one — a build that also ran on its candidate environment would
// otherwise report nothing about it.
func Latest(ctx context.Context, pool *pgxpool.Pool, buildID string) ([]Result, error) {
	return readResults(ctx, pool, buildID,
		`select distinct on (criterion_id) id, actor_kind, actor_key, actor_key_basis, at,
			build_id, run, criterion_id, outcome, place, composition
		from `+ResultTable+` where build_id = $1 order by criterion_id, run desc`, buildID)
}

// Undecided is every criterion of the build whose runs disagree: two runs over
// one build whose compositions match and whose outcomes differ. Two runs
// against compositions that differ are two answers to two questions and make
// nothing undecided, so the composition copied onto each row is what the
// grouping is by.
//
// A criterion the build's own process decided cannot be undecided: it is
// decided once, in the build, and an encoding of that kind that cannot hold its
// answer shows only as two builds disagreeing.
func Undecided(ctx context.Context, pool *pgxpool.Pool, buildID string) ([]string, error) {
	rows, err := pool.Query(ctx, `select distinct criterion_id from `+ResultTable+`
		where build_id = $1 and place = $2
		group by criterion_id, composition
		having count(distinct outcome) > 1
		order by criterion_id`, buildID, string(PlaceCandidateEnvironment))
	if err != nil {
		return nil, fmt.Errorf("criterion: reading what is undecided over build %s: %w", buildID, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("criterion: reading an undecided criterion of build %s: %w", buildID, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("criterion: reading what is undecided over build %s: %w", buildID, err)
	}
	return ids, nil
}

func readResults(ctx context.Context, pool *pgxpool.Pool, buildID, sql string, args ...any) ([]Result, error) {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("criterion: reading what was decided over build %s: %w", buildID, err)
	}
	defer rows.Close()

	var read []Result
	for rows.Next() {
		var r Result
		var kind, basis, outcome, place string
		if err := rows.Scan(&r.ID, &kind, &r.Actor.Key, &basis, &r.At, &r.BuildID, &r.Run,
			&r.CriterionID, &outcome, &place, &r.Composition); err != nil {
			return nil, fmt.Errorf("criterion: reading a result of build %s: %w", buildID, err)
		}
		r.Actor.Kind = record.Kind(kind)
		r.Actor.Basis = record.Basis(basis)
		r.Outcome = Outcome(outcome)
		r.Place = Place(place)
		read = append(read, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("criterion: reading what was decided over build %s: %w", buildID, err)
	}
	return read, nil
}

func contains(outcomes []Outcome, outcome Outcome) bool {
	for _, o := range outcomes {
		if o == outcome {
			return true
		}
	}
	return false
}

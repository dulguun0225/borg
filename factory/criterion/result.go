package criterion

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Outcome is what deciding one criterion against one build produced. There are
// three and the third is not a kind of pass.
type Outcome string

const (
	// OutcomePassed is an encoding that passed.
	OutcomePassed Outcome = "passed"
	// OutcomeFailed is an encoding that failed.
	OutcomeFailed Outcome = "failed"
	// OutcomeUndecided is an encoding that produced a failure and a pass over
	// the same build. What a repeated run measures is the repetition, so the
	// criterion is undecided for that build rather than passed — recording it as
	// a pass is what would make the merge verdict rest on nothing, a passing
	// criterion being the whole of what that gate reads about the item's own
	// behaviour.
	OutcomeUndecided Outcome = "undecided"
)

// Outcomes is every outcome a result may have. The CHECK constraint in [DDL]
// lists the same three, and TestDDLListsEveryOutcome fails if the two stop
// agreeing.
var Outcomes = []Outcome{OutcomePassed, OutcomeFailed, OutcomeUndecided}

// Blocks says whether the outcome stops the item at the Merge to master gate. Undecided is
// read there the way a failure is, which is the whole reason the value exists.
func (o Outcome) Blocks() bool { return o != OutcomePassed }

// Decide is the outcome of two runs of one encoding over one build: what they
// agree on, and undecided where they disagree. The second run is the only thing
// that can produce undecided, and running twice is what it costs.
func Decide(first, second bool) Outcome {
	switch {
	case first != second:
		return OutcomeUndecided
	case first:
		return OutcomePassed
	default:
		return OutcomeFailed
	}
}

var (
	// ErrBuildIDEmpty is returned by [RecordResults] for a result naming no
	// build. record's doc.go states what a link is checked for.
	ErrBuildIDEmpty = errors.New("criterion: the build id is empty")
	// ErrOutcomeUnknown is returned by [RecordResults] for an outcome outside
	// [Outcomes].
	ErrOutcomeUnknown = errors.New("criterion: the outcome is none of passed, failed, undecided")
	// ErrCriterionIDEmpty is returned by [RecordResults] for a result naming no
	// criterion. The pair is the identity, so half of it is not a row.
	ErrCriterionIDEmpty = errors.New("criterion: the criterion id is empty")
)

// Result is one criterion decided against one build. Its identity is the pair,
// which is why this is a table of two ids and not a record with an id of its own:
// the environment the run happened on is the item's, and the build names the
// item.
type Result struct {
	ID          string
	Actor       record.Actor
	At          string
	BuildID     string
	CriterionID string
	Outcome     Outcome
}

// RecordResults writes what a run on a candidate environment produced, one row
// per criterion decided. Its caller is the deploy agent, when the run finishes:
// what is written is what was observed, and what is decided is decided at the
// gate.
//
// A re-verification runs the encodings again over a new build, which is a new
// pair and so new rows. Re-running against the same build writes the outcome
// again over the same pair, because the second run is part of how one outcome is
// reached and a recomposed environment can produce a different one — so the
// insert conflicts on the pair and updates.
func RecordResults(ctx context.Context, pool *pgxpool.Pool, actor record.Actor, buildID string, results map[string]Outcome) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if buildID == "" {
		return ErrBuildIDEmpty
	}
	for criterionID, outcome := range results {
		if criterionID == "" {
			return fmt.Errorf("%w: a result of build %s", ErrCriterionIDEmpty, buildID)
		}
		if !contains(Outcomes, outcome) {
			return fmt.Errorf("%w: %q", ErrOutcomeUnknown, outcome)
		}
		_, err := pool.Exec(ctx, `insert into `+ResultTable+`
			(id, actor_kind, actor_name, at, build_id, criterion_id, outcome)
			values ($1, $2, $3, $4, $5, $6, $7)
			on conflict (build_id, criterion_id) do update set outcome = excluded.outcome`,
			record.NewID(ResultIDPrefix), string(actor.Kind), actor.Name, record.Now(),
			buildID, criterionID, string(outcome),
		)
		if err != nil {
			return fmt.Errorf("criterion: recording %s over build %s: %w", criterionID, buildID, err)
		}
	}
	return nil
}

// ResultsForBuild is every criterion decided against one build, in the order the
// rows were written. It takes the pool and not a writer, because reading what a
// run produced is not a reason to be handed the thing that records it.
func ResultsForBuild(ctx context.Context, pool *pgxpool.Pool, buildID string) ([]Result, error) {
	rows, err := pool.Query(ctx, `select id, actor_kind, actor_name, at, build_id, criterion_id, outcome
		from `+ResultTable+` where build_id = $1 order by at, criterion_id`, buildID)
	if err != nil {
		return nil, fmt.Errorf("criterion: reading what was decided over build %s: %w", buildID, err)
	}
	defer rows.Close()

	var read []Result
	for rows.Next() {
		var r Result
		var kind, outcome string
		if err := rows.Scan(&r.ID, &kind, &r.Actor.Name, &r.At, &r.BuildID, &r.CriterionID, &outcome); err != nil {
			return nil, fmt.Errorf("criterion: reading a result of build %s: %w", buildID, err)
		}
		r.Actor.Kind = record.Kind(kind)
		r.Outcome = Outcome(outcome)
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

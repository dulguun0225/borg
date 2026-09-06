package criterion

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/service"
)

// Reliability is a criterion's outcome history read against a bound: how many
// builds it was decided over, how many of them disagreed with what it usually
// answers, that share, and whether the share is above the bound.
//
// It is derived and never authored. Each run's results already attach to the
// build and carry the composition with them, so nothing is written to produce
// this.
type Reliability struct {
	Builds        int
	Disagreements int
	Rate          float64
	// Unreliable is whether the rate is above the bound — what test practice
	// calls flaky. While it holds, the criterion's failure rejects nothing,
	// counts no attempt, and moves no prior, and Merge to master reads it as
	// absent; the result is still recorded. [Outcome.Blocks] is where that
	// exception is applied.
	Unreliable bool
}

// Unreliable reads the criterion's outcome history over the builds given and
// says whether it is above the bound. The disagreement is a build whose
// outcome differs from the outcome that criterion returned on most of them;
// the rate is that count over the builds, and fewer than two builds is a rate
// of zero, there being nothing for an outcome to disagree with.
//
// Which builds to read is the caller's, because the two narrowings the design
// puts on the history are facts this table does not hold: builds composed from
// one seed version, and builds whose diffs do not reach the requirement the
// criterion names. A caller that passes every build of the service reads a
// rate over a wider set than the design's, and a regression reached by a path
// the diff does not name reads as disagreement either way — which is why the
// criterion likeliest to catch such a regression is the one likeliest marked
// unreliable, and the gate rests on the others while it is.
//
// The bound is a field of the service record: bound is what the caller read
// off it, authored or not, and this reads it back through
// [service.UnreliableBoundInForce] rather than trusting a raw number — an
// unauthored field read as its zero value would mark a criterion unreliable
// at its first disagreement, which is not what "nothing authored" means for
// this field. The intent becoming unreliable raises is the caller's to raise,
// which doc.go names and the command-line interface now does.
func Unreliable(ctx context.Context, pool *pgxpool.Pool, criterionID string, buildIDs []string, bound gatepolicy.Authored) (Reliability, error) {
	if criterionID == "" || len(buildIDs) == 0 {
		return Reliability{}, nil
	}
	inForce := service.UnreliableBoundInForce(bound)
	rows, err := pool.Query(ctx, `select distinct on (build_id) build_id, outcome
		from `+ResultTable+` where criterion_id = $1 and build_id = any($2)
		order by build_id, run desc`, criterionID, buildIDs)
	if err != nil {
		return Reliability{}, fmt.Errorf("criterion: reading the outcome history of %s: %w", criterionID, err)
	}
	defer rows.Close()

	counts := map[Outcome]int{}
	total := 0
	for rows.Next() {
		var buildID, outcome string
		if err := rows.Scan(&buildID, &outcome); err != nil {
			return Reliability{}, fmt.Errorf("criterion: reading an outcome of %s: %w", criterionID, err)
		}
		counts[Outcome(outcome)]++
		total++
	}
	if err := rows.Err(); err != nil {
		return Reliability{}, fmt.Errorf("criterion: reading the outcome history of %s: %w", criterionID, err)
	}
	if total < 2 {
		return Reliability{Builds: total}, nil
	}

	most := 0
	for _, n := range counts {
		if n > most {
			most = n
		}
	}
	r := Reliability{Builds: total, Disagreements: total - most}
	r.Rate = float64(r.Disagreements) / float64(total)
	r.Unreliable = r.Rate > inForce
	return r, nil
}
